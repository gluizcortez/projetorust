package usecase_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"

	"github.com/gluizcortez/projetorust/internal/domain"
	"github.com/gluizcortez/projetorust/internal/domain/domaintest"
	"github.com/gluizcortez/projetorust/internal/usecase"
)

// executorFalso registra as submissões e executa a tarefa na hora, para que o
// teste observe o efeito sem esperar por goroutine.
type executorFalso struct {
	mu         sync.Mutex
	diario     *domaintest.Diario
	Erro       error
	Submetidas int
	// Adiar guarda a tarefa em vez de executá-la, para que o teste decida
	// quando — e se — ela roda.
	Adiar   bool
	Tarefas []func(context.Context)
}

func (e *executorFalso) Submeter(_ context.Context, ctxDaTarefa context.Context, tarefa func(context.Context)) error {
	e.mu.Lock()
	e.diario.Anotar("Executor.Submeter()")
	if e.Erro != nil {
		e.mu.Unlock()
		return e.Erro
	}
	e.Submetidas++
	adiar := e.Adiar
	if adiar {
		e.Tarefas = append(e.Tarefas, tarefa)
	}
	e.mu.Unlock()

	if !adiar {
		tarefa(ctxDaTarefa)
	}
	return nil
}

// submissaoValida devolve campos que passam por todas as críticas.
func submissaoValida() domain.SubmissaoPDF {
	texto := func(s string) *string { return &s }
	return domain.SubmissaoPDF{
		DataCaderno:          texto("2024-03-15"),
		DataDisponibilizacao: texto("2024-03-16"),
		IDUsuario:            texto("77"),
		IDCaderno:            texto("9"),
		ArquivoEnviado:       true,
		NomeDoArquivo:        texto("diario.pdf"),
	}
}

type cenarioDeIngestao struct {
	diario      *domaintest.Diario
	importacoes *domaintest.RepositorioImportacaoFalso
	executor    *executorFalso
	processadas []int64
	mu          sync.Mutex
}

func novoCenarioDeIngestao() *cenarioDeIngestao {
	d := &domaintest.Diario{}
	return &cenarioDeIngestao{
		diario:      d,
		importacoes: &domaintest.RepositorioImportacaoFalso{Diario: d, IDGerado: 42},
		executor:    &executorFalso{diario: d},
	}
}

func (c *cenarioDeIngestao) ingestao(t *testing.T) *usecase.Ingestao {
	t.Helper()
	i, err := usecase.NovaIngestao(usecase.DependenciasDaIngestao{
		Importacoes: c.importacoes,
		Executor:    c.executor,
		Logger:      slog.New(slog.NewTextHandler(io.Discard, nil)),
		Processar: func(_ context.Context, id int64, conteudo []byte) {
			c.mu.Lock()
			defer c.mu.Unlock()
			c.processadas = append(c.processadas, id)
			c.diario.Anotar("Pipeline.Processar(%d, bytes=%d)", id, len(conteudo))
		},
	})
	if err != nil {
		t.Fatalf("NovaIngestao: %v", err)
	}
	return i
}

// TestIngestaoSequenciaDeChamadas fixa a ordem do caminho síncrono,
// reference/main.rs:233-256.
func TestIngestaoSequenciaDeChamadas(t *testing.T) {
	c := novoCenarioDeIngestao()

	id, err := c.ingestao(t).Executar(context.Background(), usecase.ComandoIngerir{
		Submissao: submissaoValida(),
		Conteudo:  []byte("conteudo do pdf"),
	})
	if err != nil {
		t.Fatalf("Executar: %v", err)
	}
	if id != 42 {
		t.Errorf("id = %d; esperava 42", id)
	}

	esperado := hex.EncodeToString(func() []byte {
		s := sha256.Sum256([]byte("conteudo do pdf"))
		return s[:]
	}())

	conferirSequencia(t, c.diario, []string{
		"Importacao.Registrar(caderno=9, hash=" + esperado + ")",
		// main.rs:249 — ESCRITA REDUNDANTE: o INSERT já gravou 0.
		"Importacao.AtualizarStatus(42, recebido)",
		"Executor.Submeter()",
		"Pipeline.Processar(42, bytes=15)",
	})
}

// TestIngestaoCalculaHashDoConteudo fixa main.rs:233-235: o resumo é do
// CONTEÚDO, não do nome do arquivo.
func TestIngestaoCalculaHashDoConteudo(t *testing.T) {
	c := novoCenarioDeIngestao()

	if _, err := c.ingestao(t).Executar(context.Background(), usecase.ComandoIngerir{
		Submissao: submissaoValida(),
		Conteudo:  []byte("abc"),
	}); err != nil {
		t.Fatalf("Executar: %v", err)
	}

	if len(c.importacoes.Registradas) != 1 {
		t.Fatalf("houve %d registro(s)", len(c.importacoes.Registradas))
	}
	// SHA-256 de "abc", valor conhecido publicamente.
	const esperado = "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad"
	if obtido := c.importacoes.Registradas[0].HashSHA256; obtido != esperado {
		t.Errorf("hash = %q; esperava %q", obtido, esperado)
	}
}

// TestIngestaoRegistraOsCamposValidados confere o que vai para o banco.
func TestIngestaoRegistraOsCamposValidados(t *testing.T) {
	c := novoCenarioDeIngestao()

	if _, err := c.ingestao(t).Executar(context.Background(), usecase.ComandoIngerir{
		Submissao: submissaoValida(),
		Conteudo:  []byte("pdf"),
	}); err != nil {
		t.Fatalf("Executar: %v", err)
	}

	imp := c.importacoes.Registradas[0]
	if imp.IDUsuario != 77 || imp.IDCaderno != 9 {
		t.Errorf("id_usuario=%d id_caderno=%d; esperava 77 e 9", imp.IDUsuario, imp.IDCaderno)
	}
	if imp.ArquivoPDF != "diario.pdf" {
		t.Errorf("arquivo = %q", imp.ArquivoPDF)
	}
	if imp.TipoCaderno != domain.TipoCadernoPDF {
		t.Errorf("tipo_caderno = %q; esperava %q", imp.TipoCaderno, domain.TipoCadernoPDF)
	}
	if imp.Status != domain.StatusRecebido {
		t.Errorf("status = %s; esperava recebido", imp.Status)
	}
	if imp.DataCaderno.String() != "2024-03-15" {
		t.Errorf("data_caderno = %q", imp.DataCaderno.String())
	}
}

// TestIngestaoRejeitaSemTocarNoBanco: havendo crítica, nenhuma porta é chamada.
//
// A ordem das críticas é contrato observável (docs/ESPECIFICACAO.md §1.4.2) e
// vem do domínio; aqui o que se verifica é que a rejeição acontece ANTES de
// qualquer efeito.
func TestIngestaoRejeitaSemTocarNoBanco(t *testing.T) {
	c := novoCenarioDeIngestao()

	_, err := c.ingestao(t).Executar(context.Background(), usecase.ComandoIngerir{
		Submissao: domain.SubmissaoPDF{}, // tudo ausente
		Conteudo:  []byte("pdf"),
	})

	if !errors.Is(err, domain.ErrValidacao) {
		t.Fatalf("erro = %v; esperava ErrValidacao", err)
	}

	var validacao *domain.ErroDeValidacao
	if !errors.As(err, &validacao) {
		t.Fatal("o erro não carrega as críticas")
	}
	const esperada = "Data do caderno não informada," +
		"Data de disponibilização não informada," +
		"Id do usuário não informado," +
		"Id do caderno não informado," +
		"PDF não enviado"
	if obtida := validacao.Criticas.Mensagem(); obtida != esperada {
		t.Errorf("mensagem = %q\nesperava      %q", obtida, esperada)
	}

	if entradas := c.diario.Entradas(); len(entradas) != 0 {
		t.Errorf("nenhuma porta deveria ter sido chamada, e foram: %v", entradas)
	}
}

// TestIngestaoFalhaAoRegistrar cobre main.rs:237-245: o erro sobe e nada é
// agendado.
func TestIngestaoFalhaAoRegistrar(t *testing.T) {
	c := novoCenarioDeIngestao()
	c.importacoes.ErroRegistrar = domain.ErrPersistencia

	_, err := c.ingestao(t).Executar(context.Background(), usecase.ComandoIngerir{
		Submissao: submissaoValida(),
		Conteudo:  []byte("pdf"),
	})

	if !errors.Is(err, domain.ErrPersistencia) {
		t.Fatalf("erro = %v; esperava ErrPersistencia", err)
	}
	if c.executor.Submetidas != 0 {
		t.Error("nada deveria ter sido agendado")
	}
}

// TestIngestaoFalhaAoGravarStatusNaoInterrompe: a escrita redundante de
// main.rs:249 usa `let _ = ...`.
func TestIngestaoFalhaAoGravarStatusNaoInterrompe(t *testing.T) {
	c := novoCenarioDeIngestao()
	c.importacoes.ErroAtualizarStatus = domain.ErrPersistencia

	id, err := c.ingestao(t).Executar(context.Background(), usecase.ComandoIngerir{
		Submissao: submissaoValida(),
		Conteudo:  []byte("pdf"),
	})
	if err != nil {
		t.Fatalf("Executar: %v", err)
	}
	if id != 42 {
		t.Errorf("id = %d", id)
	}
	if c.executor.Submetidas != 1 {
		t.Error("o processamento deveria ter sido agendado assim mesmo")
	}
}

// TestIngestaoFalhaAoAgendar: sem equivalente no legado, onde
// `tokio::task::spawn` nunca recusa. A importação já existe e o id é devolvido
// junto com o erro, para que a camada HTTP saiba do que se trata.
func TestIngestaoFalhaAoAgendar(t *testing.T) {
	c := novoCenarioDeIngestao()
	c.executor.Erro = errors.New("executor encerrado")

	id, err := c.ingestao(t).Executar(context.Background(), usecase.ComandoIngerir{
		Submissao: submissaoValida(),
		Conteudo:  []byte("pdf"),
	})

	if err == nil {
		t.Fatal("esperava erro")
	}
	if id != 42 {
		t.Errorf("id = %d; o registro aconteceu e o id deve voltar mesmo com erro", id)
	}
}

// TestIngestaoTarefaSobreviveAoCancelamentoDaRequisicao é paridade: no legado a
// tarefa de fundo sobrevive à resposta HTTP (main.rs:256). Cancelar o
// processamento porque o cliente desligou seria comportamento novo.
func TestIngestaoTarefaSobreviveAoCancelamentoDaRequisicao(t *testing.T) {
	c := novoCenarioDeIngestao()
	c.executor.Adiar = true

	ctx, cancelar := context.WithCancel(context.Background())
	if _, err := c.ingestao(t).Executar(ctx, usecase.ComandoIngerir{
		Submissao: submissaoValida(),
		Conteudo:  []byte("pdf"),
	}); err != nil {
		t.Fatalf("Executar: %v", err)
	}

	// A requisição termina — e a tarefa ainda não rodou.
	cancelar()

	if len(c.executor.Tarefas) != 1 {
		t.Fatalf("houve %d tarefa(s) adiada(s)", len(c.executor.Tarefas))
	}
	c.executor.Tarefas[0](context.Background())

	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.processadas) != 1 {
		t.Fatal("a tarefa não rodou depois do cancelamento da requisição")
	}
}

// TestIngestaoDependenciasAusentesSaoRecusadas.
func TestIngestaoDependenciasAusentesSaoRecusadas(t *testing.T) {
	if _, err := usecase.NovaIngestao(usecase.DependenciasDaIngestao{}); err == nil {
		t.Fatal("NovaIngestao aceitou dependências vazias")
	}

	c := novoCenarioDeIngestao()
	semProcessar := usecase.DependenciasDaIngestao{
		Importacoes: c.importacoes,
		Executor:    c.executor,
		Logger:      slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	_, err := usecase.NovaIngestao(semProcessar)
	if err == nil || !strings.Contains(err.Error(), "Processar") {
		t.Errorf("erro = %v; esperava mencionar Processar", err)
	}
}
