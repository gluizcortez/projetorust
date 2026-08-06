// Package app é a raiz de composição: monta o grafo de dependências inteiro,
// explicitamente e de cima para baixo, e conduz o ciclo de vida do processo.
//
// A montagem é manual e legível como uma lista. Este pacote NÃO usa framework
// de injeção de dependência, reflexão ou registro dinâmico de componentes.
package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/gluizcortez/projetorust/internal/adapter/httpapi"
	"github.com/gluizcortez/projetorust/internal/adapter/pdftext"
	"github.com/gluizcortez/projetorust/internal/adapter/postgres"
	"github.com/gluizcortez/projetorust/internal/adapter/searchidx"
	"github.com/gluizcortez/projetorust/internal/config"
	"github.com/gluizcortez/projetorust/internal/domain"
	"github.com/gluizcortez/projetorust/internal/platform/observability"
	"github.com/gluizcortez/projetorust/internal/platform/worker"
	"github.com/gluizcortez/projetorust/internal/usecase"
)

// Erros que o chamador traduz em código de saída.
var (
	// ErrTetoDeEncerramento indica que SHUTDOWN_TIMEOUT estourou com trabalho
	// pendente. NÃO acontece com o valor padrão zero, que espera
	// indefinidamente como o legado.
	ErrTetoDeEncerramento = errors.New("teto de encerramento estourado")

	// ErrEncerramentoForcado indica segundo sinal durante o encerramento.
	ErrEncerramentoForcado = errors.New("encerramento forçado por segundo sinal")
)

// App é o serviço montado, pronto para executar.
type App struct {
	cfg      *config.Config
	logger   *slog.Logger
	servidor *http.Server
	executor *worker.Pool
	pool     *pgxpool.Pool

	// encerrarTracing esvazia traços e métricas. Nunca é nula: quem monta
	// fornece uma função vazia quando não há telemetria.
	encerrarTracing observability.Encerrar

	// escutar é injetável para que os testes de ciclo de vida usem um ouvinte
	// próprio em vez de abrir uma porta de verdade.
	escutar func(rede, endereco string) (net.Listener, error)

	// motivo descreve por que o encerramento começou. Quem sabe disso é o
	// ouvinte de sinais, que nomeia SIGINT e SIGTERM separadamente — o legado
	// também os distingue, com mensagens diferentes (ESPECIFICACAO §6.2).
	motivo func() string

	// enderecoEfetivo é onde o servidor REALMENTE escuta, que difere do
	// configurado quando a porta é 0 e o sistema escolhe uma efêmera
	// (docs/ESPECIFICACAO.md §1.1).
	//
	// É atômico porque Executar o escreve enquanto quem observa o serviço pode
	// estar lendo — o detector de corrida encontrou exatamente isso na primeira
	// versão, que sobrescrevia `servidor.Addr`.
	enderecoEfetivo atomic.Pointer[string]

	versao, revisao string
}

// Dependencias são as peças já construídas.
//
// Existe para que os testes de ciclo de vida montem o App com dublês, sem banco
// e sem PDF — o que é o que permite exercitar sinais e drenagem de verdade.
type Dependencias struct {
	Config   *config.Config
	Logger   *slog.Logger
	Servidor *http.Server
	Executor *worker.Pool

	// Pool pode ser nulo quando não há banco (testes de ciclo de vida).
	Pool *pgxpool.Pool
	// EncerrarTracing pode ser nula.
	EncerrarTracing observability.Encerrar

	// Motivo descreve a causa do encerramento; tipicamente
	// shutdown.Escuta.Motivo. Nula, ou devolvendo vazio, cai em
	// "sinal recebido".
	Motivo func() string

	Versao, Revisao string
}

// Montar compõe o App a partir de peças prontas.
func Montar(d Dependencias) (*App, error) {
	faltando := []string{}
	if d.Config == nil {
		faltando = append(faltando, "Config")
	}
	if d.Logger == nil {
		faltando = append(faltando, "Logger")
	}
	if d.Servidor == nil {
		faltando = append(faltando, "Servidor")
	}
	if d.Executor == nil {
		faltando = append(faltando, "Executor")
	}
	if len(faltando) > 0 {
		return nil, fmt.Errorf("app: dependências ausentes: %v", faltando)
	}

	encerrar := d.EncerrarTracing
	if encerrar == nil {
		encerrar = func(context.Context) error { return nil }
	}

	return &App{
		cfg:             d.Config,
		logger:          d.Logger,
		servidor:        d.Servidor,
		executor:        d.Executor,
		pool:            d.Pool,
		encerrarTracing: encerrar,
		escutar:         net.Listen,
		motivo:          d.Motivo,
		versao:          d.Versao,
		revisao:         d.Revisao,
	}, nil
}

// Novo monta o grafo INTEIRO, de baixo para cima.
//
// A ordem abaixo é a de construção. A de encerramento, em Encerrar, NÃO é
// simplesmente a inversa: ela é declarada em docs/ESPECIFICACAO.md §6.3 e tem
// razão própria — o servidor HTTP para primeiro para que nenhuma importação
// nova entre enquanto as antigas drenam.
//
//  1. registrador                     observability.NovoLogger
//  2. rastreamento                    observability.IniciarTracing
//  3. métricas                        observability.NovasMetricas
//  4. pool de conexões                postgres.NovoPool
//  5. unidade de trabalho             postgres.NovaUnidadeDeTrabalho
//  6. repositório de importação       postgres.NovoRepositorioImportacao
//  7. repositório de perfil           postgres.NovoRepositorioPerfil
//  8. repositório de recorte          postgres.NovoRepositorioRecorte
//  9. extrator de texto               pdftext.NovoExtrator
//  10. indexador                       searchidx.NovoIndexador
//  11. pipeline                        usecase.NovoPipeline
//  12. executor de segundo plano       worker.NovoPool
//  13. ingestão                        usecase.NovaIngestao
//  14. roteador                        httpapi.NovoRouter
//  15. servidor                        httpapi.NovoServidor
func Novo(
	ctx context.Context,
	cfg *config.Config,
	logger *slog.Logger,
	encerrarTracing observability.Encerrar,
	metricas *observability.Metricas,
	versao, revisao string,
	motivo func() string,
) (*App, error) {
	// 4 — pool de conexões. Primeiro recurso externo; falha aqui impede o
	// arranque, como no legado (reference/main.rs:38, que entra em pânico).
	pool, err := postgres.NovoPool(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("montando o pool de conexões: %w", err)
	}

	// 5 a 8 — persistência.
	uow := postgres.NovaUnidadeDeTrabalho(pool)
	importacoes := postgres.NovoRepositorioImportacao(pool)
	perfis := postgres.NovoRepositorioPerfil(pool)
	recortes := postgres.NovoRepositorioRecorte(pool, uow)

	// 9 e 10 — extração e índice.
	extrator := pdftext.NovoExtrator()
	indexador := searchidx.NovoIndexador()

	// 11 — pipeline de segundo plano.
	pipeline, err := usecase.NovoPipeline(usecase.DependenciasDoPipeline{
		Importacoes: importacoes,
		Perfis:      perfis,
		Recortes:    recortes,
		Extrator:    extrator,
		Indexador:   indexador,
		Relogio:     relogioDoSistema{},
		Logger:      logger,
		Metricas:    metricasDoPipeline{m: metricas},
	})
	if err != nil {
		pool.Close()
		return nil, fmt.Errorf("montando o pipeline: %w", err)
	}

	// 12 — executor. Teto zero reproduz o legado, onde `tokio::task::spawn`
	// nunca recusa nem enfileira.
	executor := worker.NovoPool(logger, cfg.MaxImportacoesConcorrentes)

	// 13 — ingestão, o caminho síncrono.
	ingestao, err := usecase.NovaIngestao(usecase.DependenciasDaIngestao{
		Importacoes: importacoes,
		Executor:    executor,
		Logger:      logger,
		Processar:   pipeline.Processar,
	})
	if err != nil {
		pool.Close()
		return nil, fmt.Errorf("montando a ingestão: %w", err)
	}

	// 14 — roteador com a cadeia de middleware.
	roteador, err := httpapi.NovoRouter(httpapi.Dependencias{
		Ingestor:            ingestao,
		Logger:              logger,
		APIKey:              cfg.APIKey.Revelar(),
		MaxUploadBytes:      cfg.MaxUploadBytes,
		RespostaProblemJSON: cfg.RespostaProblemJSON,
	})
	if err != nil {
		pool.Close()
		return nil, fmt.Errorf("montando o roteador: %w", err)
	}

	// 15 — servidor.
	servidor := httpapi.NovoServidor(roteador, httpapi.OpcoesDoServidor{
		Endereco:               cfg.Endereco(),
		TempoLimiteDeCabecalho: cfg.HTTPTempoLimiteDeCabecalho,
		TempoLimiteDeLeitura:   cfg.HTTPTempoLimiteDeLeitura,
		TempoLimiteDeEscrita:   cfg.HTTPTempoLimiteDeEscrita,
		TempoLimiteOcioso:      cfg.HTTPTempoLimiteOcioso,
		MaxHeaderBytes:         cfg.HTTPMaxHeaderBytes,
	})

	return Montar(Dependencias{
		Config:          cfg,
		Logger:          logger,
		Servidor:        servidor,
		Executor:        executor,
		Pool:            pool,
		EncerrarTracing: encerrarTracing,
		Motivo:          motivo,
		Versao:          versao,
		Revisao:         revisao,
	})
}

// Endereco devolve onde o servidor REALMENTE escuta.
//
// Antes de Executar, é o endereço configurado. Depois, é o efetivo — que difere
// quando a porta configurada é 0 e o sistema escolhe uma efêmera.
//
// Seguro para chamar de outra goroutine enquanto o serviço executa.
func (a *App) Endereco() string {
	if efetivo := a.enderecoEfetivo.Load(); efetivo != nil {
		return *efetivo
	}
	return a.servidor.Addr
}

// Executar sobe o servidor e bloqueia até o encerramento.
//
// `ctx` é cancelado no primeiro sinal. `forcar`, quando não nulo, é fechado no
// segundo — e aí a função devolve ErrEncerramentoForcado sem esperar mais nada.
//
// Nenhuma goroutine sobrevive ao retorno: a do servidor termina em Shutdown, e
// as do executor são aguardadas em Drenar.
func (a *App) Executar(ctx context.Context, forcar <-chan struct{}) error {
	ouvinte, err := a.escutar("tcp", a.servidor.Addr)
	if err != nil {
		return fmt.Errorf("escutando em %s: %w", a.servidor.Addr, err)
	}
	// A porta efetiva pode diferir da configurada quando ela é 0. O campo
	// `servidor.Addr` NÃO é sobrescrito: o http.Server é lido por suas próprias
	// goroutines, e mutá-lo aqui é corrida.
	endereco := ouvinte.Addr().String()
	a.enderecoEfetivo.Store(&endereco)

	a.logger.InfoContext(ctx, "servidor iniciando",
		slog.String("endereco", endereco),
		slog.String("versao", a.versao),
		slog.String("revisao", a.revisao),
		slog.Any("config", a.cfg),
	)

	erros := make(chan error, 1)
	go func() {
		defer close(erros)
		if err := a.servidor.Serve(ouvinte); err != nil && !errors.Is(err, http.ErrServerClosed) {
			erros <- err
		}
	}()

	var motivo string
	select {
	case err := <-erros:
		if err != nil {
			return fmt.Errorf("servindo em %s: %w", endereco, err)
		}
		// O servidor encerrou sozinho, sem ninguém ter pedido. Continua para o
		// encerramento ordenado assim mesmo: pode haver importação em curso.
		motivo = "servidor encerrou sozinho"
	case <-forcarOuNunca(forcar):
		// O segundo sinal NÃO dispensa o encerramento: `Encerrar` detecta a
		// força, corta o prazo de cada etapa e devolve ErrEncerramentoForcado.
		// Retornar direto daqui deixaria o ouvinte aberto — foi o que a
		// primeira versão fazia, e o teste do canal já fechado pegou.
		motivo = "segundo sinal"
	case <-ctx.Done():
		motivo = a.motivoDoSinal()
	}

	err = a.Encerrar(context.WithoutCancel(ctx), motivo, forcar)

	// Espera a goroutine do servidor terminar. `Serve` só retorna depois de
	// `Shutdown` fechar o ouvinte, e sem esta espera `Executar` retornaria com
	// ela ainda viva — que é o que o goleak acusa.
	<-erros

	return err
}

// motivoDoSinal devolve a descrição do sinal, ou um texto genérico quando o
// encerramento não veio de sinal — cancelamento do contexto por outro caminho.
func (a *App) motivoDoSinal() string {
	if a.motivo != nil {
		if m := a.motivo(); m != "" {
			return m
		}
	}
	return "sinal recebido"
}

// forcarOuNunca devolve um canal que nunca entrega quando `forcar` é nulo,
// para que o `select` não precise de dois ramos.
func forcarOuNunca(forcar <-chan struct{}) <-chan struct{} {
	if forcar != nil {
		return forcar
	}
	return nil // um canal nulo bloqueia para sempre no select
}

// Encerrar executa a ordem de encerramento declarada.
//
// A ordem é normativa (docs/ESPECIFICACAO.md §6.3) e cada etapa é registrada com
// a própria duração:
//
//  1. para de aceitar conexões e deixa terminar o que está em curso
//  2. drena as importações em andamento
//  3. fecha o pool do banco
//  4. esvazia a telemetria
//
// O servidor para ANTES da drenagem de propósito: enquanto ele aceitar
// requisições, importações novas continuam entrando e a drenagem nunca acaba.
//
// Todas as etapas dividem o mesmo prazo, vindo de SHUTDOWN_TIMEOUT. Valor ZERO
// significa espera indefinida, que é o comportamento do legado e o padrão —
// `DEFEITO PRESERVADO` de §6.3.
func (a *App) Encerrar(ctx context.Context, motivo string, forcar <-chan struct{}) error {
	inicio := time.Now()
	a.logger.InfoContext(ctx, "encerrando", slog.String("motivo", motivo))

	prazo := ctx
	if a.cfg.ShutdownTimeout > 0 {
		var cancelar context.CancelFunc
		prazo, cancelar = context.WithTimeout(ctx, a.cfg.ShutdownTimeout)
		defer cancelar()
	}

	// O segundo sinal interrompe a espera de qualquer etapa.
	if forcar != nil {
		var cancelar context.CancelFunc
		prazo, cancelar = context.WithCancel(prazo)
		defer cancelar()
		go func() {
			select {
			case <-forcar:
				cancelar()
			case <-prazo.Done():
			}
		}()
	}

	var problemas []error

	// 1 — parar de aceitar; as requisições em curso terminam.
	problemas = a.etapa(ctx, prazo, "servidor http", problemas, a.servidor.Shutdown)

	// 2 — esperar as importações em andamento.
	problemas = a.etapa(ctx, prazo, "importações", problemas, a.executor.Drenar)

	// 3 — fechar o pool. Não recebe prazo: `Close` aguarda as conexões voltarem
	// e, chegando aqui, ninguém mais as toma.
	if a.pool != nil {
		marco := time.Now()
		a.pool.Close()
		a.logger.InfoContext(ctx, "etapa de encerramento concluída",
			slog.String("etapa", "pool de conexões"),
			slog.Duration("duracao", time.Since(marco)))
	}

	// 4 — esvaziar a telemetria. É a última porque as etapas anteriores ainda
	// emitem traços e métricas.
	problemas = a.etapa(ctx, prazo, "telemetria", problemas, a.encerrarTracing)

	if forcado(forcar) {
		a.logger.WarnContext(ctx, "encerramento forçado por segundo sinal",
			slog.Int64("importacoes_pendentes", a.executor.EmAndamento()),
			slog.Duration("duracao", time.Since(inicio)))
		return ErrEncerramentoForcado
	}

	if err := prazo.Err(); errors.Is(err, context.DeadlineExceeded) {
		a.logger.ErrorContext(ctx, "teto de encerramento estourado",
			slog.Duration("teto", a.cfg.ShutdownTimeout),
			slog.Int64("importacoes_pendentes", a.executor.EmAndamento()),
			slog.Duration("duracao", time.Since(inicio)))
		return fmt.Errorf("%w após %s com %d importação(ões) pendente(s)",
			ErrTetoDeEncerramento, a.cfg.ShutdownTimeout, a.executor.EmAndamento())
	}

	a.logger.InfoContext(ctx, "encerrado",
		slog.Duration("duracao", time.Since(inicio)))
	return errors.Join(problemas...)
}

// etapa roda uma fase do encerramento medindo a duração e acumulando a falha.
//
// Uma etapa que falha NÃO interrompe as seguintes: fechar o pool importa mesmo
// que a drenagem tenha estourado, e esvaziar a telemetria importa mesmo que
// tudo o mais tenha dado errado.
func (a *App) etapa(
	ctx, prazo context.Context,
	nome string,
	problemas []error,
	fn func(context.Context) error,
) []error {
	marco := time.Now()
	err := fn(prazo)
	duracao := time.Since(marco)

	if err != nil {
		a.logger.ErrorContext(ctx, "etapa de encerramento falhou",
			slog.String("etapa", nome),
			slog.Duration("duracao", duracao),
			slog.Any("erro", err))
		return append(problemas, fmt.Errorf("encerrando %s: %w", nome, err))
	}

	a.logger.InfoContext(ctx, "etapa de encerramento concluída",
		slog.String("etapa", nome),
		slog.Duration("duracao", duracao))
	return problemas
}

func forcado(forcar <-chan struct{}) bool {
	if forcar == nil {
		return false
	}
	select {
	case <-forcar:
		return true
	default:
		return false
	}
}

// -------------------------------------------------------------------------
// Adaptadores que só a raiz de composição precisa
// -------------------------------------------------------------------------

// relogioDoSistema é o domain.Relogio de produção.
//
// Existe aqui, e não em internal/platform, porque é o único lugar que precisa
// dele: os testes injetam relógio fixo pela porta.
type relogioDoSistema struct{}

var _ domain.Relogio = relogioDoSistema{}

func (relogioDoSistema) Agora() time.Time { return time.Now() }

// metricasDoPipeline liga a porta usecase.Metricas aos instrumentos do
// Prometheus.
//
// A ligação está AQUI porque a regra de dependência proíbe internal/usecase de
// importar internal/platform. O caso de uso declara a porta; a raiz de
// composição a preenche.
type metricasDoPipeline struct{ m *observability.Metricas }

var _ usecase.Metricas = metricasDoPipeline{}

func (a metricasDoPipeline) ObservarEstagio(estagio string, d time.Duration) {
	if a.m == nil {
		return
	}
	a.m.EstagioDuracao.WithLabelValues(estagio).Observe(d.Seconds())
}

func (a metricasDoPipeline) ObservarPaginas(paginas int) {
	if a.m == nil {
		return
	}
	a.m.PaginasPorDocumento.Observe(float64(paginas))
	// INV-P20: documento aceito que produz zero páginas termina em status 5,
	// indistinguível no banco de um diário sem ocorrências. Este contador é a
	// única forma de enxergá-lo sem alterar comportamento (D-18).
	if paginas == 0 {
		a.m.DocumentosSemPaginas.Inc()
	}
}

func (a metricasDoPipeline) ObservarRecortes(recortes int) {
	if a.m == nil {
		return
	}
	a.m.RecortesPorImportacao.Observe(float64(recortes))
}

func (a metricasDoPipeline) ContarImportacao(desfecho string) {
	if a.m == nil {
		return
	}
	a.m.ImportacoesTotal.WithLabelValues(desfecho).Inc()
}
