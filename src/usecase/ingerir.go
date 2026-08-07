package usecase

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/gluizcortez/projetorust/src/domain"
)

// Ingestao é o caminho SÍNCRONO da submissão de um PDF: tudo o que o legado faz
// antes de responder 200 (reference/main.rs:143-253).
//
// Não extrai texto, não indexa e não busca — apenas valida, registra e entrega
// o trabalho ao executor.
type Ingestao struct {
	importacoes domain.RepositorioImportacao
	executor    Executor
	logger      *slog.Logger

	// processar é o que será executado em segundo plano. É uma função, e não o
	// *Pipeline, para que a ingestão não dependa do pipeline concreto — o que
	// mantém os dois testáveis isolados.
	processar func(ctx context.Context, id int64, conteudo []byte)

	// --- evoluções da fase F11, todas desligadas por padrão ---

	// validarAssinatura recusa arquivo que não comece com `%PDF-`.
	validarAssinatura bool

	// equivalentes é consultado quando a idempotência por hash está ligada.
	// NULO desliga a evolução — e é o padrão, então nenhuma consulta a mais é
	// emitida em relação ao legado.
	equivalentes ConsultorDeIdempotencia
}

// ConsultorDeIdempotencia procura uma importação já registrada para o mesmo
// documento — EVOLUÇÃO da fase F11, atrás de IDEMPOTENCIA_POR_HASH.
//
// Porta definida pelo consumidor, como todas as outras.
type ConsultorDeIdempotencia interface {
	// ImportacaoEquivalente devolve a importação de mesmo hash, mesmo caderno e
	// mesma data de caderno.
	//
	// Nenhuma correspondência devolve domain.ErrImportacaoNaoEncontrada — não um
	// resumo zerado, que seria indistinguível de uma importação de id 0.
	//
	// Havendo mais de uma — o legado permite, porque nunca deduplicou —, a
	// implementação devolve a MAIS RECENTE. Ver postgres.RepositorioConsulta.
	ImportacaoEquivalente(
		ctx context.Context, chave domain.ChaveDeIdempotencia,
	) (domain.ResumoDaImportacao, error)
}

// DependenciasDaIngestao reúne o que a ingestão precisa.
type DependenciasDaIngestao struct {
	Importacoes domain.RepositorioImportacao
	Executor    Executor
	Logger      *slog.Logger
	Processar   func(ctx context.Context, id int64, conteudo []byte)

	// ValidarAssinaturaPDF liga a checagem do prefixo `%PDF-`. Padrão false.
	ValidarAssinaturaPDF bool

	// Equivalentes liga a idempotência por hash quando NÃO nula. Padrão nulo.
	Equivalentes ConsultorDeIdempotencia
}

// NovaIngestao valida as dependências e monta o caso de uso.
func NovaIngestao(d DependenciasDaIngestao) (*Ingestao, error) {
	faltando := []string{}
	if d.Importacoes == nil {
		faltando = append(faltando, "Importacoes")
	}
	if d.Executor == nil {
		faltando = append(faltando, "Executor")
	}
	if d.Logger == nil {
		faltando = append(faltando, "Logger")
	}
	if d.Processar == nil {
		faltando = append(faltando, "Processar")
	}
	if len(faltando) > 0 {
		return nil, fmt.Errorf("ingestão: dependências ausentes: %s", strings.Join(faltando, ", "))
	}

	return &Ingestao{
		importacoes:       d.Importacoes,
		executor:          d.Executor,
		logger:            d.Logger,
		processar:         d.Processar,
		validarAssinatura: d.ValidarAssinaturaPDF,
		equivalentes:      d.Equivalentes,
	}, nil
}

// ComandoIngerir são os dados crus de uma submissão.
type ComandoIngerir struct {
	// Submissao são os campos do formulário, ainda como texto.
	Submissao domain.SubmissaoPDF
	// Conteudo é o arquivo já lido em memória.
	//
	// O legado lê o arquivo inteiro antes de qualquer coisa
	// (reference/main.rs:233) e o move para a tarefa de fundo. Transmitir em
	// fluxo mudaria o momento em que uma falha de leitura aparece.
	Conteudo []byte
}

// Executar valida a submissão, registra a importação e agenda o processamento.
//
// Devolve o identificador gerado. A tradução para HTTP — 400 com as críticas,
// 422 na falha de registro, 200 com o corpo literal — é da fase F9.
//
// Reproduz main.rs:143-253 nesta ordem:
//
//  1. valida, acumulando as críticas na ordem contratual;
//  2. havendo crítica, devolve ErroDeValidacao e NÃO toca no banco;
//  3. calcula o SHA-256 do conteúdo;
//  4. Registrar → id;
//  5. AtualizarStatus(id, recebido) — redundante, o INSERT já gravou 0;
//  6. entrega ao executor.
func (i *Ingestao) Executar(ctx context.Context, cmd ComandoIngerir) (int64, error) {
	// 1 e 2 — main.rs:151-222.
	importacao, criticas := cmd.Submissao.Validar()
	if !criticas.Vazio() {
		i.logger.WarnContext(ctx, "validação rejeitou a requisição",
			slog.Any("criticas", criticas.Itens()))
		return 0, domain.NovoErroDeValidacao(criticas)
	}

	// EVOLUÇÃO — VALIDAR_ASSINATURA_PDF.
	//
	// Fica DEPOIS da validação de propósito. Uma submissão que erra a data E
	// manda um arquivo que não é PDF continua recebendo as críticas na ordem
	// contratual, em vez de trocá-las por um 422 menos informativo — o efeito da
	// chave se limita ao que o legado ACEITARIA.
	//
	// Ainda é "antes de qualquer processamento" no sentido que importa: nada é
	// gravado, nada é agendado, nenhuma importação é gasta.
	//
	// A resposta é a MESMA 422 do legado, sem texto novo: ErrPDFInvalido cai no
	// ramo genérico de uploadPDF, que responde "Erro ao processar o PDF".
	if i.validarAssinatura && !domain.PareceComPDF(cmd.Conteudo) {
		i.logger.WarnContext(ctx, "arquivo sem assinatura de PDF recusado",
			slog.Int("bytes", len(cmd.Conteudo)))
		return 0, fmt.Errorf("assinatura ausente: %w", domain.ErrPDFInvalido)
	}

	// 3 — main.rs:233-235. O resumo é do conteúdo, não do nome.
	soma := sha256.Sum256(cmd.Conteudo)
	importacao.HashSHA256 = hex.EncodeToString(soma[:])

	// EVOLUÇÃO — IDEMPOTENCIA_POR_HASH. Precisa vir depois do resumo, que é a
	// própria chave de busca, e antes do Registrar, que é o que ela evita.
	if id, reaproveitou := i.reaproveitar(ctx, importacao); reaproveitou {
		return id, nil
	}

	// 4 — main.rs:237-245.
	id, err := i.importacoes.Registrar(ctx, importacao)
	if err != nil {
		i.logger.ErrorContext(ctx, "falha ao registrar importação", slog.Any("erro", err))
		return 0, fmt.Errorf("registrando importação: %w", err)
	}

	log := i.logger.With(slog.Int64("id_importacao", id))

	// 5 — main.rs:249. ESCRITA REDUNDANTE, preservada: o INSERT já gravou 0
	// via o Default de DiarioPDF. Uma auditoria que conte comandos observa duas
	// operações. Ver docs/ESPECIFICACAO.md §3.2 e o achado A16.
	if err := i.importacoes.AtualizarStatus(ctx, id, domain.StatusRecebido); err != nil {
		log.ErrorContext(ctx, "falha ao gravar status; o legado ignoraria em silêncio",
			slog.String("status", domain.StatusRecebido.String()), slog.Any("erro", err))
	}

	// 6 — main.rs:251-256. O contador do legado é incrementado ANTES do spawn;
	// aqui quem registra a tarefa antes de dispará-la é o executor.
	//
	// context.WithoutCancel PRESERVA os valores do contexto da requisição — o
	// identificador de requisição, que é o que liga os registros das duas
	// pontas — e DESCARTA o cancelamento. É paridade: no legado a tarefa
	// sobrevive à resposta (main.rs:256), e encerrá-la porque o cliente
	// desconectou seria comportamento novo.
	//
	// A tarefa fica sem prazo, como no legado. Quem a interrompe é a drenagem
	// do executor, no encerramento do processo — não um relógio.
	conteudo := cmd.Conteudo
	tarefa := func(ctxDaTarefa context.Context) { i.processar(ctxDaTarefa, id, conteudo) }

	if err := i.executor.Submeter(ctx, context.WithoutCancel(ctx), tarefa); err != nil {
		// Sem equivalente no legado, onde `tokio::task::spawn` nunca recusa.
		// Só acontece durante o encerramento. A importação fica em 0, que é o
		// estado correto para algo que foi aceito e não começou.
		log.ErrorContext(ctx, "falha ao agendar o processamento", slog.Any("erro", err))
		return id, fmt.Errorf("agendando processamento da importação %d: %w", id, err)
	}

	log.InfoContext(ctx, "importação aceita", slog.Int("bytes", len(cmd.Conteudo)))
	return id, nil
}

// reaproveitar procura uma importação FINALIZADA do mesmo documento.
//
// EVOLUÇÃO da fase F11 — IDEMPOTENCIA_POR_HASH. Devolve `false` quando a chave
// está desligada, quando não há equivalente, ou quando o equivalente ainda não
// terminou.
//
// # Por que só a FINALIZADA conta
//
// Uma importação em status 0 a 3 está em curso: devolver o id dela faria o
// cliente crer que o reenvio foi aceito, quando na verdade ele só herdou o
// destino de uma tarefa que pode falhar. Uma em -1 terminou em ERRO, e reenviar
// depois de um erro é exatamente o que o operador precisa poder fazer. Só o 5 é
// resultado utilizável.
//
// # Falha de consulta NÃO derruba a submissão
//
// A idempotência é otimização, não regra de negócio: banco indisponível para o
// SELECT vira registro e segue o caminho normal, que é o do legado. Falhar aqui
// tornaria o serviço MENOS disponível com a chave ligada — o oposto do que ela
// existe para fazer.
func (i *Ingestao) reaproveitar(ctx context.Context, imp domain.Importacao) (int64, bool) {
	if i.equivalentes == nil {
		return 0, false
	}

	chave := domain.DeImportacao(imp)
	existente, err := i.equivalentes.ImportacaoEquivalente(ctx, chave)
	switch {
	case errors.Is(err, domain.ErrImportacaoNaoEncontrada):
		return 0, false
	case err != nil:
		i.logger.ErrorContext(ctx, "falha ao consultar idempotência; seguindo sem ela",
			slog.Any("erro", err))
		return 0, false
	}

	if existente.Status != domain.StatusFinalizado {
		i.logger.InfoContext(ctx, "documento equivalente ainda em curso; registrando mesmo assim",
			slog.Int64("id_importacao_equivalente", existente.ID),
			slog.String("status", existente.Status.String()))
		return 0, false
	}

	i.logger.InfoContext(ctx, "reenvio reaproveitou importação finalizada",
		slog.Int64("id_importacao", existente.ID),
		slog.Int("bytes", len(imp.HashSHA256)/2))
	return existente.ID, true
}
