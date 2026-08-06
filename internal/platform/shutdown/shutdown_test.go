package shutdown_test

import (
	"context"
	"os"
	"syscall"
	"testing"
	"time"

	"github.com/gluizcortez/projetorust/internal/platform/shutdown"
)

// Os testes deste arquivo enviam SINAIS DE VERDADE ao próprio processo de teste.
// Não podem rodar em paralelo entre si: `signal.Notify` é global ao processo, e
// dois ouvintes simultâneos disputariam o mesmo sinal.

// enviar manda um sinal ao processo de teste.
func enviar(t *testing.T, s syscall.Signal) {
	t.Helper()
	if err := syscall.Kill(os.Getpid(), s); err != nil {
		t.Fatalf("enviando %v: %v", s, err)
	}
}

// esperar aguarda o canal fechar, ou falha por prazo.
func esperar(t *testing.T, c <-chan struct{}, oQue string) {
	t.Helper()
	select {
	case <-c:
	case <-time.After(5 * time.Second):
		t.Fatalf("%s não aconteceu em 5s", oQue)
	}
}

// TestPrimeiroSinalCancelaOContexto é o comportamento do legado: o primeiro
// SIGINT ou SIGTERM inicia o encerramento gracioso (reference/main.rs:88-110).
func TestPrimeiroSinalCancelaOContexto(t *testing.T) {
	e := shutdown.Ouvir(context.Background())
	defer e.Parar()

	select {
	case <-e.Contexto.Done():
		t.Fatal("o contexto foi cancelado antes de qualquer sinal")
	default:
	}

	enviar(t, syscall.SIGTERM)
	esperar(t, e.Contexto.Done(), "o cancelamento do contexto")

	if m := e.Motivo(); m != "sinal SIGTERM" {
		t.Errorf("motivo = %q; esperava `sinal SIGTERM`", m)
	}
	select {
	case <-e.Forcado:
		t.Error("o primeiro sinal não pode forçar a saída")
	default:
	}
}

// TestSIGINTTambemECapturado: o legado escuta os dois (§6.2).
func TestSIGINTTambemECapturado(t *testing.T) {
	e := shutdown.Ouvir(context.Background())
	defer e.Parar()

	enviar(t, syscall.SIGINT)
	esperar(t, e.Contexto.Done(), "o cancelamento do contexto")

	if m := e.Motivo(); m != "sinal SIGINT" {
		t.Errorf("motivo = %q; esperava `sinal SIGINT`", m)
	}
}

// TestSegundoSinalForca é o comportamento NOVO.
//
// O legado chama `stop_graceful(None)` no primeiro sinal e ignora os seguintes:
// diante de uma importação travada, o operador só tem SIGKILL. Aceitar o
// segundo dá a ele uma saída ordenada.
func TestSegundoSinalForca(t *testing.T) {
	e := shutdown.Ouvir(context.Background())
	defer e.Parar()

	enviar(t, syscall.SIGTERM)
	esperar(t, e.Contexto.Done(), "o cancelamento do contexto")

	enviar(t, syscall.SIGTERM)
	esperar(t, e.Forcado, "o fechamento do canal de força")

	// O motivo continua sendo o do PRIMEIRO sinal.
	if m := e.Motivo(); m != "sinal SIGTERM" {
		t.Errorf("motivo = %q", m)
	}
}

// TestSemSinalNaoHaMotivo.
func TestSemSinalNaoHaMotivo(t *testing.T) {
	e := shutdown.Ouvir(context.Background())
	defer e.Parar()

	if m := e.Motivo(); m != "" {
		t.Errorf("motivo = %q; esperava vazio", m)
	}
}

// TestCancelamentoDoPaiPropaga: o encerramento também pode vir de fora.
func TestCancelamentoDoPaiPropaga(t *testing.T) {
	pai, cancelar := context.WithCancel(context.Background())
	e := shutdown.Ouvir(pai)
	defer e.Parar()

	cancelar()
	esperar(t, e.Contexto.Done(), "a propagação do cancelamento do pai")

	// Não veio de sinal, então não há motivo.
	if m := e.Motivo(); m != "" {
		t.Errorf("motivo = %q; esperava vazio", m)
	}
}

// TestPararEIdempotente: chamar duas vezes não pode travar nem entrar em pânico.
func TestPararEIdempotente(t *testing.T) {
	e := shutdown.Ouvir(context.Background())

	pronto := make(chan struct{})
	go func() {
		defer close(pronto)
		e.Parar()
		e.Parar()
		e.Parar()
	}()
	esperar(t, pronto, "as três chamadas a Parar")
}

// TestPararDesinstalaOManipulador: depois de Parar, um sinal não pode mais
// afetar esta escuta — e, o que importa mais, não pode derrubar o processo de
// teste por falta de manipulador.
func TestPararDesinstalaOManipulador(t *testing.T) {
	e := shutdown.Ouvir(context.Background())
	e.Parar()

	// Reinstala um ouvinte para absorver o sinal: sem manipulador algum, o
	// SIGTERM mataria o processo de teste.
	guarda := shutdown.Ouvir(context.Background())
	defer guarda.Parar()

	enviar(t, syscall.SIGTERM)
	esperar(t, guarda.Contexto.Done(), "o novo ouvinte receber o sinal")

	if m := e.Motivo(); m != "" {
		t.Errorf("a escuta parada registrou o motivo %q", m)
	}
}

// TestSinaisPersonalizados confere que a lista é configurável.
func TestSinaisPersonalizados(t *testing.T) {
	e := shutdown.Ouvir(context.Background(), syscall.SIGUSR1)
	defer e.Parar()

	enviar(t, syscall.SIGUSR1)
	esperar(t, e.Contexto.Done(), "o cancelamento por SIGUSR1")

	if m := e.Motivo(); m != "sinal user defined signal 1" {
		t.Errorf("motivo = %q", m)
	}
}
