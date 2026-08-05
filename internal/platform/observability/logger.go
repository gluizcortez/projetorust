package observability

import (
	"context"
	"io"
	"log/slog"

	"go.opentelemetry.io/otel/trace"

	"github.com/gluizcortez/projetorust/internal/config"
)

// Nomes dos atributos propagados por contexto. São o vocabulário de correlação
// do serviço: qualquer consulta em ferramenta de observabilidade usa estes.
const (
	AtributoIDRequisicao = "id_requisicao"
	AtributoIDImportacao = "id_importacao"
	AtributoIDPerfil     = "id_perfil"
	AtributoTraceID      = "trace_id"
	AtributoSpanID       = "span_id"
)

// NovoLogger monta o registrador estruturado conforme a configuração.
//
// A saída vai para os.Stderr por padrão; use NovoLoggerEm para direcioná-la,
// o que os testes fazem.
func NovoLogger(cfg *config.Config, saida io.Writer) *slog.Logger {
	opcoes := &slog.HandlerOptions{Level: cfg.LogNivel}

	var base slog.Handler
	if cfg.LogFormato == "text" {
		base = slog.NewTextHandler(saida, opcoes)
	} else {
		base = slog.NewJSONHandler(saida, opcoes)
	}

	return slog.New(&manipuladorDeContexto{base: base})
}

// manipuladorDeContexto copia para atributos os identificadores presentes no
// contexto, a cada registro.
//
// É o que substitui a interpolação do legado: em vez de
// "[ID Importação: 42] -> Processo de recorte INICIADO", o evento sai como
// mensagem estável mais o campo id_importacao=42 — correlacionável.
type manipuladorDeContexto struct {
	base slog.Handler
}

func (m *manipuladorDeContexto) Enabled(ctx context.Context, nivel slog.Level) bool {
	return m.base.Enabled(ctx, nivel)
}

func (m *manipuladorDeContexto) Handle(ctx context.Context, r slog.Record) error {
	if id, ok := IDRequisicao(ctx); ok {
		r.AddAttrs(slog.String(AtributoIDRequisicao, id))
	}
	if id, ok := IDImportacao(ctx); ok {
		r.AddAttrs(slog.Int64(AtributoIDImportacao, id))
	}
	if id, ok := IDPerfil(ctx); ok {
		r.AddAttrs(slog.Int64(AtributoIDPerfil, id))
	}
	if sc := trace.SpanContextFromContext(ctx); sc.IsValid() {
		r.AddAttrs(
			slog.String(AtributoTraceID, sc.TraceID().String()),
			slog.String(AtributoSpanID, sc.SpanID().String()),
		)
	}

	//nolint:wrapcheck // repasse direto ao manipulador embrulhado
	return m.base.Handle(ctx, r)
}

func (m *manipuladorDeContexto) WithAttrs(atributos []slog.Attr) slog.Handler {
	return &manipuladorDeContexto{base: m.base.WithAttrs(atributos)}
}

func (m *manipuladorDeContexto) WithGroup(nome string) slog.Handler {
	return &manipuladorDeContexto{base: m.base.WithGroup(nome)}
}
