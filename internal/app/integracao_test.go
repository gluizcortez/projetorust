//go:build integration

package app_test

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gluizcortez/projetorust/internal/app"
	"github.com/gluizcortez/projetorust/internal/config"
	"github.com/gluizcortez/projetorust/internal/platform/observability"
)

// Os testes deste arquivo exercitam app.Novo — a montagem COMPLETA, com banco
// de verdade. Sem eles, a lista de quinze construções de Novo só seria
// verificada pelo teste de ponta a ponta, que roda o binário e não distingue
// qual peça faltou.
//
// Rodam com `make test-integration` ou com TEST_DATABASE_URL definida.

func configComBanco(t *testing.T) *config.Config {
	t.Helper()

	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL não definida — rode `make test-integration`")
	}

	t.Setenv("DATABASE_URL", dsn)
	t.Setenv("API_KEY", "chave-de-integracao")
	t.Setenv("SERVIDOR_IP", "127.0.0.1")
	t.Setenv("SERVIDOR_PORTA", "0")

	cfg, err := config.Carregar(context.Background())
	if err != nil {
		t.Fatalf("Carregar: %v", err)
	}
	return cfg
}

// TestIntegracaoMontagemCompleta monta o grafo inteiro e o exercita ponta a
// ponta dentro do processo: sobe, responde /ping, autentica /pdf e encerra.
func TestIntegracaoMontagemCompleta(t *testing.T) {
	cfg := configComBanco(t)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	ctx, cancelarMontagem := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancelarMontagem()

	servico, err := app.Novo(ctx, cfg, logger, nil, observability.NovasMetricas(),
		"integracao", "abc123", func() string { return "teste" })
	if err != nil {
		t.Fatalf("app.Novo: %v", err)
	}

	execucao, sinal := context.WithCancel(context.Background())
	resultado := make(chan error, 1)
	go func() { resultado <- servico.Executar(execucao, nil) }()

	endereco := esperarPorta(t, servico)
	cliente := &http.Client{Timeout: 5 * time.Second}

	t.Run("ping responde sem autenticação", func(t *testing.T) {
		resp, err := cliente.Get("http://" + endereco + "/ping")
		if err != nil {
			t.Fatalf("GET /ping: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()

		corpo, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != http.StatusOK || string(corpo) != "pong" {
			t.Errorf("status %d, corpo %q", resp.StatusCode, corpo)
		}
	})

	t.Run("pdf sem chave devolve 401", func(t *testing.T) {
		resp, err := cliente.Post("http://"+endereco+"/pdf", "multipart/form-data; boundary=x", nil)
		if err != nil {
			t.Fatalf("POST /pdf: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()

		corpo, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("status = %d; esperava 401", resp.StatusCode)
		}
		if string(corpo) != "Faltou a X-API-KEY" {
			t.Errorf("corpo = %q", corpo)
		}
	})

	t.Run("rota inexistente devolve o catcher", func(t *testing.T) {
		resp, err := cliente.Get("http://" + endereco + "/naoexiste")
		if err != nil {
			t.Fatalf("GET /naoexiste: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("status = %d; esperava 404", resp.StatusCode)
		}
		if tipo := resp.Header.Get("Content-Type"); tipo != "text/html" {
			t.Errorf("Content-Type = %q; esperava text/html", tipo)
		}
	})

	sinal()
	select {
	case err := <-resultado:
		if err != nil {
			t.Fatalf("Executar: %v", err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("Executar não retornou em 30s")
	}
}

// TestIntegracaoMontagemFechaOPoolAoFalhar: uma falha depois de abrir o pool
// não pode vazar conexões.
//
// A API_KEY vazia faz httpapi.NovoRouter recusar, que é a última etapa da
// montagem — e a única alcançável depois do pool sem alterar o código.
func TestIntegracaoMontagemFechaOPoolAoFalhar(t *testing.T) {
	cfg := configComBanco(t)
	cfg.APIKey = "" // invalida a etapa 14

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	ctx, cancelar := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancelar()

	_, err := app.Novo(ctx, cfg, logger, nil, nil, "t", "r", nil)
	if err == nil {
		t.Fatal("app.Novo aceitou APIKey vazia")
	}
	if !strings.Contains(err.Error(), "roteador") {
		t.Errorf("erro = %v; deveria apontar a etapa do roteador", err)
	}
}

func esperarPorta(t *testing.T, servico *app.App) string {
	t.Helper()
	limite := time.After(30 * time.Second)
	for {
		endereco := servico.Endereco()
		if endereco != "" && !strings.HasSuffix(endereco, ":0") {
			return endereco
		}
		select {
		case <-limite:
			t.Fatal("o servidor não subiu em 30s")
		default:
			time.Sleep(5 * time.Millisecond)
		}
	}
}
