package worker

import (
	"context"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"
)

// O observador da fila é a fonte das métricas de MAX_IMPORTACOES_CONCORRENTES.
// Sem teste, um rótulo trocado ou uma notificação esquecida vira um painel que
// mostra fila zero enquanto o serviço engasga — pior que não ter painel.

func loggerSilencioso() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// observadorGravado registra tudo o que o pool notificou.
type observadorGravado struct {
	mu       sync.Mutex
	filas    [][2]int64
	esperas  []time.Duration
	maiorFil int64
}

func (o *observadorGravado) ObservarFila(aguardando, emAndamento int64) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.filas = append(o.filas, [2]int64{aguardando, emAndamento})
	if aguardando > o.maiorFil {
		o.maiorFil = aguardando
	}
}

func (o *observadorGravado) ObservarEspera(d time.Duration) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.esperas = append(o.esperas, d)
}

func (o *observadorGravado) resumo() (notificacoes, amostras int, maiorFila int64) {
	o.mu.Lock()
	defer o.mu.Unlock()
	return len(o.filas), len(o.esperas), o.maiorFil
}

// TestSemTetoNaoProduzAmostraDeEspera é a paridade.
//
// Sem MAX_IMPORTACOES_CONCORRENTES ninguém espera, e a série de espera precisa
// ficar VAZIA — não cheia de zeros. Uma amostra de zero por submissão sugeriria
// fila onde não há, e faria qualquer percentil da série mentir.
func TestSemTetoNaoProduzAmostraDeEspera(t *testing.T) {
	obs := &observadorGravado{}
	p := NovoPool(loggerSilencioso(), 0, ComObservador(obs))

	for range 50 {
		if err := p.Submeter(context.Background(), context.Background(), func(context.Context) {}); err != nil {
			t.Fatalf("Submeter: %v", err)
		}
	}
	if err := p.Drenar(context.Background()); err != nil {
		t.Fatalf("Drenar: %v", err)
	}

	notificacoes, amostras, maiorFila := obs.resumo()
	if amostras != 0 {
		t.Errorf("amostras de espera = %d; sem teto ninguém espera", amostras)
	}
	if maiorFila != 0 {
		t.Errorf("maior fila = %d; sem teto a fila é sempre zero", maiorFila)
	}
	// As notificações de fila continuam saindo — elas também carregam
	// `emAndamento`, que é observável com ou sem teto.
	if notificacoes == 0 {
		t.Error("nenhuma notificação de fila; emAndamento deixou de ser observável")
	}
}

// TestComTetoRegistraFilaEEspera.
//
// A barreira é o que torna o teste determinístico: sem ela, dependeríamos de o
// escalonador colocar duas goroutines em espera ao mesmo tempo — que ele não
// garante, e foi a causa de uma instabilidade na fase F10.
func TestComTetoRegistraFilaEEspera(t *testing.T) {
	obs := &observadorGravado{}
	p := NovoPool(loggerSilencioso(), 1, ComObservador(obs))

	liberar := make(chan struct{})
	primeira := make(chan struct{})

	// Ocupa a única vaga e a segura.
	var uma sync.Once
	if err := p.Submeter(context.Background(), context.Background(), func(context.Context) {
		uma.Do(func() { close(primeira) })
		<-liberar
	}); err != nil {
		t.Fatalf("Submeter: %v", err)
	}
	<-primeira

	// As próximas cinco NÃO têm vaga: cada uma entra na fila.
	var submetidas sync.WaitGroup
	for range 5 {
		submetidas.Add(1)
		go func() {
			defer submetidas.Done()
			_ = p.Submeter(context.Background(), context.Background(), func(context.Context) {})
		}()
	}

	// Espera a fila encher de fato antes de liberar.
	prazo := time.After(10 * time.Second)
	for p.Aguardando() < 5 {
		select {
		case <-prazo:
			t.Fatalf("a fila chegou a %d; esperava 5", p.Aguardando())
		default:
			time.Sleep(time.Millisecond)
		}
	}

	close(liberar)
	submetidas.Wait()
	if err := p.Drenar(context.Background()); err != nil {
		t.Fatalf("Drenar: %v", err)
	}

	_, amostras, maiorFila := obs.resumo()
	if amostras != 5 {
		t.Errorf("amostras de espera = %d; as cinco que esperaram deviam produzir uma cada", amostras)
	}
	if maiorFila < 5 {
		t.Errorf("maior fila observada = %d; a fila chegou a 5", maiorFila)
	}
	// E o estado final tem de voltar a zero: um medidor que não desce é pior
	// que nenhum, porque dispara alarme para sempre.
	if p.Aguardando() != 0 || p.EmAndamento() != 0 {
		t.Errorf("estado final: aguardando=%d emAndamento=%d; esperava zero em ambos",
			p.Aguardando(), p.EmAndamento())
	}
}

// TestObservadorNuloEhOPadrao: quem monta o pool sem instrumentação não pode
// receber pânico por ponteiro nulo.
func TestObservadorNuloEhOPadrao(t *testing.T) {
	p := NovoPool(loggerSilencioso(), 2)
	if err := p.Submeter(context.Background(), context.Background(), func(context.Context) {}); err != nil {
		t.Fatalf("Submeter: %v", err)
	}
	if err := p.Drenar(context.Background()); err != nil {
		t.Fatalf("Drenar: %v", err)
	}

	// E ComObservador(nil) não pode SUBSTITUIR o padrão por um nulo.
	p2 := NovoPool(loggerSilencioso(), 2, ComObservador(nil))
	if err := p2.Submeter(context.Background(), context.Background(), func(context.Context) {}); err != nil {
		t.Fatalf("Submeter: %v", err)
	}
	if err := p2.Drenar(context.Background()); err != nil {
		t.Fatalf("Drenar: %v", err)
	}
}

// TestNotificacaoFinalNaoVeEstadoIntermediario.
//
// A última notificação de cada tarefa sai DEPOIS de o contador cair e a vaga
// voltar. Se saísse antes, o medidor terminaria em um valor diferente de zero e
// o painel mostraria trabalho que não existe.
func TestNotificacaoFinalNaoVeEstadoIntermediario(t *testing.T) {
	obs := &observadorGravado{}
	p := NovoPool(loggerSilencioso(), 2, ComObservador(obs))

	for range 10 {
		if err := p.Submeter(context.Background(), context.Background(), func(context.Context) {}); err != nil {
			t.Fatalf("Submeter: %v", err)
		}
	}
	if err := p.Drenar(context.Background()); err != nil {
		t.Fatalf("Drenar: %v", err)
	}

	obs.mu.Lock()
	defer obs.mu.Unlock()
	if len(obs.filas) == 0 {
		t.Fatal("nenhuma notificação")
	}
	ultima := obs.filas[len(obs.filas)-1]
	if ultima != [2]int64{0, 0} {
		t.Errorf("última notificação = aguardando %d, emAndamento %d; esperava zero em ambos",
			ultima[0], ultima[1])
	}
}
