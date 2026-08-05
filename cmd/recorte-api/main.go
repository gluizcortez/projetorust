// Comando recorte-api é o ponto de entrada do serviço de recorte de diários
// oficiais.
//
// Nesta fase (F2) ele carrega a configuração e monta a observabilidade, mas
// ainda não registra rotas: responde 404 em qualquer caminho, inclusive em
// /ping. Esse é o estado esperado, documentado nos critérios de aceite da F1.
//
// As fases seguintes o preenchem:
//   - F9  registra as rotas de internal/adapter/httpapi
//   - F10 move a montagem e o ciclo de vida para internal/app
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gluizcortez/projetorust/internal/config"
	"github.com/gluizcortez/projetorust/internal/platform/observability"
)

// Injetados na compilação com -ldflags "-X main.versao=... -X main.revisao=...".
var (
	versao  = "dev"
	revisao = "desconhecida"
)

// Códigos de saída. Ampliados na fase F10, que acrescenta o estouro do teto de
// encerramento e o segundo sinal.
const (
	saidaLimpa    = 0
	saidaFalha    = 1
	saidaDoenteHC = 1
)

func main() {
	os.Exit(executar())
}

func executar() int {
	var (
		mostrarVersao = flag.Bool("versao", false, "imprime a versão e encerra")
		sondar        = flag.Bool("healthcheck", false, "consulta /ping no próprio processo e encerra")
	)
	flag.Parse()

	if *mostrarVersao {
		fmt.Printf("%s (%s)\n", versao, revisao) //nolint:forbidigo // saída do comando
		return saidaLimpa
	}

	ctx := context.Background()

	cfg, err := config.Carregar(ctx)
	if err != nil {
		// Antes de haver registrador não há como registrar: a falha de
		// configuração é a única que sai por stderr cru.
		fmt.Fprintf(os.Stderr, "%v\n", err) //nolint:forbidigo // anterior ao registrador
		return saidaFalha
	}

	if *sondar {
		return sondarSaude(cfg.Endereco())
	}

	log := observability.NovoLogger(cfg, os.Stderr)

	encerrarTracing, err := observability.IniciarTracing(ctx, cfg)
	if err != nil {
		log.ErrorContext(ctx, "falha ao iniciar o rastreamento", "erro", err)
		return saidaFalha
	}

	metricas := observability.NovasMetricas()

	if err := servir(ctx, cfg, log, metricas, encerrarTracing); err != nil {
		log.ErrorContext(ctx, "encerramento com erro", "erro", err)
		return saidaFalha
	}
	return saidaLimpa
}

// servir sobe o servidor e bloqueia até receber SIGINT ou SIGTERM.
//
// O encerramento aqui é o mínimo viável. A ordem completa — parar de aceitar,
// drenar HTTP, drenar importações, fechar o pool, esvaziar telemetria — é da
// fase F10.
func servir(
	ctx context.Context,
	cfg *config.Config,
	log *slog.Logger,
	metricas *observability.Metricas,
	encerrarTracing observability.Encerrar,
) error {
	ctx, pararSinais := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer pararSinais()

	// As métricas ainda não têm quem as alimente; o pipeline entra na F8.
	// Referenciá-las aqui mantém a montagem explícita e o compilador honesto
	// sobre a dependência.
	_ = metricas

	// Roteador vazio: 404 em qualquer caminho. As rotas entram na fase F9.
	srv := &http.Server{
		Addr:              cfg.Endereco(),
		Handler:           http.NewServeMux(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       5 * time.Minute,
		WriteTimeout:      5 * time.Minute,
		IdleTimeout:       2 * time.Minute,
		MaxHeaderBytes:    1 << 20,
	}

	log.InfoContext(ctx, "servidor iniciando",
		"endereco", cfg.Endereco(),
		"versao", versao,
		"revisao", revisao,
		"config", cfg,
	)

	erros := make(chan error, 1)
	go func() {
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			erros <- err
		}
		close(erros)
	}()

	var motivo string
	select {
	case err := <-erros:
		if err != nil {
			return fmt.Errorf("escutando em %s: %w", cfg.Endereco(), err)
		}
		motivo = "servidor encerrou sozinho"
	case <-ctx.Done():
		motivo = "sinal recebido"
	}

	log.InfoContext(ctx, "encerrando", "motivo", motivo)

	// O contexto de encerramento não pode ser o que acabou de ser cancelado.
	desligar, cancelar := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancelar()

	var problemas []error
	if err := srv.Shutdown(desligar); err != nil {
		problemas = append(problemas, fmt.Errorf("encerrando servidor: %w", err))
	}
	if err := encerrarTracing(desligar); err != nil {
		problemas = append(problemas, err)
	}

	log.InfoContext(ctx, "encerrado")
	return errors.Join(problemas...)
}

// sondarSaude consulta /ping no endereço de escuta. É o comando invocado pelo
// HEALTHCHECK da imagem, que assim dispensa shell e utilitários de rede.
//
// ATENÇÃO: nesta fase o serviço responde 404 em /ping, porque ainda não há
// manipuladores. A sonda portanto reporta o contêiner como não saudável, e
// isso é o estado esperado. A fase F9 registra a rota e a sonda passa a obter
// 200.
func sondarSaude(endereco string) int {
	cliente := &http.Client{Timeout: 3 * time.Second}
	resp, err := cliente.Get("http://" + endereco + "/ping")
	if err != nil {
		fmt.Fprintf(os.Stderr, "sonda: %v\n", err) //nolint:forbidigo // sonda sem registrador
		return saidaDoenteHC
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		fmt.Fprintf(os.Stderr, "sonda: /ping devolveu %d (esperado 200)\n", resp.StatusCode) //nolint:forbidigo // sonda sem registrador
		return saidaDoenteHC
	}
	return saidaLimpa
}
