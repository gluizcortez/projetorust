package worker

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"runtime/debug"
	"sync"
	"sync/atomic"
	"time"
)

// IntervaloDeAvisoDeDrenagem é de quanto em quanto tempo a drenagem registra
// quantas tarefas ainda faltam.
//
// 100 ms é o mesmo período do laço de sondagem do legado
// (reference/main.rs:73), preservado para que o volume de registro durante o
// encerramento seja o mesmo. A diferença é que aqui o valor NÃO governa a
// espera — a drenagem acorda por notificação, não por sondagem —, então
// alterá-lo muda apenas a frequência do aviso.
const IntervaloDeAvisoDeDrenagem = 100 * time.Millisecond

// ErrPoolEncerrado indica submissão após o início da drenagem.
//
// Não tem equivalente no legado, onde `tokio::task::spawn` aceita tarefas até o
// processo morrer. Só é observável durante o encerramento, quando o servidor
// HTTP já parou de aceitar conexões e portanto nenhuma submissão nova deveria
// chegar. Ver docs/ESPECIFICACAO.md §6.3.
var ErrPoolEncerrado = errors.New("executor encerrado")

// Tarefa é uma unidade de trabalho de segundo plano.
//
// Recebe o contexto do executor, NÃO o da requisição que a originou: no legado
// a tarefa sobrevive à resposta HTTP (reference/main.rs:256), e cancelar o
// processamento porque o cliente desligou seria comportamento novo.
//
// É um APELIDO, não um tipo definido. A porta `usecase.Executor` declara
// `func(context.Context)`, e um tipo definido não satisfaria a interface — o Go
// exige assinatura idêntica, e `worker.Tarefa` seria um tipo diferente. O
// apelido mantém o nome para quem lê e a compatibilidade para o compilador.
type Tarefa = func(ctx context.Context)

// Pool executa tarefas de segundo plano com teto de concorrência opcional e
// drenagem por notificação.
//
// Substitui o par `Arc<AtomicUsize>` mais espera por sondagem do legado
// (reference/main.rs:42, 252, 265, 71-74). O contador continua existindo e
// continua observável — via EmAndamento —, mas quem espera por ele usa
// sync.WaitGroup e acorda quando a última tarefa termina, em vez de perguntar
// a cada 100 ms.
//
// Seguro para uso concorrente.
type Pool struct {
	logger *slog.Logger

	// vagas é o semáforo do teto de concorrência. NIL quando não há teto, que
	// é o padrão e o comportamento do legado: `tokio::task::spawn` nunca
	// recusa nem enfileira. Ver a chave MAX_IMPORTACOES_CONCORRENTES.
	vagas chan struct{}

	grupo       sync.WaitGroup
	emAndamento atomic.Int64
	aguardando  atomic.Int64

	mu         sync.Mutex
	encerrando bool
}

// NovoPool cria o executor.
//
// `maxConcorrentes` menor ou igual a zero desliga o teto, reproduzindo o
// legado. Qualquer valor positivo é EVOLUÇÃO: limita o paralelismo e faz
// Submeter bloquear enquanto não houver vaga.
func NovoPool(logger *slog.Logger, maxConcorrentes int) *Pool {
	p := &Pool{logger: logger}
	if maxConcorrentes > 0 {
		p.vagas = make(chan struct{}, maxConcorrentes)
	}
	return p
}

// Submeter entrega uma tarefa ao executor.
//
// A tarefa é registrada no grupo de espera ANTES de a goroutine ser disparada:
// entre o registro e a partida não existe janela em que a drenagem possa
// concluir achando que não há trabalho.
//
// `ctx` governa apenas a ESPERA POR VAGA, quando há teto. Ele não é repassado à
// tarefa — quem chama informa o contexto de execução em `ctxDaTarefa`, que tem
// prazo próprio, independente da requisição HTTP.
func (p *Pool) Submeter(ctx context.Context, ctxDaTarefa context.Context, tarefa Tarefa) error { //nolint:revive // dois contextos com papéis distintos, documentados acima
	if tarefa == nil {
		return errors.New("executor: tarefa nula")
	}

	p.mu.Lock()
	if p.encerrando {
		p.mu.Unlock()
		return ErrPoolEncerrado
	}
	// O registro acontece com a trava tomada, para não correr com Drenar.
	p.grupo.Add(1)
	p.mu.Unlock()

	if err := p.tomarVaga(ctx); err != nil {
		p.grupo.Done()
		return err
	}

	p.emAndamento.Add(1)

	go func() {
		defer p.grupo.Done()
		defer p.emAndamento.Add(-1)
		defer p.devolverVaga()
		defer p.recuperar(ctxDaTarefa)

		tarefa(ctxDaTarefa)
	}()

	return nil
}

// tomarVaga bloqueia até haver vaga, ou até o contexto expirar.
func (p *Pool) tomarVaga(ctx context.Context) error {
	if p.vagas == nil {
		return nil // sem teto: o caminho do legado
	}

	// Tentativa não bloqueante primeiro, para que o caso comum não pague o
	// custo do select com contexto nem apareça na métrica de fila.
	select {
	case p.vagas <- struct{}{}:
		return nil
	default:
	}

	p.aguardando.Add(1)
	defer p.aguardando.Add(-1)

	select {
	case p.vagas <- struct{}{}:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("executor: aguardando vaga: %w", ctx.Err())
	}
}

func (p *Pool) devolverVaga() {
	if p.vagas != nil {
		<-p.vagas
	}
}

// recuperar é a rede de último recurso: um pânico em uma tarefa NUNCA derruba o
// processo.
//
// Reproduz o efeito do `tokio`, que captura o pânico na `JoinHandle` e a
// descarta (docs/ESPECIFICACAO.md §6.4). O contador é liberado de qualquer
// forma, porque o `defer p.grupo.Done()` foi registrado antes deste.
//
// Não toca no estado da importação: o executor não conhece importações. Quem
// tem essa responsabilidade — e a decisão de qual status gravar — é o caso de
// uso, que instala a própria recuperação mais perto do trabalho.
func (p *Pool) recuperar(ctx context.Context) {
	r := recover()
	if r == nil {
		return
	}
	p.logger.ErrorContext(ctx, "tarefa de segundo plano entrou em pânico",
		slog.Any("panico", r),
		slog.String("pilha", string(debug.Stack())),
	)
}

// EmAndamento devolve quantas tarefas estão executando neste instante.
//
// É o `tarefas.load(Ordering::SeqCst)` do legado (reference/main.rs:71).
func (p *Pool) EmAndamento() int64 { return p.emAndamento.Load() }

// Aguardando devolve quantas submissões estão bloqueadas esperando vaga.
// Vale sempre zero enquanto não houver teto de concorrência.
func (p *Pool) Aguardando() int64 { return p.aguardando.Load() }

// Drenar recusa novas submissões e aguarda as tarefas em andamento.
//
// Reproduz o laço de espera de reference/main.rs:71-74, com duas diferenças
// deliberadas:
//
//   - acorda por NOTIFICAÇÃO, não por sondagem a cada 100 ms. O aviso periódico
//     continua saindo na mesma cadência, para não mudar o volume de registro;
//   - respeita o PRAZO do contexto. O legado espera para sempre — uma
//     importação travada segura o processo indefinidamente
//     (docs/ESPECIFICACAO.md §6.3, DEFEITO PRESERVADO). Passar um contexto sem
//     prazo reproduz exatamente esse comportamento, e é o que a chave
//     SHUTDOWN_TIMEOUT com valor zero faz.
//
// Chamar duas vezes é seguro: a segunda chamada apenas aguarda de novo.
func (p *Pool) Drenar(ctx context.Context) error {
	p.mu.Lock()
	p.encerrando = true
	p.mu.Unlock()

	pronto := make(chan struct{})
	go func() {
		p.grupo.Wait()
		close(pronto)
	}()

	// Caminho rápido: nada em andamento, nenhum aviso a emitir.
	select {
	case <-pronto:
		return nil
	default:
	}

	aviso := time.NewTicker(IntervaloDeAvisoDeDrenagem)
	defer aviso.Stop()

	for {
		select {
		case <-pronto:
			return nil
		case <-aviso.C:
			p.logger.InfoContext(ctx, "drenando importações",
				slog.Int64("em_andamento", p.EmAndamento()))
		case <-ctx.Done():
			return fmt.Errorf("executor: drenagem interrompida com %d tarefa(s) em andamento: %w",
				p.EmAndamento(), ctx.Err())
		}
	}
}
