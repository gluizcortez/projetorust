package observability

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
)

// Prefixo de todas as métricas do serviço.
const prefixo = "recorte_"

// Rótulos de estado da métrica de importações. Correspondem aos desfechos
// observáveis da máquina de estados — ver docs/ESPECIFICACAO.md §3.
const (
	EstadoFinalizado = "finalizado"
	EstadoErro       = "erro"
)

// Nomes de estágio do pipeline, usados como rótulo de duração.
const (
	EstagioExtracao   = "extracao"
	EstagioIndexacao  = "indexacao"
	EstagioRecorte    = "recorte"
	EstagioPersistviz = "persistencia"
)

// Metricas reúne os instrumentos do serviço e o registro que os contém.
//
// Não usa o registro global do Prometheus: a instância é criada em main e
// injetada, como toda dependência (invariante 4 do preâmbulo).
type Metricas struct {
	registro *prometheus.Registry

	// ImportacoesTotal conta importações concluídas, por desfecho.
	ImportacoesTotal *prometheus.CounterVec
	// EstagioDuracao mede a duração de cada estágio do pipeline.
	EstagioDuracao *prometheus.HistogramVec
	// PaginasPorDocumento distribui o número de páginas extraídas.
	PaginasPorDocumento prometheus.Histogram
	// RecortesPorImportacao distribui o número de recortes gravados.
	RecortesPorImportacao prometheus.Histogram
	// ImportacoesEmAndamento acompanha as tarefas de fundo ativas. Substitui o
	// Arc<AtomicUsize> do legado como fonte de observação.
	ImportacoesEmAndamento prometheus.Gauge
	// FilaProfundidade acompanha as importações aguardando vaga quando
	// MAX_IMPORTACOES_CONCORRENTES está ligado (fase F11).
	FilaProfundidade prometheus.Gauge
	// DocumentosSemPaginas conta documentos que produziram zero páginas.
	//
	// Existe por causa de INV-P20: um PDF truncado é aceito pelo MuPDF, produz
	// zero páginas e a importação termina em status 5 — indistinguível, no
	// banco, de um diário sem ocorrências. Esta métrica é a única forma de
	// enxergar o caso sem alterar comportamento. Ver DECISOES-ABERTAS.md, D-18.
	DocumentosSemPaginas prometheus.Counter
}

// NovasMetricas cria os instrumentos e os registra.
func NovasMetricas() *Metricas {
	m := &Metricas{
		registro: prometheus.NewRegistry(),

		ImportacoesTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: prefixo + "importacoes_total",
			Help: "Total de importações concluídas, por desfecho.",
		}, []string{"estado"}),

		EstagioDuracao: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name: prefixo + "estagio_duracao_segundos",
			Help: "Duração de cada estágio do processamento de uma importação.",
			// Documentos de diário vão de segundos a minutos; a escala cobre
			// de 100 ms a ~27 min.
			Buckets: prometheus.ExponentialBuckets(0.1, 2, 14),
		}, []string{"estagio"}),

		PaginasPorDocumento: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:    prefixo + "paginas_por_documento",
			Help:    "Número de páginas extraídas por documento.",
			Buckets: []float64{1, 5, 10, 25, 50, 100, 250, 500, 1000, 2500},
		}),

		RecortesPorImportacao: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:    prefixo + "recortes_por_importacao",
			Help:    "Número de recortes gravados por importação.",
			Buckets: []float64{0, 1, 5, 10, 50, 100, 500, 1000, 5000, 10000},
		}),

		ImportacoesEmAndamento: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: prefixo + "importacoes_em_andamento",
			Help: "Importações sendo processadas neste instante.",
		}),

		FilaProfundidade: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: prefixo + "fila_profundidade",
			Help: "Importações aguardando vaga para processamento.",
		}),

		DocumentosSemPaginas: prometheus.NewCounter(prometheus.CounterOpts{
			Name: prefixo + "documentos_sem_paginas_total",
			Help: "Documentos aceitos que produziram zero páginas (ver INV-P20).",
		}),
	}

	m.registro.MustRegister(
		m.ImportacoesTotal,
		m.EstagioDuracao,
		m.PaginasPorDocumento,
		m.RecortesPorImportacao,
		m.ImportacoesEmAndamento,
		m.FilaProfundidade,
		m.DocumentosSemPaginas,
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
	)

	return m
}

// Registro devolve o registro para exposição por HTTP.
//
// A rota /metrics é da fase F11, atrás da chave HEALTH_ENDPOINTS; aqui só
// entregamos o coletor.
func (m *Metricas) Registro() *prometheus.Registry { return m.registro }
