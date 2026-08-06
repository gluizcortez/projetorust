package usecase

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"strings"

	"github.com/gluizcortez/projetorust/internal/domain"
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
}

// DependenciasDaIngestao reúne o que a ingestão precisa.
type DependenciasDaIngestao struct {
	Importacoes domain.RepositorioImportacao
	Executor    Executor
	Logger      *slog.Logger
	Processar   func(ctx context.Context, id int64, conteudo []byte)
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
		importacoes: d.Importacoes,
		executor:    d.Executor,
		logger:      d.Logger,
		processar:   d.Processar,
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

	// 3 — main.rs:233-235. O resumo é do conteúdo, não do nome.
	soma := sha256.Sum256(cmd.Conteudo)
	importacao.HashSHA256 = hex.EncodeToString(soma[:])

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
