package app_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"go.uber.org/goleak"

	"github.com/gluizcortez/projetorust/internal/adapter/httpapi"
	"github.com/gluizcortez/projetorust/internal/app"
	"github.com/gluizcortez/projetorust/internal/config"
	"github.com/gluizcortez/projetorust/internal/platform/worker"
)

// TestMain instala o detector de goroutines remanescentes.
//
// É o critério de aceite "goleak não detecta goroutine remanescente após
// Executar retornar" — e aplicado ao pacote inteiro, não a um teste só, o que é
// mais forte: qualquer teste que deixe goroutine viva derruba a execução.
//
// As duas exceções são do próprio ferramental: o coletor de métricas do
// OpenTelemetry e a goroutine de leitura ociosa do net/http, que não pertencem
// ao código sob teste.
func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m,
		goleak.IgnoreTopFunction("go.opencensus.io/stats/view.(*worker).start"),
		goleak.IgnoreAnyFunction("net/http.(*persistConn).readLoop"),
		goleak.IgnoreAnyFunction("net/http.(*persistConn).writeLoop"),
	)
}

// -------------------------------------------------------------------------
// Montagem
// -------------------------------------------------------------------------

func configDeTeste(t *testing.T) *config.Config {
	t.Helper()
	t.Setenv("DATABASE_URL", "postgres://u:s@localhost:1/d")
	t.Setenv("API_KEY", "chave-de-teste")
	t.Setenv("SERVIDOR_IP", "127.0.0.1")
	t.Setenv("SERVIDOR_PORTA", "0")

	cfg, err := config.Carregar(context.Background())
	if err != nil {
		t.Fatalf("Carregar: %v", err)
	}
	return cfg
}

// registroEmBuffer devolve um registrador e o leitor do que ele escreveu.
func registroEmBuffer() (*slog.Logger, func() string) {
	var mu sync.Mutex
	buf := &bytes.Buffer{}
	w := escritorSincronizado{mu: &mu, buf: buf}
	return slog.New(slog.NewTextHandler(w, &slog.HandlerOptions{Level: slog.LevelDebug})),
		func() string {
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

// servico é o App montado com peças de teste: servidor HTTP real, executor
// real, SEM banco e SEM telemetria.
//
// Sem banco é deliberado: o que esta fase precisa exercitar é sinal, drenagem e
// ordem de encerramento, e um PostgreSQL no meio só tornaria o teste lento e
// frágil sem medir nada a mais.
type servico struct {
	app      *app.App
	logger   *slog.Logger
	conteudo func() string
	executor *worker.Pool
	cfg      *config.Config
}

func montar(t *testing.T, manipulador http.Handler, ajustar func(*config.Config)) *servico {
	t.Helper()

	cfg := configDeTeste(t)
	if ajustar != nil {
		ajustar(cfg)
	}
	logger, conteudo := registroEmBuffer()
	executor := worker.NovoPool(logger, cfg.MaxImportacoesConcorrentes)

	if manipulador == nil {
		manipulador = http.NotFoundHandler()
	}
	servidor := httpapi.NovoServidor(manipulador, httpapi.PadroesDoServidor("127.0.0.1:0"))

	a, err := app.Montar(app.Dependencias{
		Config:   cfg,
		Logger:   logger,
		Servidor: servidor,
		Executor: executor,
		Versao:   "teste",
		Revisao:  "abc123",
	})
	if err != nil {
		t.Fatalf("Montar: %v", err)
	}
	return &servico{app: a, logger: logger, conteudo: conteudo, executor: executor, cfg: cfg}
}

// executarEmSegundoPlano roda App.Executar e devolve um canal com o resultado.
func (s *servico) executarEmSegundoPlano(
	ctx context.Context, forcar <-chan struct{},
) <-chan error {
	resultado := make(chan error, 1)
	go func() {
		resultado <- s.app.Executar(ctx, forcar)
	}()
	return resultado
}

// esperarEscutando aguarda o servidor aceitar conexão.
func (s *servico) esperarEscutando(t *testing.T) string {
	t.Helper()
	limite := time.After(5 * time.Second)
	for {
		select {
		case <-limite:
			t.Fatal("o servidor não começou a escutar em 5s")
		default:
		}
		endereco := s.app.Endereco()
		if endereco != "" && !strings.HasSuffix(endereco, ":0") {
			c, err := net.DialTimeout("tcp", endereco, 200*time.Millisecond)
			if err == nil {
				_ = c.Close()
				return endereco
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func colher(t *testing.T, c <-chan error, prazo time.Duration) error {
	t.Helper()
	select {
	case err := <-c:
		return err
	case <-time.After(prazo):
		t.Fatalf("Executar não retornou em %s", prazo)
		return nil
	}
}

// -------------------------------------------------------------------------
// Critérios de aceite
// -------------------------------------------------------------------------

// TestSinalEsperaAImportacaoEmAndamento é o critério de aceite principal: com
// uma importação em curso, o processo ESPERA antes de sair.
//
// Reproduz reference/main.rs:71-74, onde o `main` só termina depois de o
// contador de tarefas chegar a zero — só que por notificação, não por sondagem.
func TestSinalEsperaAImportacaoEmAndamento(t *testing.T) {
	s := montar(t, nil, nil)

	const duracaoDaImportacao = 2 * time.Second
	var (
		inicioDaTarefa time.Time
		concluiu       = make(chan struct{})
	)
	if err := s.executor.Submeter(context.Background(), context.Background(),
		func(context.Context) {
			inicioDaTarefa = time.Now()
			time.Sleep(duracaoDaImportacao)
			close(concluiu)
		}); err != nil {
		t.Fatalf("Submeter: %v", err)
	}

	ctx, sinal := context.WithCancel(context.Background())
	resultado := s.executarEmSegundoPlano(ctx, nil)
	s.esperarEscutando(t)

	sinal() // equivale ao SIGTERM

	if err := colher(t, resultado, 30*time.Second); err != nil {
		t.Fatalf("Executar: %v", err)
	}
	retorno := time.Now()

	select {
	case <-concluiu:
	default:
		t.Fatal("Executar retornou antes de a importação terminar")
	}
	// A medida é do início REAL da tarefa, não do sinal: a tarefa foi submetida
	// antes de Executar subir, e medir do sinal descontaria essa folga.
	if esperado := inicioDaTarefa.Add(duracaoDaImportacao); retorno.Before(esperado) {
		t.Errorf("Executar retornou %v antes de a importação poder ter terminado",
			esperado.Sub(retorno))
	}
	t.Logf("esperou %v pela importação de %v", retorno.Sub(inicioDaTarefa), duracaoDaImportacao)
}

// TestRequisicaoEmCursoEConcluidaENovaERecusada é o segundo critério: o que já
// estava em curso termina com 200; o que chega depois do sinal é recusado.
func TestRequisicaoEmCursoEConcluidaENovaERecusada(t *testing.T) {
	entrou := make(chan struct{})
	liberar := make(chan struct{})

	manipulador := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/demorada" {
			close(entrou)
			<-liberar
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("pronto"))
	})

	s := montar(t, manipulador, nil)
	ctx, sinal := context.WithCancel(context.Background())
	resultado := s.executarEmSegundoPlano(ctx, nil)
	endereco := s.esperarEscutando(t)

	// Requisição em curso.
	respostaEmCurso := make(chan int, 1)
	go func() {
		cliente := &http.Client{Timeout: 30 * time.Second}
		resp, err := cliente.Get("http://" + endereco + "/demorada")
		if err != nil {
			respostaEmCurso <- -1
			return
		}
		defer func() { _ = resp.Body.Close() }()
		_, _ = io.Copy(io.Discard, resp.Body)
		respostaEmCurso <- resp.StatusCode
	}()

	<-entrou
	sinal() // SIGTERM com a requisição no meio

	// Dar tempo ao Shutdown de fechar o ouvinte antes da segunda tentativa.
	time.Sleep(200 * time.Millisecond)

	// Nova requisição: o ouvinte já não aceita.
	cliente := &http.Client{Timeout: 3 * time.Second}
	if resp, err := cliente.Get("http://" + endereco + "/nova"); err == nil {
		_ = resp.Body.Close()
		t.Errorf("a requisição nova foi atendida com %d; deveria ter sido recusada", resp.StatusCode)
	}

	close(liberar)

	if codigo := <-respostaEmCurso; codigo != http.StatusOK {
		t.Errorf("a requisição em curso terminou com %d; esperava 200", codigo)
	}
	if err := colher(t, resultado, 30*time.Second); err != nil {
		t.Fatalf("Executar: %v", err)
	}
}

// TestTetoDeEncerramentoEstourado é o terceiro critério: com SHUTDOWN_TIMEOUT
// definido e uma importação mais longa, o encerramento desiste e informa o que
// ficou pendente.
//
// Com o padrão ZERO isso NÃO acontece — a espera é indefinida, como no legado.
func TestTetoDeEncerramentoEstourado(t *testing.T) {
	s := montar(t, nil, func(c *config.Config) {
		c.ShutdownTimeout = 500 * time.Millisecond
	})

	presa := make(chan struct{})
	defer close(presa)
	if err := s.executor.Submeter(context.Background(), context.Background(),
		func(context.Context) { <-presa }); err != nil {
		t.Fatalf("Submeter: %v", err)
	}

	ctx, sinal := context.WithCancel(context.Background())
	resultado := s.executarEmSegundoPlano(ctx, nil)
	s.esperarEscutando(t)
	sinal()

	err := colher(t, resultado, 30*time.Second)
	if !errors.Is(err, app.ErrTetoDeEncerramento) {
		t.Fatalf("erro = %v; esperava ErrTetoDeEncerramento", err)
	}
	if !strings.Contains(err.Error(), "1 importação") {
		t.Errorf("o erro deveria dizer quantas ficaram pendentes: %v", err)
	}

	registro := s.conteudo()
	if !strings.Contains(registro, "teto de encerramento estourado") {
		t.Errorf("o registro não trouxe o estouro:\n%s", registro)
	}
	if !strings.Contains(registro, "importacoes_pendentes=1") {
		t.Errorf("o registro não trouxe as pendentes:\n%s", registro)
	}
}

// TestTetoZeroEsperaIndefinidamente é a paridade: o padrão reproduz o legado.
func TestTetoZeroEsperaIndefinidamente(t *testing.T) {
	s := montar(t, nil, nil)
	if s.cfg.ShutdownTimeout != 0 {
		t.Fatalf("SHUTDOWN_TIMEOUT padrão = %v; deveria ser zero", s.cfg.ShutdownTimeout)
	}

	liberar := make(chan struct{})
	if err := s.executor.Submeter(context.Background(), context.Background(),
		func(context.Context) { <-liberar }); err != nil {
		t.Fatalf("Submeter: %v", err)
	}

	ctx, sinal := context.WithCancel(context.Background())
	resultado := s.executarEmSegundoPlano(ctx, nil)
	s.esperarEscutando(t)
	sinal()

	// Um segundo sem retornar demonstra a espera; o legado esperaria para
	// sempre, e o teste não pode fazer isso.
	select {
	case err := <-resultado:
		t.Fatalf("Executar retornou (%v) enquanto havia importação em andamento", err)
	case <-time.After(time.Second):
	}

	close(liberar)
	if err := colher(t, resultado, 30*time.Second); err != nil {
		t.Fatalf("Executar: %v", err)
	}
}

// TestSegundoSinalForcaASaida é o quarto critério.
func TestSegundoSinalForcaASaida(t *testing.T) {
	s := montar(t, nil, nil)

	presa := make(chan struct{})
	defer close(presa)
	if err := s.executor.Submeter(context.Background(), context.Background(),
		func(context.Context) { <-presa }); err != nil {
		t.Fatalf("Submeter: %v", err)
	}

	ctx, sinal := context.WithCancel(context.Background())
	forcar := make(chan struct{})
	resultado := s.executarEmSegundoPlano(ctx, forcar)
	s.esperarEscutando(t)

	sinal() // primeiro sinal: começa a drenar e fica preso na importação

	select {
	case err := <-resultado:
		t.Fatalf("Executar retornou cedo demais: %v", err)
	case <-time.After(300 * time.Millisecond):
	}

	close(forcar) // segundo sinal

	err := colher(t, resultado, 10*time.Second)
	if !errors.Is(err, app.ErrEncerramentoForcado) {
		t.Fatalf("erro = %v; esperava ErrEncerramentoForcado", err)
	}
	if registro := s.conteudo(); !strings.Contains(registro, "forçado por segundo sinal") {
		t.Errorf("o registro não trouxe a força:\n%s", registro)
	}
}

// TestSegundoSinalAntesDoPrimeiroEncerramento cobre o caso em que o canal já
// está fechado quando Executar chega ao select.
func TestSegundoSinalAntesDoPrimeiroEncerramento(t *testing.T) {
	s := montar(t, nil, nil)

	forcar := make(chan struct{})
	close(forcar)

	resultado := s.executarEmSegundoPlano(context.Background(), forcar)

	err := colher(t, resultado, 10*time.Second)
	if !errors.Is(err, app.ErrEncerramentoForcado) {
		t.Fatalf("erro = %v; esperava ErrEncerramentoForcado", err)
	}
	// O servidor precisa ter sido encerrado assim mesmo — a saída forçada não
	// pode deixar o ouvinte aberto.
	if _, err := net.DialTimeout("tcp", s.app.Endereco(), 200*time.Millisecond); err == nil {
		t.Error("o ouvinte continuou aceitando conexões depois da saída forçada")
	}
}

// -------------------------------------------------------------------------
// Ordem de encerramento
// -------------------------------------------------------------------------

// TestOrdemDeEncerramento fixa a sequência declarada em ESPECIFICACAO §6.3.
//
// A ordem é normativa e tem razão: o servidor HTTP para PRIMEIRO para que
// nenhuma importação nova entre enquanto as antigas drenam. Trocar as duas
// primeiras etapas produz uma drenagem que pode nunca terminar sob carga.
func TestOrdemDeEncerramento(t *testing.T) {
	var (
		mu     sync.Mutex
		etapas []string
	)
	anotar := func(nome string) {
		mu.Lock()
		defer mu.Unlock()
		etapas = append(etapas, nome)
	}

	s := montar(t, nil, nil)

	// A telemetria registra sua própria etapa.
	a, err := app.Montar(app.Dependencias{
		Config:   s.cfg,
		Logger:   s.logger,
		Servidor: httpapi.NovoServidor(http.NotFoundHandler(), httpapi.PadroesDoServidor("127.0.0.1:0")),
		Executor: s.executor,
		EncerrarTracing: func(context.Context) error {
			anotar("telemetria")
			return nil
		},
	})
	if err != nil {
		t.Fatalf("Montar: %v", err)
	}

	// Uma importação que anota quando termina, para posicioná-la na sequência.
	liberar := make(chan struct{})
	if err := s.executor.Submeter(context.Background(), context.Background(),
		func(context.Context) {
			<-liberar
			anotar("importação concluída")
		}); err != nil {
		t.Fatalf("Submeter: %v", err)
	}

	ctx, sinal := context.WithCancel(context.Background())
	resultado := make(chan error, 1)
	go func() { resultado <- a.Executar(ctx, nil) }()

	// Espera o ouvinte subir.
	limite := time.After(5 * time.Second)
	for a.Endereco() == "" || strings.HasSuffix(a.Endereco(), ":0") {
		select {
		case <-limite:
			t.Fatal("o servidor não subiu")
		default:
			time.Sleep(5 * time.Millisecond)
		}
	}

	sinal()
	time.Sleep(200 * time.Millisecond) // o servidor já parou; a drenagem espera
	anotar("antes de liberar a importação")
	close(liberar)

	if err := colher(t, resultado, 30*time.Second); err != nil {
		t.Fatalf("Executar: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	esperada := []string{"antes de liberar a importação", "importação concluída", "telemetria"}
	if len(etapas) != len(esperada) {
		t.Fatalf("etapas = %v; esperava %v", etapas, esperada)
	}
	for i := range esperada {
		if etapas[i] != esperada[i] {
			t.Errorf("etapa %d = %q; esperava %q", i, etapas[i], esperada[i])
		}
	}

	// E o registro traz cada etapa com a própria duração.
	registro := s.conteudo()
	for _, etapa := range []string{"servidor http", "importações", "telemetria"} {
		if !strings.Contains(registro, `etapa=`+etapa) &&
			!strings.Contains(registro, `etapa="`+etapa+`"`) {
			t.Errorf("o registro não trouxe a etapa %q:\n%s", etapa, registro)
		}
	}
}

// TestEtapaQueFalhaNaoInterrompeAsSeguintes: fechar o pool importa mesmo que a
// drenagem tenha estourado, e esvaziar a telemetria importa mesmo assim.
func TestEtapaQueFalhaNaoInterrompeAsSeguintes(t *testing.T) {
	cfg := configDeTeste(t)
	logger, conteudo := registroEmBuffer()
	executor := worker.NovoPool(logger, 0)

	telemetriaEsvaziada := false
	a, err := app.Montar(app.Dependencias{
		Config:   cfg,
		Logger:   logger,
		Servidor: httpapi.NovoServidor(http.NotFoundHandler(), httpapi.PadroesDoServidor("127.0.0.1:0")),
		Executor: executor,
		EncerrarTracing: func(context.Context) error {
			telemetriaEsvaziada = true
			return errors.New("falha ao esvaziar")
		},
	})
	if err != nil {
		t.Fatalf("Montar: %v", err)
	}

	err = a.Encerrar(context.Background(), "teste", nil)
	if err == nil {
		t.Fatal("esperava o erro da telemetria")
	}
	if !telemetriaEsvaziada {
		t.Error("a telemetria não foi esvaziada")
	}
	if !strings.Contains(err.Error(), "falha ao esvaziar") {
		t.Errorf("erro = %v", err)
	}
	if registro := conteudo(); !strings.Contains(registro, "etapa de encerramento falhou") {
		t.Errorf("a falha não foi registrada:\n%s", registro)
	}
}

// -------------------------------------------------------------------------
// Montagem e arranque
// -------------------------------------------------------------------------

// TestMontarExigeDependencias.
func TestMontarExigeDependencias(t *testing.T) {
	cfg := configDeTeste(t)
	completo := app.Dependencias{
		Config:   cfg,
		Logger:   slog.New(slog.NewTextHandler(io.Discard, nil)),
		Servidor: httpapi.NovoServidor(http.NotFoundHandler(), httpapi.PadroesDoServidor("127.0.0.1:0")),
		Executor: worker.NovoPool(slog.New(slog.NewTextHandler(io.Discard, nil)), 0),
	}
	if _, err := app.Montar(completo); err != nil {
		t.Fatalf("Montar completo: %v", err)
	}

	for nome, remover := range map[string]func(*app.Dependencias){
		"Config":   func(d *app.Dependencias) { d.Config = nil },
		"Logger":   func(d *app.Dependencias) { d.Logger = nil },
		"Servidor": func(d *app.Dependencias) { d.Servidor = nil },
		"Executor": func(d *app.Dependencias) { d.Executor = nil },
	} {
		t.Run("sem "+nome, func(t *testing.T) {
			d := completo
			remover(&d)
			_, err := app.Montar(d)
			if err == nil || !strings.Contains(err.Error(), nome) {
				t.Errorf("erro = %v; esperava mencionar %s", err, nome)
			}
		})
	}
}

// TestArranqueRegistraVersaoEConfiguracao: o registro de arranque reproduz
// `Servidor iniciando em: {servidor}` (main.rs:66), com os campos como
// atributos.
func TestArranqueRegistraVersaoEConfiguracao(t *testing.T) {
	s := montar(t, nil, nil)

	ctx, sinal := context.WithCancel(context.Background())
	resultado := s.executarEmSegundoPlano(ctx, nil)
	s.esperarEscutando(t)
	sinal()
	if err := colher(t, resultado, 30*time.Second); err != nil {
		t.Fatalf("Executar: %v", err)
	}

	registro := s.conteudo()
	// `Config` satisfaz slog.LogValuer devolvendo um GRUPO, então o manipulador
	// de texto o achata em `config.campo=valor` — não existe um `config=`.
	for _, esperado := range []string{
		"servidor iniciando", "versao=teste", "revisao=abc123", "endereco=",
		"config.servidor=", "config.api_key=", "config.chaves.",
	} {
		if !strings.Contains(registro, esperado) {
			t.Errorf("o registro de arranque não trouxe %q:\n%s", esperado, registro)
		}
	}
	// A configuração sai REDIGIDA: nem a URL do banco nem a chave da API podem
	// aparecer.
	if strings.Contains(registro, "chave-de-teste") {
		t.Error("a API_KEY apareceu no registro")
	}
}

// TestPortaOcupadaFalhaNoArranque: o erro precisa dizer o endereço.
func TestPortaOcupadaFalhaNoArranque(t *testing.T) {
	ouvinte, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("abrindo porta: %v", err)
	}
	defer func() { _ = ouvinte.Close() }()

	cfg := configDeTeste(t)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	a, err := app.Montar(app.Dependencias{
		Config:   cfg,
		Logger:   logger,
		Servidor: httpapi.NovoServidor(http.NotFoundHandler(), httpapi.PadroesDoServidor(ouvinte.Addr().String())),
		Executor: worker.NovoPool(logger, 0),
	})
	if err != nil {
		t.Fatalf("Montar: %v", err)
	}

	err = a.Executar(context.Background(), nil)
	if err == nil {
		t.Fatal("esperava falha de escuta")
	}
	if !strings.Contains(err.Error(), ouvinte.Addr().String()) {
		t.Errorf("o erro deveria dizer o endereço: %v", err)
	}
}

// TestPortaEfemeraEResolvida: com SERVIDOR_PORTA=0 o sistema escolhe a porta, e
// o endereço registrado precisa ser o EFETIVO — senão o registro de arranque
// mente. A especificação registra o caso em §1.1.
func TestPortaEfemeraEResolvida(t *testing.T) {
	s := montar(t, nil, nil)

	ctx, sinal := context.WithCancel(context.Background())
	resultado := s.executarEmSegundoPlano(ctx, nil)
	endereco := s.esperarEscutando(t)

	if strings.HasSuffix(endereco, ":0") {
		t.Errorf("endereço = %q; deveria trazer a porta efetiva", endereco)
	}
	if !strings.Contains(s.conteudo(), "endereco="+endereco) {
		t.Errorf("o registro não trouxe o endereço efetivo %q", endereco)
	}

	sinal()
	if err := colher(t, resultado, 30*time.Second); err != nil {
		t.Fatalf("Executar: %v", err)
	}
}

// TestEncerrarSemExecutar: chamar Encerrar direto não pode travar nem entrar em
// pânico — é o caminho de uma falha de arranque tardia.
func TestEncerrarSemExecutar(t *testing.T) {
	s := montar(t, nil, nil)
	if err := s.app.Encerrar(context.Background(), "sem execução", nil); err != nil {
		t.Errorf("Encerrar: %v", err)
	}
}

// TestNovoFalhaSemBanco confere que a montagem completa recusa o arranque
// quando o banco está inalcançável — como o legado, que entra em pânico
// (main.rs:38).
func TestNovoFalhaSemBanco(t *testing.T) {
	cfg := configDeTeste(t)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	ctx, cancelar := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelar()

	_, err := app.Novo(ctx, cfg, logger, nil, nil, "teste", "abc", nil)
	if err == nil {
		t.Fatal("app.Novo aceitou um banco inalcançável")
	}
	if !strings.Contains(err.Error(), "pool de conexões") {
		t.Errorf("erro = %v; deveria apontar a etapa", err)
	}
	// A URL do banco não pode vazar na mensagem: ela carrega a senha.
	if strings.Contains(err.Error(), "postgres://") {
		t.Errorf("a URL do banco apareceu no erro: %v", err)
	}
}

// TestServidorQueEncerraSozinhoAindaDrena: se o servidor cair por conta
// própria, as importações em andamento ainda precisam terminar.
func TestServidorQueEncerraSozinhoAindaDrena(t *testing.T) {
	s := montar(t, nil, nil)

	concluiu := make(chan struct{})
	if err := s.executor.Submeter(context.Background(), context.Background(),
		func(context.Context) {
			time.Sleep(300 * time.Millisecond)
			close(concluiu)
		}); err != nil {
		t.Fatalf("Submeter: %v", err)
	}

	resultado := s.executarEmSegundoPlano(context.Background(), nil)
	s.esperarEscutando(t)

	// Encerra o servidor por fora, como se ele tivesse parado sozinho.
	go func() {
		ctx, cancelar := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancelar()
		_ = s.app.Encerrar(ctx, "encerramento externo", nil)
	}()

	if err := colher(t, resultado, 30*time.Second); err != nil {
		t.Logf("Executar devolveu %v", err)
	}
	select {
	case <-concluiu:
	case <-time.After(5 * time.Second):
		t.Error("a importação não terminou")
	}
}

// TestExecutarNaoDeixaGoroutine é explícito, além do goleak do TestMain: depois
// de Executar retornar, o número de goroutines volta ao patamar anterior.
func TestExecutarNaoDeixaGoroutine(t *testing.T) {
	s := montar(t, nil, nil)

	ctx, sinal := context.WithCancel(context.Background())
	resultado := s.executarEmSegundoPlano(ctx, nil)
	s.esperarEscutando(t)
	sinal()
	if err := colher(t, resultado, 30*time.Second); err != nil {
		t.Fatalf("Executar: %v", err)
	}

	if err := goleak.Find(
		goleak.IgnoreAnyFunction("net/http.(*persistConn).readLoop"),
		goleak.IgnoreAnyFunction("net/http.(*persistConn).writeLoop"),
	); err != nil {
		t.Errorf("goroutine remanescente após Executar: %v", err)
	}
}

// TestCodigosDeSaidaSaoDistintos documenta o contrato com o supervisor.
//
// O legado não tem códigos próprios; estes três existem para que o operador
// distinga "encerrou como pedido" de "desistiu de esperar".
func TestCodigosDeSaidaSaoDistintos(t *testing.T) {
	if errors.Is(app.ErrTetoDeEncerramento, app.ErrEncerramentoForcado) ||
		errors.Is(app.ErrEncerramentoForcado, app.ErrTetoDeEncerramento) {
		t.Error("os dois erros de encerramento precisam ser distinguíveis por errors.Is")
	}
	envolvido := fmt.Errorf("contexto: %w", app.ErrTetoDeEncerramento)
	if !errors.Is(envolvido, app.ErrTetoDeEncerramento) {
		t.Error("o erro precisa sobreviver ao embrulho")
	}
}

// TestMotivoDoSinalVaiParaORegistro: quem sabe qual sinal chegou é o ouvinte, e
// o registro precisa dizer qual foi — o legado distingue os dois com mensagens
// diferentes (ESPECIFICACAO §6.2).
func TestMotivoDoSinalVaiParaORegistro(t *testing.T) {
	cfg := configDeTeste(t)
	logger, conteudo := registroEmBuffer()

	a, err := app.Montar(app.Dependencias{
		Config:   cfg,
		Logger:   logger,
		Servidor: httpapi.NovoServidor(http.NotFoundHandler(), httpapi.PadroesDoServidor("127.0.0.1:0")),
		Executor: worker.NovoPool(logger, 0),
		Motivo:   func() string { return "sinal SIGTERM" },
	})
	if err != nil {
		t.Fatalf("Montar: %v", err)
	}

	ctx, sinal := context.WithCancel(context.Background())
	resultado := make(chan error, 1)
	go func() { resultado <- a.Executar(ctx, nil) }()

	limite := time.After(5 * time.Second)
	for a.Endereco() == "" || strings.HasSuffix(a.Endereco(), ":0") {
		select {
		case <-limite:
			t.Fatal("o servidor não subiu")
		default:
			time.Sleep(5 * time.Millisecond)
		}
	}
	sinal()
	if err := colher(t, resultado, 30*time.Second); err != nil {
		t.Fatalf("Executar: %v", err)
	}

	if registro := conteudo(); !strings.Contains(registro, `motivo="sinal SIGTERM"`) {
		t.Errorf("o registro não trouxe o motivo específico:\n%s", registro)
	}
}

// TestSemMotivoCaiNoGenerico: o encerramento pode vir de fora, sem sinal.
func TestSemMotivoCaiNoGenerico(t *testing.T) {
	s := montar(t, nil, nil)

	ctx, sinal := context.WithCancel(context.Background())
	resultado := s.executarEmSegundoPlano(ctx, nil)
	s.esperarEscutando(t)
	sinal()
	if err := colher(t, resultado, 30*time.Second); err != nil {
		t.Fatalf("Executar: %v", err)
	}

	if registro := s.conteudo(); !strings.Contains(registro, `motivo="sinal recebido"`) {
		t.Errorf("o registro não trouxe o motivo genérico:\n%s", registro)
	}
}

// TestServidorParaANTESDaDrenagem é o teste que detecta a inversão das duas
// primeiras etapas.
//
// A ordem de ESPECIFICACAO §6.3 não é arbitrária: se a drenagem viesse antes do
// `Shutdown`, o servidor continuaria aceitando requisições enquanto espera — e
// cada requisição nova submete uma importação nova, de modo que a drenagem pode
// não terminar nunca sob carga.
//
// O teste monta exatamente esse cenário: um manipulador que submete trabalho ao
// executor, e uma requisição disparada DURANTE a janela de drenagem. Com a
// ordem certa, a conexão é recusada; com a ordem invertida, ela é atendida.
func TestServidorParaANTESDaDrenagem(t *testing.T) {
	s := montar(t, nil, nil)

	var submetidasPelaRota atomic.Int64
	manipulador := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		// Cada requisição gera uma importação, como o /pdf real.
		_ = s.executor.Submeter(context.Background(), context.Background(),
			func(context.Context) { submetidasPelaRota.Add(1) })
		w.WriteHeader(http.StatusOK)
	})

	a, err := app.Montar(app.Dependencias{
		Config:   s.cfg,
		Logger:   s.logger,
		Servidor: httpapi.NovoServidor(manipulador, httpapi.PadroesDoServidor("127.0.0.1:0")),
		Executor: s.executor,
	})
	if err != nil {
		t.Fatalf("Montar: %v", err)
	}

	// Uma importação longa mantém a drenagem ocupada por tempo suficiente para
	// a requisição de teste chegar.
	liberar := make(chan struct{})
	if err := s.executor.Submeter(context.Background(), context.Background(),
		func(context.Context) { <-liberar }); err != nil {
		t.Fatalf("Submeter: %v", err)
	}

	ctx, sinal := context.WithCancel(context.Background())
	resultado := make(chan error, 1)
	go func() { resultado <- a.Executar(ctx, nil) }()

	limite := time.After(5 * time.Second)
	for a.Endereco() == "" || strings.HasSuffix(a.Endereco(), ":0") {
		select {
		case <-limite:
			t.Fatal("o servidor não subiu")
		default:
			time.Sleep(5 * time.Millisecond)
		}
	}
	endereco := a.Endereco()

	// Confirma que a rota funciona ANTES do sinal — senão o teste passaria por
	// estar batendo num endereço errado.
	cliente := &http.Client{Timeout: 2 * time.Second}
	resp, err := cliente.Get("http://" + endereco + "/importar")
	if err != nil {
		t.Fatalf("a rota não respondeu antes do sinal: %v", err)
	}
	_ = resp.Body.Close()

	sinal()

	// Janela de drenagem: o servidor JÁ deve ter parado de aceitar.
	time.Sleep(200 * time.Millisecond)
	if resp, err := cliente.Get("http://" + endereco + "/importar"); err == nil {
		_ = resp.Body.Close()
		t.Error("o servidor atendeu durante a drenagem: a ordem das etapas está invertida")
	}

	close(liberar)
	if err := colher(t, resultado, 30*time.Second); err != nil {
		t.Fatalf("Executar: %v", err)
	}

	// Só a requisição de antes do sinal pode ter gerado importação.
	if n := submetidasPelaRota.Load(); n != 1 {
		t.Errorf("a rota submeteu %d importação(ões); esperava 1", n)
	}
}
