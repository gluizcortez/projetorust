package usecase_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gluizcortez/projetorust/internal/domain"
	"github.com/gluizcortez/projetorust/internal/domain/domaintest"
	"github.com/gluizcortez/projetorust/internal/usecase"
)

// A varredura é a evolução com maior potencial de dano da fase: ela ESCREVE em
// importações que ninguém pediu para tocar. Os testes abaixo cercam as três
// coisas que podem dar errado — varrer cedo demais, varrer o que não devia, e
// varrer a mesma coisa duas vezes.

func loggerMudoVarredura() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// relogioFixo devolve sempre o mesmo instante.
type relogioFixo struct{ t time.Time }

func (r relogioFixo) Agora() time.Time { return r.t }

var agoraDeTeste = time.Date(2024, 3, 15, 12, 0, 0, 0, time.UTC)

// localizadorFalso registra os parâmetros da busca e devolve o que lhe mandarem.
type localizadorFalso struct {
	mu       sync.Mutex
	presas   []domain.ImportacaoPresa
	err      error
	chamadas []buscaFeita
}

type buscaFeita struct {
	antesDe time.Time
	limite  int32
}

func (l *localizadorFalso) ImportacoesPresas(
	_ context.Context, antesDe time.Time, limite int32,
) ([]domain.ImportacaoPresa, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.chamadas = append(l.chamadas, buscaFeita{antesDe: antesDe, limite: limite})
	if l.err != nil {
		return nil, l.err
	}
	return l.presas, nil
}

// uowFalsa executa a função e registra se houve reversão.
type uowFalsa struct {
	revertida bool
	entradas  int
}

func (u *uowFalsa) EmTransacao(ctx context.Context, fn func(context.Context) error) error {
	u.entradas++
	if err := fn(ctx); err != nil {
		u.revertida = true
		return err
	}
	return nil
}

// metricasDaVarreduraFalsas acumula o que a varredura observou.
type metricasDaVarreduraFalsas struct {
	passagens   int
	encontradas int
	tratadas    int
	porStatus   map[string]int
}

func novasMetricasDaVarredura() *metricasDaVarreduraFalsas {
	return &metricasDaVarreduraFalsas{porStatus: map[string]int{}}
}

func (m *metricasDaVarreduraFalsas) ObservarVarredura(encontradas, tratadas int, _ time.Duration) {
	m.passagens++
	m.encontradas += encontradas
	m.tratadas += tratadas
}

func (m *metricasDaVarreduraFalsas) ContarOrfa(status string) { m.porStatus[status]++ }

func montarVarredura(
	t *testing.T,
	loc *localizadorFalso,
	repo *domaintest.RepositorioImportacaoFalso,
	uow *uowFalsa,
	metricas usecase.MetricasDeVarredura,
	politica usecase.PoliticaDeVarredura,
) *usecase.Varredura {
	t.Helper()
	v, err := usecase.NovaVarredura(usecase.DependenciasDaVarredura{
		Orfas:       loc,
		Importacoes: repo,
		UoW:         uow,
		Relogio:     relogioFixo{t: agoraDeTeste},
		Logger:      loggerMudoVarredura(),
		Metricas:    metricas,
		Limiar:      time.Hour,
		Politica:    politica,
	})
	if err != nil {
		t.Fatalf("NovaVarredura: %v", err)
	}
	return v
}

func presa(id int64, status domain.StatusImportacao, ha time.Duration) domain.ImportacaoPresa {
	inicio := agoraDeTeste.Add(-ha)
	return domain.ImportacaoPresa{ID: id, Status: status, DataInicio: &inicio}
}

// TestVarreduraCalculaOLimiteAPartirDoRelogio.
//
// O limite passado à consulta é `agora - limiar`. Errar o sinal aqui varreria
// tudo, ou nada — e as duas falhas são silenciosas.
func TestVarreduraCalculaOLimiteAPartirDoRelogio(t *testing.T) {
	loc := &localizadorFalso{}
	v := montarVarredura(t, loc, &domaintest.RepositorioImportacaoFalso{Diario: &domaintest.Diario{}},
		&uowFalsa{}, novasMetricasDaVarredura(), usecase.PoliticaObservar)

	if _, err := v.Executar(context.Background()); err != nil {
		t.Fatalf("Executar: %v", err)
	}

	if len(loc.chamadas) != 1 {
		t.Fatalf("chamadas = %d; esperava 1", len(loc.chamadas))
	}
	esperado := agoraDeTeste.Add(-time.Hour)
	if !loc.chamadas[0].antesDe.Equal(esperado) {
		t.Errorf("antesDe = %s; esperava %s", loc.chamadas[0].antesDe, esperado)
	}
	if loc.chamadas[0].limite != usecase.LotePadraoDeVarredura {
		t.Errorf("limite = %d; esperava %d", loc.chamadas[0].limite, usecase.LotePadraoDeVarredura)
	}
}

// TestPoliticaObservarNaoEscreve é o padrão, e o mais importante dos dois: uma
// varredura que já nasce escrevendo pode marcar como erro um lote inteiro de
// importações que estavam apenas lentas, e não há como desfazer.
func TestPoliticaObservarNaoEscreve(t *testing.T) {
	loc := &localizadorFalso{presas: []domain.ImportacaoPresa{
		presa(1, domain.StatusSelecionado, 3*time.Hour),
		presa(2, domain.StatusIndexando, 5*time.Hour),
		presa(3, domain.StatusRecortando, 9*time.Hour),
	}}
	repo := &domaintest.RepositorioImportacaoFalso{Diario: &domaintest.Diario{}}
	metricas := novasMetricasDaVarredura()

	v := montarVarredura(t, loc, repo, &uowFalsa{}, metricas, usecase.PoliticaObservar)

	tratadas, err := v.Executar(context.Background())
	if err != nil {
		t.Fatalf("Executar: %v", err)
	}

	if tratadas != 0 {
		t.Errorf("tratadas = %d; a política observar não trata nada", tratadas)
	}
	if gravados := repo.StatusGravados(); len(gravados) != 0 {
		t.Errorf("gravou %v; a política observar NÃO escreve", gravados)
	}

	// Mas CONTA — é para isso que ela serve.
	if metricas.encontradas != 3 {
		t.Errorf("encontradas = %d; esperava 3", metricas.encontradas)
	}
	for _, status := range domain.StatusPresos {
		if metricas.porStatus[status.String()] != 1 {
			t.Errorf("status %s contado %d vez(es); esperava 1",
				status, metricas.porStatus[status.String()])
		}
	}
}

// TestPoliticaErroMarcaMenosUm.
func TestPoliticaErroMarcaMenosUm(t *testing.T) {
	loc := &localizadorFalso{presas: []domain.ImportacaoPresa{
		presa(11, domain.StatusIndexando, 2*time.Hour),
		presa(22, domain.StatusRecortando, 4*time.Hour),
	}}
	repo := &domaintest.RepositorioImportacaoFalso{Diario: &domaintest.Diario{}}
	metricas := novasMetricasDaVarredura()

	v := montarVarredura(t, loc, repo, &uowFalsa{}, metricas, usecase.PoliticaMarcarErro)

	tratadas, err := v.Executar(context.Background())
	if err != nil {
		t.Fatalf("Executar: %v", err)
	}
	if tratadas != 2 {
		t.Errorf("tratadas = %d; esperava 2", tratadas)
	}

	// O diário guarda a SEQUÊNCIA com o identificador, que é o que interessa
	// aqui: marcar o -1 na importação errada é o pior desfecho possível.
	entradas := repo.Diario.Entradas()
	esperadas := []string{
		"Importacao.AtualizarStatus(11, erro)",
		"Importacao.AtualizarStatus(22, erro)",
	}
	if len(entradas) != len(esperadas) {
		t.Fatalf("gravações = %v; esperava %v", entradas, esperadas)
	}
	for i, esperada := range esperadas {
		if entradas[i] != esperada {
			t.Errorf("gravação %d = %q; esperava %q", i, entradas[i], esperada)
		}
	}
	if metricas.tratadas != 2 {
		t.Errorf("métrica de tratadas = %d; esperava 2", metricas.tratadas)
	}
}

// TestFalhaNoMeioRevertePassagemInteira.
//
// Commit parcial deixaria um lote metade tratado sem que nada registrasse até
// onde foi. As presas não vão a lugar nenhum: a próxima passagem tenta de novo.
func TestFalhaNoMeioRevertePassagemInteira(t *testing.T) {
	loc := &localizadorFalso{presas: []domain.ImportacaoPresa{
		presa(1, domain.StatusIndexando, 2*time.Hour),
		presa(2, domain.StatusIndexando, 2*time.Hour),
	}}
	repo := &domaintest.RepositorioImportacaoFalso{
		Diario:              &domaintest.Diario{},
		ErroAtualizarStatus: errors.New("conexão perdida"),
	}
	uow := &uowFalsa{}

	v := montarVarredura(t, loc, repo, uow, novasMetricasDaVarredura(), usecase.PoliticaMarcarErro)

	tratadas, err := v.Executar(context.Background())
	if err == nil {
		t.Fatal("Executar não devolveu erro")
	}
	if tratadas != 0 {
		t.Errorf("tratadas = %d; a passagem falhou inteira", tratadas)
	}
	if !uow.revertida {
		t.Error("a transação não foi revertida")
	}
}

// TestBuscaFalhandoNaoEscreveNada.
func TestBuscaFalhandoNaoEscreveNada(t *testing.T) {
	loc := &localizadorFalso{err: errors.New("sem conexão")}
	repo := &domaintest.RepositorioImportacaoFalso{Diario: &domaintest.Diario{}}

	v := montarVarredura(t, loc, repo, &uowFalsa{}, novasMetricasDaVarredura(), usecase.PoliticaMarcarErro)

	if _, err := v.Executar(context.Background()); err == nil {
		t.Fatal("Executar não devolveu erro")
	}
	if gravados := repo.StatusGravados(); len(gravados) != 0 {
		t.Errorf("gravou %v apesar de a busca ter falhado", gravados)
	}
}

// TestVarreduraSempreEmTransacao é o que sustenta a exclusão mútua entre
// instâncias: sem transação, `FOR UPDATE SKIP LOCKED` tranca e destranca na
// mesma instrução e duas instâncias voltam a ver as mesmas linhas.
func TestVarreduraSempreEmTransacao(t *testing.T) {
	loc := &localizadorFalso{}
	uow := &uowFalsa{}

	v := montarVarredura(t, loc, &domaintest.RepositorioImportacaoFalso{Diario: &domaintest.Diario{}},
		uow, novasMetricasDaVarredura(), usecase.PoliticaMarcarErro)

	if _, err := v.Executar(context.Background()); err != nil {
		t.Fatalf("Executar: %v", err)
	}
	if uow.entradas != 1 {
		t.Errorf("entradas em transação = %d; esperava 1", uow.entradas)
	}
}

// TestNovaVarreduraRecusaConfiguracaoInvalida: erro no arranque, não na
// primeira passagem.
func TestNovaVarreduraRecusaConfiguracaoInvalida(t *testing.T) {
	base := func() usecase.DependenciasDaVarredura {
		return usecase.DependenciasDaVarredura{
			Orfas:       &localizadorFalso{},
			Importacoes: &domaintest.RepositorioImportacaoFalso{Diario: &domaintest.Diario{}},
			UoW:         &uowFalsa{},
			Relogio:     relogioFixo{t: agoraDeTeste},
			Logger:      loggerMudoVarredura(),
			Limiar:      time.Hour,
			Politica:    usecase.PoliticaObservar,
		}
	}

	casos := []struct {
		nome    string
		ajustar func(*usecase.DependenciasDaVarredura)
		trecho  string
	}{
		{"sem localizador", func(d *usecase.DependenciasDaVarredura) { d.Orfas = nil }, "Orfas"},
		{"sem repositório", func(d *usecase.DependenciasDaVarredura) { d.Importacoes = nil }, "Importacoes"},
		{"sem unidade de trabalho", func(d *usecase.DependenciasDaVarredura) { d.UoW = nil }, "UoW"},
		{"limiar zero", func(d *usecase.DependenciasDaVarredura) { d.Limiar = 0 }, "limiar"},
		{"limiar negativo", func(d *usecase.DependenciasDaVarredura) { d.Limiar = -time.Second }, "limiar"},
		{"política desconhecida", func(d *usecase.DependenciasDaVarredura) {
			d.Politica = "reprocessar"
		}, "política"},
	}

	for _, caso := range casos {
		t.Run(caso.nome, func(t *testing.T) {
			d := base()
			caso.ajustar(&d)
			_, err := usecase.NovaVarredura(d)
			if err == nil {
				t.Fatal("NovaVarredura aceitou configuração inválida")
			}
			if !strings.Contains(err.Error(), caso.trecho) {
				t.Errorf("erro = %v; deveria mencionar %q", err, caso.trecho)
			}
		})
	}
}

// TestReprocessarNaoEUmaPolitica documenta o achado, não só o comportamento.
//
// O serviço NÃO GUARDA O DOCUMENTO: tb_importacao tem o nome e o hash, e os
// bytes do PDF morrem com a tarefa. Reprocessar exigiria buscar o arquivo em
// algum lugar, e não há lugar. Ver docs/DECISOES-ABERTAS.md, D-21.
func TestReprocessarNaoEUmaPolitica(t *testing.T) {
	if usecase.PoliticaValida("reprocessar") {
		t.Fatal("`reprocessar` foi aceita como política — ver D-21: o documento não é arquivado")
	}
	for _, p := range usecase.PoliticasDeVarredura {
		if !usecase.PoliticaValida(string(p)) {
			t.Errorf("a política declarada %q não é aceita pela validação", p)
		}
	}
}
