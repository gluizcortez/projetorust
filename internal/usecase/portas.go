package usecase

import (
	"context"
	"time"
)

// Este arquivo declara as portas que o caso de uso precisa e que NÃO são de
// domínio: instrumentação e execução em segundo plano. São definidas pelo
// consumidor, como todas as outras, e implementadas em internal/platform.
//
// Elas existem aqui, e não em domain, porque não descrevem regra de negócio:
// descrevem como o serviço observa e agenda o próprio trabalho.

// Estágios do pipeline, como aparecem no rótulo da métrica de duração.
//
// São constantes porque o mesmo texto precisa sair igual em toda série
// temporal: um rótulo digitado errado cria uma série nova em silêncio.
const (
	EstagioExtracao   = "extracao"
	EstagioIndexacao  = "indexacao"
	EstagioChaves     = "chaves"
	EstagioRecorte    = "recorte"
	EstagioImportacao = "importacao"
)

// Desfechos de uma importação, como aparecem no rótulo da métrica de contagem.
const (
	DesfechoFinalizado = "finalizado"
	DesfechoErro       = "erro"
	// DesfechoPreso é a importação que o legado deixaria travada no último
	// status gravado, sem ir a -1 nem a 5. Ver docs/ESPECIFICACAO.md §3.5.
	DesfechoPreso = "preso"
)

// Metricas é a porta de instrumentação do pipeline.
//
// Toda implementação precisa tolerar chamada concorrente. O caso de uso NUNCA
// verifica se a porta é nula: quem constrói o pipeline sem métricas recebe
// MetricasNulas.
type Metricas interface {
	// ObservarEstagio registra a duração de um estágio.
	ObservarEstagio(estagio string, duracao time.Duration)
	// ObservarPaginas registra quantas páginas o documento produziu.
	ObservarPaginas(paginas int)
	// ObservarRecortes registra quantos recortes a importação gravou.
	ObservarRecortes(recortes int)
	// ContarImportacao registra o desfecho de uma importação encerrada.
	ContarImportacao(desfecho string)
}

// MetricasNulas descarta tudo. É o padrão quando nenhuma é injetada.
type MetricasNulas struct{}

var _ Metricas = MetricasNulas{}

// ObservarEstagio não faz nada.
func (MetricasNulas) ObservarEstagio(string, time.Duration) {}

// ObservarPaginas não faz nada.
func (MetricasNulas) ObservarPaginas(int) {}

// ObservarRecortes não faz nada.
func (MetricasNulas) ObservarRecortes(int) {}

// ContarImportacao não faz nada.
func (MetricasNulas) ContarImportacao(string) {}

// Executor agenda o processamento de segundo plano.
//
// Substitui `tokio::task::spawn` (reference/main.rs:256). São dois contextos
// porque eles têm prazos diferentes: `ctx` governa a submissão em si — e
// portanto a espera por vaga, quando há teto —, enquanto `ctxDaTarefa` governa
// a execução, que sobrevive à resposta HTTP.
type Executor interface {
	Submeter(ctx context.Context, ctxDaTarefa context.Context, tarefa func(context.Context)) error
}
