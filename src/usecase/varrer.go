package usecase

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/gluizcortez/projetorust/src/domain"
)

// Varredura resgata importações presas em status 1, 2 ou 3 — EVOLUÇÃO da fase
// F11, atrás de VARREDURA_ORFAS.
//
// # O problema que ela endereça
//
// No legado, uma tarefa de segundo plano que morre — pânico, queda do processo,
// contêiner reciclado no meio do processamento — deixa a linha no último status
// gravado, PARA SEMPRE. Nada a reexamina: não há varredura, não há prazo, não há
// nova tentativa. A importação some do radar e os recortes daquele diário
// simplesmente não existem. Ver docs/ESPECIFICACAO.md §3.5 e o achado A15.
//
// # Por que não existe política de REPROCESSAR
//
// Porque o serviço NÃO GUARDA O DOCUMENTO. `tb_importacao` tem o nome original
// e o resumo SHA-256, e nada mais: os bytes do PDF vivem em memória durante o
// processamento e são descartados com a tarefa. Reprocessar exigiria buscar o
// arquivo em algum lugar — e não há lugar.
//
// Isso NÃO é limitação desta implementação; é o desenho do legado. A decisão de
// passar a arquivar o documento submetido, e onde, está registrada em
// docs/DECISOES-ABERTAS.md, D-21, e é pré-requisito de qualquer reprocessamento
// automático. Enquanto ela não for tomada, as políticas possíveis são as duas
// abaixo.
type Varredura struct {
	orfas       LocalizadorDeOrfas
	importacoes domain.RepositorioImportacao
	uow         domain.UnidadeDeTrabalho
	relogio     domain.Relogio
	logger      *slog.Logger
	metricas    MetricasDeVarredura

	limiar   time.Duration
	politica PoliticaDeVarredura
	lote     int32
}

// PoliticaDeVarredura decide o que fazer com uma importação presa.
type PoliticaDeVarredura string

const (
	// PoliticaObservar apenas REGISTRA e CONTA as presas, sem tocar no banco.
	//
	// É o valor recomendado para a primeira ativação: ele mede o tamanho do
	// problema antes de mudar qualquer estado. Uma varredura que já nasce
	// escrevendo pode marcar como erro um lote inteiro de importações que na
	// verdade estavam lentas, e não presas — e não há como desfazer.
	PoliticaObservar PoliticaDeVarredura = "observar"

	// PoliticaMarcarErro grava status -1, tornando o desfecho terminal.
	//
	// É o que um operador faria à mão hoje. Não recupera o trabalho — o
	// documento não existe mais —, mas tira a linha do limbo e a torna visível
	// para quem consulta por status.
	PoliticaMarcarErro PoliticaDeVarredura = "erro"
)

// PoliticasDeVarredura lista os valores aceitos, na ordem de menor para maior
// efeito. A configuração usa esta lista para validar e para a mensagem de erro.
var PoliticasDeVarredura = []PoliticaDeVarredura{PoliticaObservar, PoliticaMarcarErro}

// PoliticaValida informa se o texto é uma política conhecida.
func PoliticaValida(v string) bool {
	for _, p := range PoliticasDeVarredura {
		if string(p) == v {
			return true
		}
	}
	return false
}

// LocalizadorDeOrfas encontra e TRANCA importações presas.
//
// Porta definida pelo consumidor. A implementação precisa trancar as linhas de
// forma que instâncias concorrentes não vejam as mesmas — ver
// postgres.RepositorioConsulta.ImportacoesPresas, que usa
// `FOR UPDATE SKIP LOCKED`.
//
// É chamada SEMPRE dentro da transação aberta por Varredura.Executar; sem isso
// a trava não sobreviveria à instrução e a exclusão mútua seria ilusória.
type LocalizadorDeOrfas interface {
	ImportacoesPresas(
		ctx context.Context, antesDe time.Time, limite int32,
	) ([]domain.ImportacaoPresa, error)
}

// MetricasDeVarredura é a instrumentação da varredura.
type MetricasDeVarredura interface {
	// ObservarVarredura registra o resultado de uma passagem.
	ObservarVarredura(encontradas, tratadas int, duracao time.Duration)
	// ContarOrfa registra uma importação presa, por status de origem.
	ContarOrfa(status string)
}

// MetricasDeVarreduraNulas descarta tudo.
type MetricasDeVarreduraNulas struct{}

var _ MetricasDeVarredura = MetricasDeVarreduraNulas{}

// ObservarVarredura não faz nada.
func (MetricasDeVarreduraNulas) ObservarVarredura(int, int, time.Duration) {}

// ContarOrfa não faz nada.
func (MetricasDeVarreduraNulas) ContarOrfa(string) {}

// DependenciasDaVarredura reúne o que a varredura precisa.
type DependenciasDaVarredura struct {
	Orfas       LocalizadorDeOrfas
	Importacoes domain.RepositorioImportacao
	UoW         domain.UnidadeDeTrabalho
	Relogio     domain.Relogio
	Logger      *slog.Logger

	// Metricas é opcional; nula vira MetricasDeVarreduraNulas.
	Metricas MetricasDeVarredura

	// Limiar é a idade mínima de data_inicio para uma importação ser
	// considerada presa. Precisa ser MAIOR que o pior tempo de processamento
	// real — ver docs/OPERACAO.md.
	Limiar time.Duration

	// Politica decide o que fazer com o que for encontrado.
	Politica PoliticaDeVarredura

	// Lote limita quantas linhas uma passagem tranca. Zero cai no padrão.
	Lote int32
}

// LotePadraoDeVarredura é quantas importações uma passagem trata.
//
// As linhas ficam TRANCADAS até o fim da transação, então o lote é o que
// limita por quanto tempo. Cem é folgado para o volume esperado de presas — que
// deveria ser zero — e pequeno o bastante para a transação ser curta.
const LotePadraoDeVarredura = 100

// NovaVarredura valida as dependências e monta o caso de uso.
func NovaVarredura(d DependenciasDaVarredura) (*Varredura, error) {
	faltando := []string{}
	if d.Orfas == nil {
		faltando = append(faltando, "Orfas")
	}
	if d.Importacoes == nil {
		faltando = append(faltando, "Importacoes")
	}
	if d.UoW == nil {
		faltando = append(faltando, "UoW")
	}
	if d.Relogio == nil {
		faltando = append(faltando, "Relogio")
	}
	if d.Logger == nil {
		faltando = append(faltando, "Logger")
	}
	if len(faltando) > 0 {
		return nil, fmt.Errorf("varredura: dependências ausentes: %s", strings.Join(faltando, ", "))
	}
	if d.Limiar <= 0 {
		return nil, fmt.Errorf("varredura: limiar precisa ser positivo, veio %s", d.Limiar)
	}
	if !PoliticaValida(string(d.Politica)) {
		return nil, fmt.Errorf("varredura: política %q desconhecida", d.Politica)
	}

	metricas := d.Metricas
	if metricas == nil {
		metricas = MetricasDeVarreduraNulas{}
	}
	lote := d.Lote
	if lote <= 0 {
		lote = LotePadraoDeVarredura
	}

	return &Varredura{
		orfas:       d.Orfas,
		importacoes: d.Importacoes,
		uow:         d.UoW,
		relogio:     d.Relogio,
		logger:      d.Logger,
		metricas:    metricas,
		limiar:      d.Limiar,
		politica:    d.Politica,
		lote:        lote,
	}, nil
}

// Executar roda UMA passagem e devolve quantas importações foram tratadas.
//
// A busca e o tratamento acontecem na MESMA transação, e é isso que torna a
// varredura segura com várias instâncias: a trava de `FOR UPDATE SKIP LOCKED`
// só vale até o commit, então soltá-la antes de gravar abriria a janela em que
// outra instância pega a mesma linha.
//
// Uma falha em QUALQUER importação reverte a passagem inteira. É deliberado: as
// presas não vão a lugar nenhum, e a próxima passagem tenta de novo. Commit
// parcial deixaria um lote metade tratado sem que nada registrasse até onde foi.
func (v *Varredura) Executar(ctx context.Context) (int, error) {
	inicio := v.relogio.Agora()
	limite := inicio.Add(-v.limiar)

	var encontradas, tratadas int

	err := v.uow.EmTransacao(ctx, func(ctx context.Context) error {
		presas, err := v.orfas.ImportacoesPresas(ctx, limite, v.lote)
		if err != nil {
			return fmt.Errorf("buscando importações presas: %w", err)
		}
		encontradas = len(presas)

		for _, presa := range presas {
			v.metricas.ContarOrfa(presa.Status.String())

			log := v.logger.With(
				slog.Int64("id_importacao", presa.ID),
				slog.String("status", presa.Status.String()),
				slog.Duration("presa_ha", idadeDe(presa.DataInicio, inicio)),
			)

			if v.politica == PoliticaObservar {
				log.WarnContext(ctx, "importação presa detectada; política é apenas observar")
				continue
			}

			log.WarnContext(ctx, "importação presa marcada como erro")
			if err := v.importacoes.AtualizarStatus(ctx, presa.ID, domain.StatusErro); err != nil {
				return fmt.Errorf("marcando importação %d como erro: %w", presa.ID, err)
			}
			tratadas++
		}
		return nil
	})

	duracao := v.relogio.Agora().Sub(inicio)
	v.metricas.ObservarVarredura(encontradas, tratadas, duracao)

	if err != nil {
		return 0, fmt.Errorf("varredura de órfãs: %w", err)
	}

	if encontradas > 0 {
		v.logger.InfoContext(ctx, "varredura de órfãs concluída",
			slog.Int("encontradas", encontradas),
			slog.Int("tratadas", tratadas),
			slog.String("politica", string(v.politica)),
			slog.Duration("duracao", duracao))
	}
	return tratadas, nil
}

// idadeDe calcula há quanto tempo a importação começou. Carimbo nulo devolve
// zero — a consulta já exclui esses casos, então é só defesa.
func idadeDe(inicioDaImportacao *time.Time, agora time.Time) time.Duration {
	if inicioDaImportacao == nil {
		return 0
	}
	return agora.Sub(*inicioDaImportacao)
}
