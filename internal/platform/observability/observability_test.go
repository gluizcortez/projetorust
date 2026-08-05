package observability_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"

	"github.com/gluizcortez/projetorust/internal/config"
	"github.com/gluizcortez/projetorust/internal/platform/observability"
)

func configDeTeste(nivel slog.Level, formato string) *config.Config {
	return &config.Config{
		ServidorIP:    "127.0.0.1",
		ServidorPorta: 6001,
		LogNivel:      nivel,
		LogFormato:    formato,
	}
}

// registrosDe decodifica cada linha JSON emitida.
func registrosDe(t *testing.T, buf *bytes.Buffer) []map[string]any {
	t.Helper()
	var saida []map[string]any
	for _, linha := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		if linha == "" {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(linha), &m); err != nil {
			t.Fatalf("linha de registro não é JSON válido: %q: %v", linha, err)
		}
		saida = append(saida, m)
	}
	return saida
}

// -------------------------------------------------------------------------
// Propagação por contexto — critério de aceite da fase
// -------------------------------------------------------------------------

func TestIDImportacaoApareceEmTodoRegistroDoContexto(t *testing.T) {
	var buf bytes.Buffer
	log := observability.NovoLogger(configDeTeste(slog.LevelDebug, "json"), &buf)

	ctx := observability.ComIDImportacao(context.Background(), 4242)

	// Vários registros, em níveis diferentes, sem passar o identificador.
	log.DebugContext(ctx, "paginando documento")
	log.InfoContext(ctx, "documento indexado")
	log.WarnContext(ctx, "expressão sem termos")
	log.ErrorContext(ctx, "falha ao gravar recorte")

	registros := registrosDe(t, &buf)
	if len(registros) != 4 {
		t.Fatalf("esperava 4 registros, obtive %d", len(registros))
	}
	for i, r := range registros {
		v, ok := r[observability.AtributoIDImportacao]
		if !ok {
			t.Errorf("registro %d não carrega %s: %v", i, observability.AtributoIDImportacao, r)
			continue
		}
		if v.(float64) != 4242 {
			t.Errorf("registro %d: %s = %v, esperado 4242", i, observability.AtributoIDImportacao, v)
		}
	}
}

func TestTodosOsIdentificadoresDeContextoSaoPropagados(t *testing.T) {
	var buf bytes.Buffer
	log := observability.NovoLogger(configDeTeste(slog.LevelInfo, "json"), &buf)

	ctx := context.Background()
	ctx = observability.ComIDRequisicao(ctx, "req-abc-123")
	ctx = observability.ComIDImportacao(ctx, 77)
	ctx = observability.ComIDPerfil(ctx, 9)

	log.InfoContext(ctx, "processo de recorte iniciado")

	r := registrosDe(t, &buf)[0]
	esperado := map[string]any{
		observability.AtributoIDRequisicao: "req-abc-123",
		observability.AtributoIDImportacao: float64(77),
		observability.AtributoIDPerfil:     float64(9),
	}
	for chave, valor := range esperado {
		if r[chave] != valor {
			t.Errorf("%s = %v (%T), esperado %v", chave, r[chave], r[chave], valor)
		}
	}
}

func TestContextoSemIdentificadoresNaoAcrescentaAtributos(t *testing.T) {
	var buf bytes.Buffer
	log := observability.NovoLogger(configDeTeste(slog.LevelInfo, "json"), &buf)

	log.InfoContext(context.Background(), "servidor iniciando")

	r := registrosDe(t, &buf)[0]
	for _, chave := range []string{
		observability.AtributoIDRequisicao,
		observability.AtributoIDImportacao,
		observability.AtributoIDPerfil,
		observability.AtributoTraceID,
	} {
		if _, presente := r[chave]; presente {
			t.Errorf("registro sem contexto não deveria carregar %s", chave)
		}
	}
}

func TestPropagacaoSobreviveAWithAttrsEWithGroup(t *testing.T) {
	var buf bytes.Buffer
	log := observability.NovoLogger(configDeTeste(slog.LevelInfo, "json"), &buf)

	ctx := observability.ComIDImportacao(context.Background(), 5)

	// O decorador precisa sobreviver às operações que criam novos manipuladores.
	log.With("componente", "pipeline").InfoContext(ctx, "estágio concluído")

	r := registrosDe(t, &buf)[0]
	if r[observability.AtributoIDImportacao] != float64(5) {
		t.Errorf("With() perdeu a propagação de contexto: %v", r)
	}
	if r["componente"] != "pipeline" {
		t.Errorf("With() perdeu o atributo estático: %v", r)
	}
}

func TestIDRequisicaoVazioNaoEntraNoContexto(t *testing.T) {
	ctx := observability.ComIDRequisicao(context.Background(), "")
	if _, ok := observability.IDRequisicao(ctx); ok {
		t.Error("identificador vazio não deveria ser anexado")
	}
}

func TestAcessadoresDevolvemOQueFoiGuardado(t *testing.T) {
	ctx := context.Background()
	ctx = observability.ComIDRequisicao(ctx, "r1")
	ctx = observability.ComIDImportacao(ctx, 10)
	ctx = observability.ComIDPerfil(ctx, 20)

	if v, ok := observability.IDRequisicao(ctx); !ok || v != "r1" {
		t.Errorf("IDRequisicao = %q, %v", v, ok)
	}
	if v, ok := observability.IDImportacao(ctx); !ok || v != 10 {
		t.Errorf("IDImportacao = %d, %v", v, ok)
	}
	if v, ok := observability.IDPerfil(ctx); !ok || v != 20 {
		t.Errorf("IDPerfil = %d, %v", v, ok)
	}
}

// -------------------------------------------------------------------------
// Formato e nível
// -------------------------------------------------------------------------

func TestFormatoTexto(t *testing.T) {
	var buf bytes.Buffer
	log := observability.NovoLogger(configDeTeste(slog.LevelInfo, "text"), &buf)
	log.InfoContext(observability.ComIDImportacao(context.Background(), 1), "evento")

	saida := buf.String()
	if strings.HasPrefix(strings.TrimSpace(saida), "{") {
		t.Errorf("LOG_FORMATO=text deveria produzir texto, não JSON: %s", saida)
	}
	if !strings.Contains(saida, observability.AtributoIDImportacao+"=1") {
		t.Errorf("o formato texto deveria carregar o identificador: %s", saida)
	}
}

func TestNivelFiltra(t *testing.T) {
	var buf bytes.Buffer
	log := observability.NovoLogger(configDeTeste(slog.LevelWarn, "json"), &buf)

	ctx := context.Background()
	log.DebugContext(ctx, "não deve sair")
	log.InfoContext(ctx, "não deve sair")
	log.WarnContext(ctx, "deve sair")
	log.ErrorContext(ctx, "deve sair")

	registros := registrosDe(t, &buf)
	if len(registros) != 2 {
		t.Fatalf("LOG_NIVEL=warn deveria deixar passar 2 registros, passaram %d", len(registros))
	}
}

// -------------------------------------------------------------------------
// Métricas
// -------------------------------------------------------------------------

func TestMetricasTemOsInstrumentosExigidos(t *testing.T) {
	m := observability.NovasMetricas()

	// Move cada instrumento para que apareça na coleta.
	m.ImportacoesTotal.WithLabelValues(observability.EstadoFinalizado).Inc()
	m.ImportacoesTotal.WithLabelValues(observability.EstadoErro).Inc()
	m.EstagioDuracao.WithLabelValues(observability.EstagioIndexacao).Observe(1.5)
	m.PaginasPorDocumento.Observe(120)
	m.RecortesPorImportacao.Observe(7)
	m.ImportacoesEmAndamento.Set(3)
	m.FilaProfundidade.Set(0)
	m.DocumentosSemPaginas.Inc()

	coletadas, err := m.Registro().Gather()
	if err != nil {
		t.Fatalf("Gather: %v", err)
	}

	presentes := map[string]*dto.MetricFamily{}
	for _, mf := range coletadas {
		presentes[mf.GetName()] = mf
	}

	exigidas := []struct {
		nome string
		tipo dto.MetricType
	}{
		{"recorte_importacoes_total", dto.MetricType_COUNTER},
		{"recorte_estagio_duracao_segundos", dto.MetricType_HISTOGRAM},
		{"recorte_paginas_por_documento", dto.MetricType_HISTOGRAM},
		{"recorte_recortes_por_importacao", dto.MetricType_HISTOGRAM},
		{"recorte_importacoes_em_andamento", dto.MetricType_GAUGE},
		{"recorte_fila_profundidade", dto.MetricType_GAUGE},
		{"recorte_documentos_sem_paginas_total", dto.MetricType_COUNTER},
	}

	for _, e := range exigidas {
		mf, ok := presentes[e.nome]
		if !ok {
			t.Errorf("métrica ausente: %s", e.nome)
			continue
		}
		if mf.GetType() != e.tipo {
			t.Errorf("%s: tipo %v, esperado %v", e.nome, mf.GetType(), e.tipo)
		}
		if mf.GetHelp() == "" {
			t.Errorf("%s: sem texto de ajuda", e.nome)
		}
	}
}

func TestMetricasUsamPrefixoERotuloEmSnakeCase(t *testing.T) {
	m := observability.NovasMetricas()
	m.ImportacoesTotal.WithLabelValues(observability.EstadoFinalizado).Inc()
	m.EstagioDuracao.WithLabelValues(observability.EstagioRecorte).Observe(0.2)

	coletadas, err := m.Registro().Gather()
	if err != nil {
		t.Fatalf("Gather: %v", err)
	}

	for _, mf := range coletadas {
		nome := mf.GetName()
		// Os coletores de runtime do Go e do processo não seguem nosso prefixo.
		if !strings.HasPrefix(nome, "recorte_") {
			continue
		}
		if nome != strings.ToLower(nome) || strings.Contains(nome, "-") {
			t.Errorf("nome fora de snake_case: %s", nome)
		}
		for _, met := range mf.GetMetric() {
			for _, rotulo := range met.GetLabel() {
				n := rotulo.GetName()
				if n != strings.ToLower(n) || strings.Contains(n, "-") {
					t.Errorf("rótulo fora de snake_case em %s: %s", nome, n)
				}
			}
		}
	}
}

func TestMetricasNaoUsamORegistroGlobal(t *testing.T) {
	// Criar duas instâncias não pode entrar em conflito: se usassem o registro
	// padrão do Prometheus, a segunda entraria em pânico por duplicidade.
	a := observability.NovasMetricas()
	b := observability.NovasMetricas()

	if a.Registro() == b.Registro() {
		t.Error("as instâncias deveriam ter registros distintos")
	}
	if a.Registro() == prometheus.DefaultRegisterer {
		t.Error("o registro não pode ser o global")
	}
}

// -------------------------------------------------------------------------
// Rastreamento
// -------------------------------------------------------------------------

func TestTracingComEndpointVazioEhNulo(t *testing.T) {
	cfg := configDeTeste(slog.LevelInfo, "json")
	cfg.OTLPEndpoint = ""

	encerrar, err := observability.IniciarTracing(context.Background(), cfg)
	if err != nil {
		t.Fatalf("endpoint vazio não deveria falhar: %v", err)
	}
	if encerrar == nil {
		t.Fatal("a função de encerramento não pode ser nula")
	}
	if err := encerrar(context.Background()); err != nil {
		t.Errorf("encerrar a implementação nula não deveria falhar: %v", err)
	}
}

func TestWithGroupPreservaAPropagacao(t *testing.T) {
	var buf bytes.Buffer
	log := observability.NovoLogger(configDeTeste(slog.LevelInfo, "json"), &buf)

	ctx := observability.ComIDImportacao(context.Background(), 8)
	log.WithGroup("estagio").InfoContext(ctx, "concluído", "nome", "indexacao")

	r := registrosDe(t, &buf)[0]
	// O identificador vem do contexto e é acrescentado ao registro depois do
	// agrupamento, então cai DENTRO do grupo — comportamento do slog.
	grupo, ok := r["estagio"].(map[string]any)
	if !ok {
		t.Fatalf("esperava o grupo 'estagio' no registro: %v", r)
	}
	if grupo["nome"] != "indexacao" {
		t.Errorf("atributo do grupo perdido: %v", grupo)
	}
	if grupo[observability.AtributoIDImportacao] != float64(8) {
		t.Errorf("WithGroup perdeu a propagação de contexto: %v", r)
	}
}

func TestTracingComEndpointConfiguradoDevolveEncerramento(t *testing.T) {
	cfg := configDeTeste(slog.LevelInfo, "json")
	// Endereço sem coletor: a criação do exportador gRPC é preguiçosa, então o
	// caminho de configuração é exercitado sem exigir infraestrutura.
	cfg.OTLPEndpoint = "127.0.0.1:4317"

	encerrar, err := observability.IniciarTracing(context.Background(), cfg)
	if err != nil {
		t.Fatalf("IniciarTracing: %v", err)
	}
	t.Cleanup(func() { _ = encerrar(context.Background()) })

	if encerrar == nil {
		t.Fatal("a função de encerramento não pode ser nula")
	}
}
