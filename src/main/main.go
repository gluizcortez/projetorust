// Comando recorte-api é o ponto de entrada do serviço de recorte de diários
// oficiais.
//
// Não tem lógica: carrega a configuração, monta o grafo em internal/app,
// executa e traduz o erro em código de saída. Toda a montagem está em
// app.Novo, e todo o encerramento em app.Encerrar.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/gluizcortez/projetorust/src/app"
	"github.com/gluizcortez/projetorust/src/config"
	"github.com/gluizcortez/projetorust/src/observability"
	"github.com/gluizcortez/projetorust/src/shutdown"
)

// Injetados na compilação com -ldflags "-X main.versao=... -X main.revisao=...".
var (
	versao  = "dev"
	revisao = "desconhecida"
)

// Códigos de saída.
//
// O legado tem apenas dois desfechos — sai limpo ou entra em pânico —, e nenhum
// código próprio. Os três abaixo existem porque o operador precisa distinguir
// "encerrou como pedido" de "desistiu de esperar", e um supervisor precisa
// decidir se reinicia.
const (
	// saidaLimpa é o encerramento normal.
	saidaLimpa = 0
	// saidaFalha é falha de arranque, ou erro durante a execução.
	saidaFalha = 1
	// saidaTetoDeEncerramento é SHUTDOWN_TIMEOUT estourado com trabalho
	// pendente. Só acontece com a chave definida: o padrão zero espera
	// indefinidamente, como o legado.
	saidaTetoDeEncerramento = 2
	// saidaForcada é o segundo sinal durante o encerramento. 130 é a convenção
	// para "encerrado por SIGINT" (128 + 2).
	saidaForcada = 130
	// saidaDoenteHC é o que a sonda de saúde da imagem devolve quando o serviço
	// não responde. É separada de saidaFalha por clareza, embora coincida.
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

	escuta := shutdown.Ouvir(ctx)
	defer escuta.Parar()

	servico, err := app.Novo(ctx, cfg, log, encerrarTracing, metricas, versao, revisao, escuta.Motivo)
	if err != nil {
		log.ErrorContext(ctx, "falha ao montar o serviço", "erro", err)
		// O rastreamento já subiu; esvaziá-lo é o único encerramento devido.
		_ = encerrarTracing(ctx)
		return saidaFalha
	}

	err = servico.Executar(escuta.Contexto, escuta.Forcado)

	switch {
	case err == nil:
		return saidaLimpa
	case errors.Is(err, app.ErrEncerramentoForcado):
		log.ErrorContext(ctx, "saída forçada", "erro", err)
		return saidaForcada
	case errors.Is(err, app.ErrTetoDeEncerramento):
		log.ErrorContext(ctx, "encerramento incompleto", "erro", err)
		return saidaTetoDeEncerramento
	default:
		log.ErrorContext(ctx, "encerramento com erro", "erro", err)
		return saidaFalha
	}
}

// sondarSaude consulta /ping no endereço de escuta. É o comando invocado pelo
// HEALTHCHECK da imagem, que assim dispensa shell e utilitários de rede.
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
