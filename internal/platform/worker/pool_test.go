package worker_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gluizcortez/projetorust/internal/platform/worker"
)

func loggerMudo() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// loggerEmBuffer devolve um registro e o buffer que o recebe, com trava para
// leitura concorrente.
func loggerEmBuffer() (*slog.Logger, func() string) {
	var mu sync.Mutex
	buf := &bytes.Buffer{}
	escritor := escritorSincronizado{mu: &mu, buf: buf}
	l := slog.New(slog.NewTextHandler(escritor, &slog.HandlerOptions{Level: slog.LevelDebug}))
	return l, func() string {
		mu.Lock()
		defer mu.Unlock()
		return buf.String()
	}
}

type escritorSincronizado struct {
	mu  *sync.Mutex
	buf *bytes.Buffer
}

func (e escritorSincronizado) Write(p []byte) (int, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.buf.Write(p) //nolint:wrapcheck // adaptador de teste sobre bytes.Buffer
}

// TestSubmeterExecutaATarefa é a sanidade básica.
func TestSubmeterExecutaATarefa(t *testing.T) {
	p := worker.NovoPool(loggerMudo(), 0)

	pronto := make(chan struct{})
	if err := p.Submeter(context.Background(), context.Background(), func(context.Context) {
		close(pronto)
	}); err != nil {
		t.Fatalf("Submeter: %v", err)
	}

	select {
	case <-pronto:
	case <-time.After(5 * time.Second):
		t.Fatal("a tarefa não executou")
	}
	if err := p.Drenar(context.Background()); err != nil {
		t.Errorf("Drenar: %v", err)
	}
}

// TestSubmeterRecusaTarefaNula.
func TestSubmeterRecusaTarefaNula(t *testing.T) {
	p := worker.NovoPool(loggerMudo(), 0)
	if err := p.Submeter(context.Background(), context.Background(), nil); err == nil {
		t.Error("Submeter aceitou tarefa nula")
	}
}

// TestDrenarAguardaTodasAsTarefas é a substituição do laço de sondagem do
// legado: a drenagem só retorna quando a última tarefa termina.
//
// O que o teste protege é a JANELA entre registrar a tarefa e disparar a
// goroutine. Se o Add(1) acontecesse dentro da goroutine, Drenar poderia
// concluir achando que não há trabalho.
func TestDrenarAguardaTodasAsTarefas(t *testing.T) {
	p := worker.NovoPool(loggerMudo(), 0)

	const tarefas = 50
	var concluidas atomic.Int64
	liberar := make(chan struct{})

	for range tarefas {
		if err := p.Submeter(context.Background(), context.Background(), func(context.Context) {
			<-liberar
			concluidas.Add(1)
		}); err != nil {
			t.Fatalf("Submeter: %v", err)
		}
	}

	close(liberar)
	if err := p.Drenar(context.Background()); err != nil {
		t.Fatalf("Drenar: %v", err)
	}
	if n := concluidas.Load(); n != tarefas {
		t.Errorf("concluíram %d tarefas; esperava %d", n, tarefas)
	}
	if n := p.EmAndamento(); n != 0 {
		t.Errorf("EmAndamento = %d depois da drenagem", n)
	}
}

// TestDrenarComPrazoExpiradoNaoBloqueiaParaSempre é o critério de aceite: o
// legado espera indefinidamente (ESPECIFICACAO §6.3, DEFEITO PRESERVADO), e a
// versão em Go reproduz isso com contexto sem prazo — mas precisa devolver erro
// quando há prazo.
func TestDrenarComPrazoExpiradoNaoBloqueiaParaSempre(t *testing.T) {
	p := worker.NovoPool(loggerMudo(), 0)

	presa := make(chan struct{})
	defer close(presa)

	if err := p.Submeter(context.Background(), context.Background(), func(context.Context) {
		<-presa
	}); err != nil {
		t.Fatalf("Submeter: %v", err)
	}

	ctx, cancelar := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancelar()

	inicio := time.Now()
	err := p.Drenar(ctx)
	decorrido := time.Since(inicio)

	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("erro = %v; esperava DeadlineExceeded", err)
	}
	if decorrido > 3*time.Second {
		t.Errorf("a drenagem levou %v; deveria respeitar o prazo", decorrido)
	}
	if err != nil && !strings.Contains(err.Error(), "1 tarefa") {
		t.Errorf("o erro deveria dizer quantas tarefas ficaram: %v", err)
	}
}

// TestDrenarEmitePeriodicamenteOAviso reproduz `Aguardando tarefas encerrarem.`
// de main.rs:72, na mesma cadência.
func TestDrenarEmitePeriodicamenteOAviso(t *testing.T) {
	logger, conteudo := loggerEmBuffer()
	p := worker.NovoPool(logger, 0)

	presa := make(chan struct{})
	if err := p.Submeter(context.Background(), context.Background(), func(context.Context) {
		<-presa
	}); err != nil {
		t.Fatalf("Submeter: %v", err)
	}

	ctx, cancelar := context.WithTimeout(context.Background(),
		4*worker.IntervaloDeAvisoDeDrenagem+50*time.Millisecond)
	defer cancelar()
	_ = p.Drenar(ctx)
	close(presa)

	texto := conteudo()
	if n := strings.Count(texto, "drenando importações"); n < 2 {
		t.Errorf("o aviso saiu %d vez(es) em ~4 intervalos; esperava ao menos 2\n%s", n, texto)
	}
	if !strings.Contains(texto, "em_andamento=1") {
		t.Errorf("o aviso deveria trazer em_andamento:\n%s", texto)
	}
}

// TestDrenarSemTarefasNaoEmiteAviso: o caminho rápido não deve poluir o
// registro no encerramento de um processo ocioso.
func TestDrenarSemTarefasNaoEmiteAviso(t *testing.T) {
	logger, conteudo := loggerEmBuffer()
	p := worker.NovoPool(logger, 0)

	if err := p.Drenar(context.Background()); err != nil {
		t.Fatalf("Drenar: %v", err)
	}
	if texto := conteudo(); strings.Contains(texto, "drenando") {
		t.Errorf("não deveria ter emitido aviso:\n%s", texto)
	}
}

// TestSubmeterDepoisDeDrenarERecusado.
func TestSubmeterDepoisDeDrenarERecusado(t *testing.T) {
	p := worker.NovoPool(loggerMudo(), 0)
	if err := p.Drenar(context.Background()); err != nil {
		t.Fatalf("Drenar: %v", err)
	}

	err := p.Submeter(context.Background(), context.Background(), func(context.Context) {})
	if !errors.Is(err, worker.ErrPoolEncerrado) {
		t.Errorf("erro = %v; esperava ErrPoolEncerrado", err)
	}
}

// TestDrenarDuasVezesESeguro.
func TestDrenarDuasVezesESeguro(t *testing.T) {
	p := worker.NovoPool(loggerMudo(), 0)
	for i := range 2 {
		if err := p.Drenar(context.Background()); err != nil {
			t.Errorf("drenagem %d: %v", i, err)
		}
	}
}

// TestPanicoNaTarefaNaoDerrubaOProcesso é o critério de aceite: o processo
// sobrevive, o contador é liberado e o pânico é registrado.
//
// Reproduz o efeito do tokio, que captura o pânico na JoinHandle e a descarta
// (docs/ESPECIFICACAO.md §6.4).
func TestPanicoNaTarefaNaoDerrubaOProcesso(t *testing.T) {
	logger, conteudo := loggerEmBuffer()
	p := worker.NovoPool(logger, 0)

	if err := p.Submeter(context.Background(), context.Background(), func(context.Context) {
		panic("defeito proposital")
	}); err != nil {
		t.Fatalf("Submeter: %v", err)
	}

	// Se o contador não fosse liberado, esta drenagem nunca retornaria.
	ctx, cancelar := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelar()
	if err := p.Drenar(ctx); err != nil {
		t.Fatalf("Drenar depois do pânico: %v", err)
	}
	if n := p.EmAndamento(); n != 0 {
		t.Errorf("EmAndamento = %d; o contador não foi liberado", n)
	}

	texto := conteudo()
	if !strings.Contains(texto, "pânico") {
		t.Errorf("o pânico deveria ter sido registrado:\n%s", texto)
	}
	if !strings.Contains(texto, "defeito proposital") {
		t.Errorf("o valor do pânico deveria aparecer:\n%s", texto)
	}
	if !strings.Contains(texto, "pilha") {
		t.Errorf("a pilha deveria aparecer:\n%s", texto)
	}
}

// TestPanicoLiberaVaga: com teto de concorrência, um pânico não pode vazar a
// vaga e travar o executor.
func TestPanicoLiberaVaga(t *testing.T) {
	p := worker.NovoPool(loggerMudo(), 1)

	for i := range 5 {
		if err := p.Submeter(context.Background(), context.Background(), func(context.Context) {
			panic(i)
		}); err != nil {
			t.Fatalf("submissão %d: %v", i, err)
		}
	}

	ctx, cancelar := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelar()
	if err := p.Drenar(ctx); err != nil {
		t.Fatalf("Drenar: %v", err)
	}
}

// TestSemTetoNaoBloqueia é a paridade: com MAX_IMPORTACOES_CONCORRENTES em
// zero, `Submeter` nunca espera, como `tokio::task::spawn`.
func TestSemTetoNaoBloqueia(t *testing.T) {
	p := worker.NovoPool(loggerMudo(), 0)

	const tarefas = 200
	liberar := make(chan struct{})
	var ativas atomic.Int64
	var pico atomic.Int64

	for range tarefas {
		if err := p.Submeter(context.Background(), context.Background(), func(context.Context) {
			atual := ativas.Add(1)
			for {
				anterior := pico.Load()
				if atual <= anterior || pico.CompareAndSwap(anterior, atual) {
					break
				}
			}
			<-liberar
			ativas.Add(-1)
		}); err != nil {
			t.Fatalf("Submeter: %v", err)
		}
	}
	if n := p.Aguardando(); n != 0 {
		t.Errorf("Aguardando = %d sem teto de concorrência", n)
	}

	close(liberar)
	if err := p.Drenar(context.Background()); err != nil {
		t.Fatalf("Drenar: %v", err)
	}
	if pico.Load() < 2 {
		t.Errorf("pico de concorrência = %d; sem teto deveria haver paralelismo", pico.Load())
	}
}

// TestTetoDeConcorrenciaLimitaOParalelismo cobre a EVOLUÇÃO: qualquer valor
// positivo em MAX_IMPORTACOES_CONCORRENTES limita as tarefas simultâneas.
func TestTetoDeConcorrenciaLimitaOParalelismo(t *testing.T) {
	const teto = 3
	p := worker.NovoPool(loggerMudo(), teto)

	var ativas atomic.Int64
	var pico atomic.Int64
	var grupo sync.WaitGroup

	const tarefas = 30
	for range tarefas {
		grupo.Add(1)
		go func() {
			defer grupo.Done()
			if err := p.Submeter(context.Background(), context.Background(), func(context.Context) {
				atual := ativas.Add(1)
				for {
					anterior := pico.Load()
					if atual <= anterior || pico.CompareAndSwap(anterior, atual) {
						break
					}
				}
				time.Sleep(time.Millisecond)
				ativas.Add(-1)
			}); err != nil {
				t.Errorf("Submeter: %v", err)
			}
		}()
	}
	grupo.Wait()

	if err := p.Drenar(context.Background()); err != nil {
		t.Fatalf("Drenar: %v", err)
	}
	if pico.Load() > teto {
		t.Errorf("pico de concorrência = %d; o teto é %d", pico.Load(), teto)
	}
}

// TestEsperaPorVagaRespeitaOContexto: com o teto cheio, uma submissão com prazo
// expirado desiste em vez de bloquear.
func TestEsperaPorVagaRespeitaOContexto(t *testing.T) {
	p := worker.NovoPool(loggerMudo(), 1)

	liberar := make(chan struct{})
	if err := p.Submeter(context.Background(), context.Background(), func(context.Context) {
		<-liberar
	}); err != nil {
		t.Fatalf("primeira submissão: %v", err)
	}

	ctx, cancelar := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancelar()

	err := p.Submeter(ctx, context.Background(), func(context.Context) {})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("erro = %v; esperava DeadlineExceeded", err)
	}

	// A recusa não pode ter deixado a contagem inconsistente: se o Add(1)
	// tivesse ficado sem o Done correspondente, esta drenagem travaria.
	close(liberar)
	ctxDrenagem, cancelarDrenagem := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelarDrenagem()
	if err := p.Drenar(ctxDrenagem); err != nil {
		t.Errorf("Drenar: %v — a submissão recusada vazou uma entrada no grupo", err)
	}
}

// TestContextoDaTarefaEIndependenteDoDaSubmissao é paridade: a tarefa recebe o
// contexto que o chamador designou para ela, não o da requisição.
func TestContextoDaTarefaEIndependenteDoDaSubmissao(t *testing.T) {
	p := worker.NovoPool(loggerMudo(), 0)

	ctxSubmissao, cancelarSubmissao := context.WithCancel(context.Background())
	ctxTarefa := context.Background()

	visto := make(chan error, 1)
	if err := p.Submeter(ctxSubmissao, ctxTarefa, func(ctx context.Context) {
		visto <- ctx.Err()
	}); err != nil {
		t.Fatalf("Submeter: %v", err)
	}
	cancelarSubmissao()

	if err := p.Drenar(context.Background()); err != nil {
		t.Fatalf("Drenar: %v", err)
	}
	if err := <-visto; err != nil {
		t.Errorf("a tarefa viu ctx.Err() = %v; deveria ser nil", err)
	}
}

// TestEmAndamentoAcompanhaAsTarefas confere o substituto do
// `tarefas.load(SeqCst)` do legado.
func TestEmAndamentoAcompanhaAsTarefas(t *testing.T) {
	p := worker.NovoPool(loggerMudo(), 0)

	if n := p.EmAndamento(); n != 0 {
		t.Errorf("EmAndamento inicial = %d", n)
	}

	dentro := make(chan struct{})
	liberar := make(chan struct{})
	if err := p.Submeter(context.Background(), context.Background(), func(context.Context) {
		close(dentro)
		<-liberar
	}); err != nil {
		t.Fatalf("Submeter: %v", err)
	}

	<-dentro
	if n := p.EmAndamento(); n != 1 {
		t.Errorf("EmAndamento com uma tarefa ativa = %d", n)
	}

	close(liberar)
	if err := p.Drenar(context.Background()); err != nil {
		t.Fatalf("Drenar: %v", err)
	}
	if n := p.EmAndamento(); n != 0 {
		t.Errorf("EmAndamento final = %d", n)
	}
}
