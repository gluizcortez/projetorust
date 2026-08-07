package periodico

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func loggerMudo() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// TestLacoRepete: o intervalo é curto para o teste não demorar, e a asserção é
// "pelo menos três", não "exatamente três" — contar execuções por tempo de
// relógio é a receita de teste instável.
func TestLacoRepete(t *testing.T) {
	var execucoes atomic.Int64
	pronto := make(chan struct{})
	var uma sync.Once

	laco := NovoLaco("teste", time.Millisecond, func(context.Context) error {
		if execucoes.Add(1) >= 3 {
			uma.Do(func() { close(pronto) })
		}
		return nil
	}, loggerMudo())

	laco.Iniciar(context.Background())

	select {
	case <-pronto:
	case <-time.After(5 * time.Second):
		t.Fatalf("o laço executou %d vez(es) em 5s", execucoes.Load())
	}

	ctx, cancelar := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelar()
	if err := laco.Parar(ctx); err != nil {
		t.Errorf("Parar: %v", err)
	}
}

// TestPrimeiraExecucaoEsperaUmIntervalo.
//
// Durante o arranque o serviço ainda está subindo; uma varredura no primeiro
// segundo compete por conexões com as importações que estão sendo aceitas.
func TestPrimeiraExecucaoEsperaUmIntervalo(t *testing.T) {
	var execucoes atomic.Int64

	laco := NovoLaco("teste", time.Hour, func(context.Context) error {
		execucoes.Add(1)
		return nil
	}, loggerMudo())

	laco.Iniciar(context.Background())
	// Um intervalo de uma hora: nada pode ter rodado ainda.
	time.Sleep(20 * time.Millisecond)

	if n := execucoes.Load(); n != 0 {
		t.Errorf("execuções = %d; a primeira só acontece depois de um intervalo", n)
	}

	ctx, cancelar := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelar()
	if err := laco.Parar(ctx); err != nil {
		t.Errorf("Parar: %v", err)
	}
}

// TestErroNaoEncerraOLaco: uma indisponibilidade momentânea do banco não pode
// desligar a varredura até o próximo reinício do processo.
func TestErroNaoEncerraOLaco(t *testing.T) {
	var execucoes atomic.Int64
	pronto := make(chan struct{})
	var uma sync.Once

	laco := NovoLaco("teste", time.Millisecond, func(context.Context) error {
		if execucoes.Add(1) >= 3 {
			uma.Do(func() { close(pronto) })
		}
		return errors.New("falha de propósito")
	}, loggerMudo())

	laco.Iniciar(context.Background())

	select {
	case <-pronto:
	case <-time.After(5 * time.Second):
		t.Fatalf("o laço parou após %d execução(ões) com erro", execucoes.Load())
	}

	ctx, cancelar := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelar()
	_ = laco.Parar(ctx)
}

// TestPararAguardaAPassagemEmCurso é o comportamento que o encerramento
// depende: sem ele, o pool de conexões fecharia debaixo de uma transação
// aberta.
func TestPararAguardaAPassagemEmCurso(t *testing.T) {
	comecou := make(chan struct{})
	liberar := make(chan struct{})
	var terminou atomic.Bool
	var primeira sync.Once

	// Só a PRIMEIRA passagem bloqueia. Depois que Parar libera, o temporizador
	// ainda pode disparar mais uma vez antes de o laço ver o sinal de parada, e
	// uma segunda tentativa de fechar os canais entraria em pânico.
	laco := NovoLaco("teste", time.Millisecond, func(context.Context) error {
		primeira.Do(func() {
			close(comecou)
			<-liberar
			terminou.Store(true)
		})
		return nil
	}, loggerMudo())

	laco.Iniciar(context.Background())
	<-comecou

	parou := make(chan error, 1)
	go func() {
		ctx, cancelar := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancelar()
		parou <- laco.Parar(ctx)
	}()

	// Parar não pode ter retornado enquanto a passagem não terminou.
	select {
	case err := <-parou:
		t.Fatalf("Parar retornou (%v) com a passagem ainda em curso", err)
	case <-time.After(50 * time.Millisecond):
	}

	close(liberar)

	select {
	case err := <-parou:
		if err != nil {
			t.Errorf("Parar: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Parar não retornou depois de a passagem terminar")
	}
	if !terminou.Load() {
		t.Error("Parar retornou antes de a passagem concluir")
	}
}

// TestPararRespeitaOPrazo: uma passagem travada não pode segurar o
// encerramento para sempre.
func TestPararRespeitaOPrazo(t *testing.T) {
	comecou := make(chan struct{})
	travar := make(chan struct{})
	defer close(travar)
	var primeira sync.Once

	laco := NovoLaco("teste", time.Millisecond, func(context.Context) error {
		primeira.Do(func() {
			close(comecou)
			<-travar
		})
		return nil
	}, loggerMudo())

	laco.Iniciar(context.Background())
	<-comecou

	ctx, cancelar := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancelar()

	if err := laco.Parar(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("Parar devolveu %v; esperava o prazo estourado", err)
	}
}

// TestPararSemIniciarNaoTrava.
func TestPararSemIniciarNaoTrava(t *testing.T) {
	laco := NovoLaco("teste", time.Hour, func(context.Context) error { return nil }, loggerMudo())

	ctx, cancelar := context.WithTimeout(context.Background(), time.Second)
	defer cancelar()
	if err := laco.Parar(ctx); err != nil {
		t.Errorf("Parar de um laço nunca iniciado devolveu %v", err)
	}

	// E um Iniciar POSTERIOR não pode disparar um laço que ninguém pararia.
	laco.Iniciar(context.Background())
	time.Sleep(10 * time.Millisecond)
}

// TestPararDuasVezesEhSeguro: o encerramento pode ser reentrante.
func TestPararDuasVezesEhSeguro(t *testing.T) {
	laco := NovoLaco("teste", time.Millisecond, func(context.Context) error { return nil }, loggerMudo())
	laco.Iniciar(context.Background())

	ctx, cancelar := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelar()
	for i := range 3 {
		if err := laco.Parar(ctx); err != nil {
			t.Errorf("Parar %d: %v", i, err)
		}
	}
}

// TestIniciarDuasVezesNaoDisparaDoisLacos.
func TestIniciarDuasVezesNaoDisparaDoisLacos(t *testing.T) {
	var execucoes atomic.Int64
	laco := NovoLaco("teste", 5*time.Millisecond, func(context.Context) error {
		execucoes.Add(1)
		return nil
	}, loggerMudo())

	for range 5 {
		laco.Iniciar(context.Background())
	}
	time.Sleep(120 * time.Millisecond)

	ctx, cancelar := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelar()
	_ = laco.Parar(ctx)

	// Cinco laços em 120 ms a cada 5 ms dariam ~120 execuções; um só dá ~24.
	// O limite generoso evita depender da precisão do temporizador.
	if n := execucoes.Load(); n > 60 {
		t.Errorf("execuções = %d; parece haver mais de um laço rodando", n)
	}
}

// TestContextoCanceladoEncerraOLaco.
func TestContextoCanceladoEncerraOLaco(t *testing.T) {
	laco := NovoLaco("teste", time.Millisecond, func(context.Context) error { return nil }, loggerMudo())

	ctx, cancelar := context.WithCancel(context.Background())
	laco.Iniciar(ctx)
	cancelar()

	espera, cancelarEspera := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelarEspera()
	if err := laco.Parar(espera); err != nil {
		t.Errorf("Parar: %v", err)
	}
}
