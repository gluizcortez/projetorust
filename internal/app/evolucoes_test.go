package app

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/gluizcortez/projetorust/internal/config"
	"github.com/gluizcortez/projetorust/internal/platform/observability"
	"github.com/gluizcortez/projetorust/internal/platform/worker"
)

// Os adaptadores das evoluções da fase F11 são curtos, e é por isso que
// precisam de teste: uma sonda de prontidão que sempre aprova é pior que
// nenhuma, e um medidor de fila que não desce dispara alarme para sempre.

// -------------------------------------------------------------------------
// seSim — a fiação condicional de TODAS as evoluções
// -------------------------------------------------------------------------

// TestSeSimDevolveZeroQuandoDesligada.
//
// Esta função é o ponto único onde "chave desligada" vira "porta nula". Um
// defeito aqui ligaria evoluções por acidente — que é a única coisa que a fase
// inteira proíbe.
func TestSeSimDevolveZeroQuandoDesligada(t *testing.T) {
	type porta interface{ Fazer() }

	if v := seSim(false, "algo"); v != "" {
		t.Errorf("seSim(false, string) = %q; esperava o zero", v)
	}
	if v := seSim(true, "algo"); v != "algo" {
		t.Errorf("seSim(true, string) = %q", v)
	}
	if v := seSim(false, 42); v != 0 {
		t.Errorf("seSim(false, int) = %d; esperava 0", v)
	}

	// O caso que interessa: uma INTERFACE não nula vira nula.
	var naoNula porta = portaDeTeste{}
	if v := seSim[porta](false, naoNula); v != nil {
		t.Error("seSim(false, interface) devolveu não nulo; a evolução ficaria ligada")
	}
	if v := seSim[porta](true, naoNula); v == nil {
		t.Error("seSim(true, interface) devolveu nulo; a evolução ficaria desligada")
	}

	// E uma função também.
	if v := seSim[func(context.Context) error](false, func(context.Context) error { return nil }); v != nil {
		t.Error("seSim(false, func) devolveu não nulo")
	}
}

type portaDeTeste struct{}

func (portaDeTeste) Fazer() {}

// -------------------------------------------------------------------------
// prontidao — /health/ready
// -------------------------------------------------------------------------

// TestProntidaoSemTetoIgnoraAFila.
//
// A fila só é sinal de saturação quando há teto. Com teto zero ninguém espera,
// e reprovar por causa dela tiraria da rotação um serviço perfeitamente sadio.
func TestProntidaoSemTetoIgnoraAFila(t *testing.T) {
	cfg := &config.Config{MaxImportacoesConcorrentes: 0}
	executor := worker.NovoPool(nil, 0)

	// Pool nulo: sem banco a verificar, sobra só a condição da fila.
	if err := prontidao(nil, executor, cfg)(context.Background()); err != nil {
		t.Errorf("prontidao reprovou sem teto e sem banco: %v", err)
	}
}

// TestProntidaoComTetoReprovaComFila.
func TestProntidaoComTetoReprovaComFila(t *testing.T) {
	cfg := &config.Config{MaxImportacoesConcorrentes: 1}
	executor := worker.NovoPool(loggerDeTeste(), 1)

	liberar := make(chan struct{})
	comecou := make(chan struct{})

	// Ocupa a única vaga.
	if err := executor.Submeter(context.Background(), context.Background(), func(context.Context) {
		close(comecou)
		<-liberar
	}); err != nil {
		t.Fatalf("Submeter: %v", err)
	}
	<-comecou

	// A próxima entra na fila.
	go func() {
		_ = executor.Submeter(context.Background(), context.Background(), func(context.Context) {})
	}()

	prazo := time.After(10 * time.Second)
	for executor.Aguardando() == 0 {
		select {
		case <-prazo:
			t.Fatal("a fila não encheu em 10s")
		default:
			time.Sleep(time.Millisecond)
		}
	}

	err := prontidao(nil, executor, cfg)(context.Background())
	if err == nil {
		t.Error("prontidao aprovou com submissão aguardando vaga")
	} else if !strings.Contains(err.Error(), "fila") {
		t.Errorf("erro = %v; deveria mencionar a fila", err)
	}

	close(liberar)
	if err := executor.Drenar(context.Background()); err != nil {
		t.Fatalf("Drenar: %v", err)
	}

	// E volta a aprovar quando a fila esvazia — uma sonda que não se recupera
	// mantém o serviço fora da rotação para sempre.
	if err := prontidao(nil, executor, cfg)(context.Background()); err != nil {
		t.Errorf("prontidao continuou reprovando com a fila vazia: %v", err)
	}
}

// -------------------------------------------------------------------------
// Instrumentação
// -------------------------------------------------------------------------

// TestFilaObservadaAlimentaOsMedidores.
func TestFilaObservadaAlimentaOsMedidores(t *testing.T) {
	m := observability.NovasMetricas()
	obs := filaObservada{m: m}

	obs.ObservarFila(3, 2)
	if v := testutil.ToFloat64(m.FilaProfundidade); v != 3 {
		t.Errorf("profundidade = %v; esperava 3", v)
	}
	if v := testutil.ToFloat64(m.ImportacoesEmAndamento); v != 2 {
		t.Errorf("em andamento = %v; esperava 2", v)
	}

	// Medidor DESCE — é a metade que costuma faltar.
	obs.ObservarFila(0, 0)
	if v := testutil.ToFloat64(m.FilaProfundidade); v != 0 {
		t.Errorf("profundidade = %v depois de esvaziar; esperava 0", v)
	}

	obs.ObservarEspera(250 * time.Millisecond)
	if n := testutil.CollectAndCount(m.FilaEspera); n != 1 {
		t.Errorf("séries de espera = %d; esperava 1", n)
	}
}

// TestMetricasDaVarreduraAlimentamOsInstrumentos.
func TestMetricasDaVarreduraAlimentamOsInstrumentos(t *testing.T) {
	m := observability.NovasMetricas()
	a := metricasDaVarredura{m: m}

	a.ContarOrfa("indexando")
	a.ContarOrfa("indexando")
	a.ContarOrfa("recortando")
	a.ObservarVarredura(3, 2, 40*time.Millisecond)

	if v := testutil.ToFloat64(m.VarreduraOrfasTotal.WithLabelValues("indexando")); v != 2 {
		t.Errorf("órfãs em indexando = %v; esperava 2", v)
	}
	if v := testutil.ToFloat64(m.VarreduraTratadasTotal); v != 2 {
		t.Errorf("tratadas = %v; esperava 2", v)
	}

	// A política "observar" não trata nada, e o contador precisa ficar parado.
	a.ObservarVarredura(5, 0, time.Millisecond)
	if v := testutil.ToFloat64(m.VarreduraTratadasTotal); v != 2 {
		t.Errorf("tratadas = %v; a passagem que não tratou nada não pode somar", v)
	}
}

// TestInstrumentacaoNulaNaoEntraEmPanico: a raiz de composição pode montar sem
// métricas, e nenhum método pode falhar por isso.
func TestInstrumentacaoNulaNaoEntraEmPanico(t *testing.T) {
	filaObservada{m: nil}.ObservarFila(1, 1)
	filaObservada{m: nil}.ObservarEspera(time.Second)
	metricasDaVarredura{m: nil}.ContarOrfa("indexando")
	metricasDaVarredura{m: nil}.ObservarVarredura(1, 1, time.Second)
}

// -------------------------------------------------------------------------
// montarVarredura
// -------------------------------------------------------------------------

// TestVarreduraDesligadaNaoEhMontada é a paridade: com a chave no padrão não
// existe laço, e portanto não existe tarefa periódica nem transação a mais.
func TestVarreduraDesligadaNaoEhMontada(t *testing.T) {
	cfg := &config.Config{VarreduraOrfas: false}

	laco, err := montarVarredura(cfg, loggerDeTeste(), nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("montarVarredura: %v", err)
	}
	if laco != nil {
		t.Error("a varredura foi montada com a chave desligada")
	}
}

// TestVarreduraLigadaComAjusteInvalidoFalhaNaMontagem.
//
// A configuração já recusa limiar zero, mas só quando a chave está ligada; esta
// é a segunda barreira, para o caso de alguém montar o App direto.
func TestVarreduraLigadaComAjusteInvalidoFalhaNaMontagem(t *testing.T) {
	cfg := &config.Config{
		VarreduraOrfas:          true,
		VarreduraOrfasIntervalo: time.Minute,
		VarreduraOrfasLimiar:    0,
		VarreduraOrfasPolitica:  "observar",
	}

	_, err := montarVarredura(cfg, loggerDeTeste(), nil, nil, nil, nil)
	if err == nil {
		t.Fatal("montarVarredura aceitou limiar zero")
	}
}

// loggerDeTeste descarta a saída: estes testes observam efeito, não registro.
func loggerDeTeste() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
