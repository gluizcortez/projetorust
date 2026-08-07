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

	"github.com/jackc/pgx/v5/pgxpool"

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

// prepararEsquema recria o esquema `recorte` do zero no banco de teste.
//
// Existe porque estes testes NÃO podem depender do resíduo de outro pacote.
// Era o que acontecia: o esquema vinha de `internal/adapter/postgres`, que o
// recria a cada teste e o deixa no estado do último — e `internal/app`
// consultava as tabelas torcendo para que ainda estivessem lá. O sintoma era um
// **500 onde o teste espera 404**, porque a consulta batia em tabela ausente.
//
// O `-p 1` do alvo `test-integration` serializa os pacotes e evita que os dois
// se atropelem no meio; ele NÃO garante que o outro pacote deixou o esquema de
// pé. Esta função garante.
//
// Usa `db/init/`, que é o mesmo esquema INFERIDO que o compose aplica — D-13
// segue aberta, e o arquivo diz isso.
func prepararEsquema(t *testing.T, dsn string) {
	t.Helper()
	ctx := context.Background()

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("abrindo pool para preparar o esquema: %v", err)
	}
	defer pool.Close()

	if _, err := pool.Exec(ctx, `DROP SCHEMA IF EXISTS recorte CASCADE`); err != nil {
		t.Fatalf("limpando o esquema: %v", err)
	}
	for _, arquivo := range []string{
		"../../db/init/01-esquema.sql",
		"../../db/init/02-dados-de-exemplo.sql",
		"../../db/init/03-perfis-do-dou-real.sql",
	} {
		bruto, err := os.ReadFile(arquivo)
		if err != nil {
			t.Fatalf("lendo %s: %v", arquivo, err)
		}
		if _, err := pool.Exec(ctx, string(bruto)); err != nil {
			t.Fatalf("aplicando %s: %v", arquivo, err)
		}
	}
}

func configComBanco(t *testing.T) *config.Config {
	t.Helper()

	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL não definida — rode `make test-integration`")
	}
	prepararEsquema(t, dsn)

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

// -------------------------------------------------------------------------
// Fase F11 — a montagem com TODAS as chaves ligadas
// -------------------------------------------------------------------------

// TestIntegracaoTodasAsChavesLigadas monta o grafo com as dez evoluções ativas.
//
// Não é teste de comportamento de cada uma — isso está nos pacotes de origem. É
// teste de FIAÇÃO: com todas ligadas, o grafo precisa montar, subir, servir as
// rotas novas e encerrar limpo. Uma porta esquecida em app.Novo só aparece aqui.
func TestIntegracaoTodasAsChavesLigadas(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL não definida — rode `make test-integration`")
	}
	prepararEsquema(t, dsn)

	t.Setenv("DATABASE_URL", dsn)
	t.Setenv("API_KEY", "chave-de-integracao")
	t.Setenv("SERVIDOR_IP", "127.0.0.1")
	t.Setenv("SERVIDOR_PORTA", "0")

	for _, chave := range []string{
		"IDEMPOTENCIA_POR_HASH", "GRAVACAO_EM_LOTE", "VALIDAR_ASSINATURA_PDF",
		"VARREDURA_ORFAS", "RESPOSTA_PROBLEM_JSON", "STATUS_ENDPOINT", "HEALTH_ENDPOINTS",
	} {
		t.Setenv(chave, "true")
	}
	t.Setenv("MAX_UPLOAD_BYTES", "1048576")
	t.Setenv("MAX_IMPORTACOES_CONCORRENTES", "2")
	t.Setenv("RATE_LIMIT_RPS", "100")
	// Intervalo curto para que a varredura RODE de verdade durante o teste, em
	// vez de apenas ser montada.
	t.Setenv("VARREDURA_ORFAS_INTERVALO", "1s")
	t.Setenv("VARREDURA_ORFAS_LIMIAR", "1s")
	t.Setenv("VARREDURA_ORFAS_POLITICA", "observar")

	cfg, err := config.Carregar(context.Background())
	if err != nil {
		t.Fatalf("Carregar: %v", err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	ctx, cancelarMontagem := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancelarMontagem()

	servico, err := app.Novo(ctx, cfg, logger, nil, observability.NovasMetricas(),
		"integracao", "abc123", func() string { return "teste" })
	if err != nil {
		t.Fatalf("app.Novo com todas as chaves ligadas: %v", err)
	}

	execucao, sinal := context.WithCancel(context.Background())
	resultado := make(chan error, 1)
	go func() { resultado <- servico.Executar(execucao, nil) }()

	endereco := esperarPorta(t, servico)
	cliente := &http.Client{Timeout: 5 * time.Second}

	t.Run("ping continua exatamente igual", func(t *testing.T) {
		resp, err := cliente.Get("http://" + endereco + "/ping")
		if err != nil {
			t.Fatalf("GET /ping: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()

		corpo, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != http.StatusOK || string(corpo) != "pong" {
			t.Errorf("status %d, corpo %q; /ping é contrato existente", resp.StatusCode, corpo)
		}
	})

	t.Run("sondas de saúde respondem", func(t *testing.T) {
		for _, caminho := range []string{"/health/live", "/health/ready"} {
			resp, err := cliente.Get("http://" + endereco + caminho)
			if err != nil {
				t.Fatalf("GET %s: %v", caminho, err)
			}
			_ = resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				t.Errorf("%s devolveu %d; com banco de pé deveria aprovar", caminho, resp.StatusCode)
			}
		}
	})

	t.Run("status de importação inexistente devolve problem+json", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodGet, "http://"+endereco+"/importacao/999999", nil)
		req.Header.Set("X-API-KEY", "chave-de-integracao")
		resp, err := cliente.Do(req)
		if err != nil {
			t.Fatalf("GET /importacao/999999: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("status = %d; esperava 404", resp.StatusCode)
		}
		// RESPOSTA_PROBLEM_JSON também está ligada: os ERROS mudam de formato.
		if tipo := resp.Header.Get("Content-Type"); tipo != "application/problem+json" {
			t.Errorf("Content-Type = %q; esperava problem+json", tipo)
		}
	})

	// Dá tempo de a varredura rodar ao menos uma passagem — o que exercita a
	// transação, o SELECT ... FOR UPDATE SKIP LOCKED e o commit.
	time.Sleep(2500 * time.Millisecond)

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
