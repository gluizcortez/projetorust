package app

import (
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/gluizcortez/projetorust/internal/platform/observability"
	"github.com/gluizcortez/projetorust/internal/usecase"
)

// Os adaptadores deste arquivo são a cola entre as portas do caso de uso e a
// infraestrutura. São curtos, e é justamente por isso que precisam de teste: um
// rótulo de métrica trocado cria uma série nova em silêncio, e um relógio que
// devolve o instante errado só aparece como duração absurda num painel.

// TestRelogioDoSistemaAvanca.
func TestRelogioDoSistemaAvanca(t *testing.T) {
	r := relogioDoSistema{}

	antes := r.Agora()
	if antes.IsZero() {
		t.Fatal("Agora devolveu o instante zero")
	}
	time.Sleep(2 * time.Millisecond)
	if depois := r.Agora(); !depois.After(antes) {
		t.Errorf("o relógio não avançou: %v não é depois de %v", depois, antes)
	}
}

// TestMetricasDoPipelineAlimentamOsInstrumentos confere que cada método da
// porta chega ao instrumento certo, com o rótulo certo.
func TestMetricasDoPipelineAlimentamOsInstrumentos(t *testing.T) {
	m := observability.NovasMetricas()
	a := metricasDoPipeline{m: m}

	a.ObservarEstagio(usecase.EstagioExtracao, 250*time.Millisecond)
	a.ObservarEstagio(usecase.EstagioRecorte, time.Second)
	a.ObservarPaginas(120)
	a.ObservarRecortes(7)
	a.ContarImportacao(usecase.DesfechoFinalizado)
	a.ContarImportacao(usecase.DesfechoErro)
	a.ContarImportacao(usecase.DesfechoErro)

	if n := testutil.CollectAndCount(m.EstagioDuracao); n != 2 {
		t.Errorf("séries de duração de estágio = %d; esperava 2", n)
	}
	if v := testutil.ToFloat64(m.ImportacoesTotal.WithLabelValues(usecase.DesfechoErro)); v != 2 {
		t.Errorf("importações com desfecho erro = %v; esperava 2", v)
	}
	if v := testutil.ToFloat64(m.ImportacoesTotal.WithLabelValues(usecase.DesfechoFinalizado)); v != 1 {
		t.Errorf("importações finalizadas = %v; esperava 1", v)
	}

	// Os nomes das séries são contrato com quem monta painel.
	esperados := []string{
		"recorte_estagio_duracao_segundos",
		"recorte_paginas_por_documento",
		"recorte_recortes_por_importacao",
		"recorte_importacoes_total",
	}
	texto := coletar(t, m)
	for _, nome := range esperados {
		if !strings.Contains(texto, nome) {
			t.Errorf("a exposição não trouxe %q", nome)
		}
	}
}

// TestDocumentoSemPaginasEContado é INV-P20 visto pela instrumentação.
//
// Um PDF truncado é aceito, produz zero páginas e termina em status 5 —
// indistinguível, no banco, de um diário sem ocorrências. Este contador é a
// única forma de enxergar o caso sem alterar comportamento. Ver
// docs/DECISOES-ABERTAS.md, D-18.
func TestDocumentoSemPaginasEContado(t *testing.T) {
	m := observability.NovasMetricas()
	a := metricasDoPipeline{m: m}

	a.ObservarPaginas(10)
	if v := testutil.ToFloat64(m.DocumentosSemPaginas); v != 0 {
		t.Fatalf("documentos sem páginas = %v antes de haver algum", v)
	}

	a.ObservarPaginas(0)
	a.ObservarPaginas(0)
	if v := testutil.ToFloat64(m.DocumentosSemPaginas); v != 2 {
		t.Errorf("documentos sem páginas = %v; esperava 2", v)
	}
}

// TestMetricasNulasNaoEntramEmPanico: a raiz de composição pode montar o
// pipeline sem instrumentação, e nenhum método pode falhar por isso.
func TestMetricasNulasNaoEntramEmPanico(t *testing.T) {
	a := metricasDoPipeline{m: nil}

	a.ObservarEstagio(usecase.EstagioExtracao, time.Second)
	a.ObservarPaginas(0)
	a.ObservarRecortes(3)
	a.ContarImportacao(usecase.DesfechoPreso)
}

func coletar(t *testing.T, m *observability.Metricas) string {
	t.Helper()
	famílias, err := m.Registro().Gather()
	if err != nil {
		t.Fatalf("coletando: %v", err)
	}
	var b strings.Builder
	for _, f := range famílias {
		b.WriteString(f.GetName())
		b.WriteByte('\n')
	}
	return b.String()
}
