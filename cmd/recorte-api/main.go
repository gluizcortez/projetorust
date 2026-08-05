// Comando recorte-api é o ponto de entrada do serviço de recorte de diários
// oficiais.
//
// Nesta fase (F1) ele é deliberadamente mínimo: sobe um servidor HTTP sem
// nenhuma rota registrada, portanto responde 404 em qualquer caminho —
// inclusive em /ping. Esse é o estado esperado da fase, documentado nos
// critérios de aceite.
//
// As fases seguintes o preenchem:
//   - F2  carrega a configuração de internal/config em vez de ler o ambiente aqui
//   - F9  registra as rotas de internal/adapter/httpapi
//   - F10 move a montagem e o ciclo de vida para internal/app
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

// versao é injetada na compilação com -ldflags "-X main.versao=...".
var versao = "dev"

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
		fmt.Println(versao) //nolint:forbidigo // saída do próprio comando, não registro
		return saidaLimpa
	}

	endereco := enderecoDeEscuta()

	if *sondar {
		return sondarSaude(endereco)
	}

	if err := servir(endereco); err != nil {
		fmt.Fprintf(os.Stderr, "erro: %v\n", err)
		return saidaFalha
	}
	return saidaLimpa
}

// enderecoDeEscuta reproduz os padrões do legado (main.rs:24-25, 44-48).
//
// A leitura definitiva, com validação e as chaves de recurso, é da fase F2.
// Aqui ela existe apenas para que o binário suba e seja verificável.
func enderecoDeEscuta() string {
	ip := os.Getenv("SERVIDOR_IP")
	if ip == "" {
		ip = "192.168.42.1"
	}
	porta := os.Getenv("SERVIDOR_PORTA")
	if porta == "" {
		porta = "6001"
	}
	return net.JoinHostPort(ip, porta)
}

// servir sobe o servidor e bloqueia até receber SIGINT ou SIGTERM.
//
// O encerramento aqui é o mínimo viável. A ordem completa — parar de aceitar,
// drenar HTTP, drenar importações, fechar o pool, esvaziar telemetria — é da
// fase F10.
func servir(endereco string) error {
	ctx, parar := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer parar()

	// Roteador vazio: 404 em qualquer caminho. As rotas entram na fase F9.
	srv := &http.Server{
		Addr:              endereco,
		Handler:           http.NewServeMux(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       5 * time.Minute,
		WriteTimeout:      5 * time.Minute,
		IdleTimeout:       2 * time.Minute,
		MaxHeaderBytes:    1 << 20,
	}

	erros := make(chan error, 1)
	go func() {
		fmt.Fprintf(os.Stderr, "servidor iniciando em: %s (versão %s)\n", endereco, versao)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			erros <- err
		}
		close(erros)
	}()

	select {
	case err := <-erros:
		return err
	case <-ctx.Done():
	}

	desligar, cancelar := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancelar()
	if err := srv.Shutdown(desligar); err != nil {
		return fmt.Errorf("encerrando servidor: %w", err)
	}
	return nil
}

// sondarSaude consulta /ping no endereço de escuta. É o comando invocado pelo
// HEALTHCHECK da imagem, que assim dispensa shell e utilitários de rede.
//
// ATENÇÃO: nesta fase o serviço responde 404 em /ping, porque ainda não há
// manipuladores. A sonda portanto reporta o contêiner como não saudável, e
// isso é o estado esperado da F1. A fase F9 registra a rota e a sonda passa a
// obter 200.
func sondarSaude(endereco string) int {
	cliente := &http.Client{Timeout: 3 * time.Second}
	resp, err := cliente.Get("http://" + endereco + "/ping")
	if err != nil {
		fmt.Fprintf(os.Stderr, "sonda: %v\n", err)
		return saidaDoenteHC
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		fmt.Fprintf(os.Stderr, "sonda: /ping devolveu %d (esperado 200)\n", resp.StatusCode)
		return saidaDoenteHC
	}
	return saidaLimpa
}
