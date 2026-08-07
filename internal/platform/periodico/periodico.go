// Package periodico executa uma tarefa em intervalos regulares até ser parado.
//
// Existe para a varredura de órfãs da fase F11 (VARREDURA_ORFAS) e nada mais. É
// pequeno de propósito: um agendador com expressões de calendário, atrasos
// exponenciais e histórico de execuções seria infraestrutura nova para resolver
// um problema que ainda não temos.
//
// O legado não tem nada equivalente — nenhuma tarefa periódica, nenhum
// temporizador. Com VARREDURA_ORFAS no padrão, nada aqui é sequer construído.
package periodico

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// Tarefa é o trabalho a repetir. O erro é registrado e a repetição continua:
// uma passagem que falha não pode encerrar o laço, senão uma indisponibilidade
// momentânea do banco desliga a varredura até o próximo reinício do processo.
type Tarefa func(ctx context.Context) error

// Laco executa uma tarefa a cada intervalo.
//
// Seguro para uso concorrente: Parar pode ser chamado de qualquer goroutine,
// quantas vezes for.
type Laco struct {
	nome      string
	intervalo time.Duration
	tarefa    Tarefa
	logger    *slog.Logger

	iniciar sync.Once
	parar   sync.Once
	parado  chan struct{}
	fim     chan struct{}
}

// NovoLaco monta o laço. Não o inicia — quem faz isso é Iniciar.
func NovoLaco(nome string, intervalo time.Duration, tarefa Tarefa, logger *slog.Logger) *Laco {
	return &Laco{
		nome:      nome,
		intervalo: intervalo,
		tarefa:    tarefa,
		logger:    logger,
		parado:    make(chan struct{}),
		fim:       make(chan struct{}),
	}
}

// Iniciar dispara o laço em segundo plano.
//
// A PRIMEIRA execução acontece depois de um intervalo, não imediatamente. É
// deliberado: durante o arranque o serviço ainda está subindo, e uma varredura
// que rode no primeiro segundo compete por conexões com as importações que
// estão sendo aceitas — além de poder ver como "presas" importações que a
// instância anterior deixou em curso e que ninguém mais vai tocar de qualquer
// forma. Esperar um intervalo não custa nada e evita as duas coisas.
//
// Chamar duas vezes não dispara dois laços.
func (l *Laco) Iniciar(ctx context.Context) {
	l.iniciar.Do(func() {
		go l.executar(ctx)
	})
}

func (l *Laco) executar(ctx context.Context) {
	defer close(l.fim)

	tique := time.NewTicker(l.intervalo)
	defer tique.Stop()

	l.logger.InfoContext(ctx, "tarefa periódica iniciada",
		slog.String("tarefa", l.nome), slog.Duration("intervalo", l.intervalo))

	for {
		select {
		case <-l.parado:
			return
		case <-ctx.Done():
			return
		case <-tique.C:
		}

		// O contexto da tarefa é o do laço: parar o laço interrompe a passagem
		// em curso, o que importa no encerramento — uma varredura que segure a
		// transação atrasaria o desligamento inteiro.
		if err := l.tarefa(ctx); err != nil {
			l.logger.ErrorContext(ctx, "tarefa periódica falhou; seguirá tentando",
				slog.String("tarefa", l.nome), slog.Any("erro", err))
		}
	}
}

// Parar encerra o laço e AGUARDA a passagem em curso terminar.
//
// Aguardar é o ponto: sem isso, o encerramento fecharia o pool de conexões
// debaixo de uma transação aberta. Devolve quando o laço terminou ou quando o
// contexto expirar, o que vier primeiro.
//
// Chamar antes de Iniciar, ou duas vezes, é seguro.
func (l *Laco) Parar(ctx context.Context) error {
	l.parar.Do(func() { close(l.parado) })

	// Nunca iniciado: não há o que esperar. `iniciar.Do` com uma função vazia
	// marca o Once como consumido, o que também impede um Iniciar posterior de
	// disparar um laço que ninguém pararia.
	iniciou := true
	l.iniciar.Do(func() { iniciou = false })
	if !iniciou {
		return nil
	}

	select {
	case <-l.fim:
		return nil
	case <-ctx.Done():
		return ctx.Err() //nolint:wrapcheck // o chamador nomeia a etapa de encerramento
	}
}
