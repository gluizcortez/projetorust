// Registro estruturado, rastros e a propagação de identificadores por
// contexto — as três faces da observabilidade, que se usam sempre juntas.
//
// As métricas ficam em metricas.go, que é maior e tem assunto próprio.
package observability

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"time"

	"github.com/gluizcortez/projetorust/src/config"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
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

// NomeDoServico identifica o serviço na telemetria.
const NomeDoServico = "recorte-api"

// Encerrar libera os recursos de telemetria, esvaziando o que estiver pendente.
//
// É chamada na última etapa do encerramento gracioso — ver
// docs/ESPECIFICACAO.md §6.3 e a fase F10.
type Encerrar func(context.Context) error

// IniciarTracing configura o rastreamento distribuído via OTLP.
//
// Endpoint vazio devolve uma implementação nula: nenhum exportador é criado,
// nenhuma conexão é aberta, e a função de encerramento não faz nada. É o padrão
// e reproduz o legado, que não tem rastreamento.
func IniciarTracing(ctx context.Context, cfg *config.Config) (Encerrar, error) {
	if cfg.OTLPEndpoint == "" {
		return func(context.Context) error { return nil }, nil
	}

	exportador, err := otlptracegrpc.New(ctx,
		otlptracegrpc.WithEndpoint(cfg.OTLPEndpoint),
		otlptracegrpc.WithInsecure(),
	)
	if err != nil {
		return nil, fmt.Errorf("criando exportador OTLP para %q: %w", cfg.OTLPEndpoint, err)
	}

	// NewSchemaless, e não NewWithAttributes: fixar uma versão de semconv aqui
	// conflita com o esquema de resource.Default() assim que o SDK avança, e a
	// falha só apareceria quando alguém ligasse o OTLP em produção. Sem esquema
	// próprio, a mesclagem herda o do SDK e o acoplamento de versão some.
	recursos, err := resource.Merge(
		resource.Default(),
		resource.NewSchemaless(
			attribute.String("service.name", NomeDoServico),
			attribute.String("servidor.endereco", cfg.Endereco()),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("montando recursos de telemetria: %w", err)
	}

	provedor := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exportador),
		sdktrace.WithResource(recursos),
	)

	otel.SetTracerProvider(provedor)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	return func(ctx context.Context) error {
		// O encerramento tem teto próprio: telemetria pendente não pode
		// segurar o processo indefinidamente.
		ctx, cancelar := context.WithTimeout(ctx, 5*time.Second)
		defer cancelar()
		if err := provedor.Shutdown(ctx); err != nil {
			return fmt.Errorf("encerrando provedor de rastreamento: %w", err)
		}
		return nil
	}, nil
}

// chave é o tipo das chaves de contexto deste pacote.
//
// Não é exportado de propósito: nenhum outro pacote consegue colidir com ele,
// nem ler os valores sem passar pelos acessadores daqui.
type chave int

const (
	chaveIDRequisicao chave = iota
	chaveIDImportacao
	chaveIDPerfil
)

// ComIDRequisicao anexa o identificador da requisição ao contexto.
//
// A partir daí, todo registro emitido com esse contexto carrega o atributo
// id_requisicao — sem que quem registra precise passá-lo.
func ComIDRequisicao(ctx context.Context, id string) context.Context {
	if id == "" {
		return ctx
	}
	return context.WithValue(ctx, chaveIDRequisicao, id)
}

// IDRequisicao lê o identificador da requisição do contexto.
func IDRequisicao(ctx context.Context) (string, bool) {
	v, ok := ctx.Value(chaveIDRequisicao).(string)
	return v, ok
}

// IDImportacao lê o identificador da importação do contexto.
func IDImportacao(ctx context.Context) (int64, bool) {
	v, ok := ctx.Value(chaveIDImportacao).(int64)
	return v, ok
}

// IDPerfil lê o identificador do perfil do contexto.
func IDPerfil(ctx context.Context) (int64, bool) {
	v, ok := ctx.Value(chaveIDPerfil).(int64)
	return v, ok
}
