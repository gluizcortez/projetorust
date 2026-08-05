package observability

import (
	"context"
	"fmt"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"

	"github.com/gluizcortez/projetorust/internal/config"
)

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
