package shutdown

import (
	"context"
	"os"
	"os/signal"
	"sync"
	"sync/atomic"
	"syscall"
)

// SinaisPadrao são os que o legado escuta (reference/main.rs:88-110).
//
// `os.Interrupt` é o SIGINT; o legado imprime `>>>>> <CTRL>-C recebido` para um
// e `>>>>> Kill recebido` para o outro, com `println!` em vez do subsistema de
// registro (docs/ESPECIFICACAO.md §6.2). Aqui os dois viram evento estruturado.
var SinaisPadrao = []os.Signal{os.Interrupt, syscall.SIGTERM}

// Escuta acompanha os sinais de encerramento.
//
// Distingue o PRIMEIRO sinal, que inicia o encerramento gracioso, do SEGUNDO,
// que força a saída. O legado não tem essa distinção: ele chama
// `stop_graceful(None)` no primeiro e ignora os seguintes, então um operador
// diante de uma importação travada não tem como desistir da espera senão com
// SIGKILL.
//
// Aceitar o segundo sinal é comportamento NOVO, e só é observável em situação
// de emergência — quando o operador já decidiu que não vai esperar. Ver
// docs/CONTEXT.md, fase F10.
type Escuta struct {
	// Contexto é cancelado no PRIMEIRO sinal.
	Contexto context.Context //nolint:containedctx // é o produto do tipo, não uma dependência escondida

	// Forcado é fechado no SEGUNDO sinal.
	Forcado <-chan struct{}

	motivo   atomic.Pointer[string]
	parar    func()
	recebido chan os.Signal
	forcado  chan struct{}
	pronto   chan struct{}

	// encerrar sinaliza à goroutine de escuta que ela deve terminar.
	//
	// É um canal PRÓPRIO, e não o `Done` do contexto, porque o contexto é
	// cancelado pelo PRIMEIRO sinal — e a escuta precisa continuar viva
	// justamente depois disso, para receber o segundo.
	encerrar chan struct{}
	uma      sync.Once
}

// Ouvir instala os manipuladores de sinal.
//
// O chamador DEVE invocar Parar quando terminar, para desinstalar os
// manipuladores e liberar a goroutine de escuta.
func Ouvir(pai context.Context, sinais ...os.Signal) *Escuta {
	if len(sinais) == 0 {
		sinais = SinaisPadrao
	}

	ctx, cancelar := context.WithCancel(pai)
	e := &Escuta{
		Contexto: ctx,
		// Capacidade 2: um sinal para iniciar o encerramento e outro para
		// forçá-lo. Sem folga, o segundo sinal poderia ser descartado pelo
		// runtime justamente quando o operador mais precisa dele.
		recebido: make(chan os.Signal, 2),
		forcado:  make(chan struct{}),
		pronto:   make(chan struct{}),
		encerrar: make(chan struct{}),
	}
	e.Forcado = e.forcado

	signal.Notify(e.recebido, sinais...)

	e.parar = func() {
		e.uma.Do(func() {
			signal.Stop(e.recebido)
			close(e.encerrar)
		})
		cancelar()
		<-e.pronto
	}

	go func() {
		defer close(e.pronto)
		for {
			select {
			case s := <-e.recebido:
				if e.motivo.CompareAndSwap(nil, descrever(s)) {
					// Primeiro sinal: inicia o encerramento gracioso e SEGUE
					// escutando. Sair aqui — ou selecionar em `ctx.Done()`, que
					// acabou de fechar — perderia o segundo sinal.
					cancelar()
					continue
				}
				// Segundo sinal: força.
				close(e.forcado)
				return
			case <-e.encerrar:
				return
			}
		}
	}()

	return e
}

// Parar desinstala os manipuladores e aguarda a goroutine de escuta terminar.
//
// É idempotente por construção: `signal.Stop` e `cancelar` toleram repetição, e
// a espera por `pronto` retorna de imediato depois da primeira vez.
func (e *Escuta) Parar() { e.parar() }

// Motivo devolve a descrição do primeiro sinal recebido, ou a cadeia vazia se
// o encerramento não veio de sinal.
func (e *Escuta) Motivo() string {
	if m := e.motivo.Load(); m != nil {
		return *m
	}
	return ""
}

// descrever nomeia o sinal para o registro.
func descrever(s os.Signal) *string {
	var nome string
	switch s {
	case os.Interrupt:
		nome = "sinal SIGINT"
	case syscall.SIGTERM:
		nome = "sinal SIGTERM"
	default:
		nome = "sinal " + s.String()
	}
	return &nome
}
