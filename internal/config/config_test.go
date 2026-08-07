package config_test

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gluizcortez/projetorust/internal/config"
	"github.com/gluizcortez/projetorust/internal/usecase"
)

// variaveis são todas as que Carregar consulta. O ambiente de teste começa sem
// nenhuma delas, para que um valor herdado do processo não contamine o
// resultado.
var variaveis = []string{
	"SERVIDOR_IP", "SERVIDOR_PORTA", "DATABASE_URL", "API_KEY",
	"CONFIG_ESTRITA", "MAX_UPLOAD_BYTES", "MAX_IMPORTACOES_CONCORRENTES",
	"INDEX_MEMORIA_BYTES", "SHUTDOWN_TIMEOUT", "IDEMPOTENCIA_POR_HASH",
	"GRAVACAO_EM_LOTE", "VALIDAR_ASSINATURA_PDF", "VARREDURA_ORFAS",
	"RESPOSTA_PROBLEM_JSON", "RATE_LIMIT_RPS", "STATUS_ENDPOINT",
	"HEALTH_ENDPOINTS", "LOG_NIVEL", "LOG_FORMATO", "OTEL_EXPORTER_OTLP_ENDPOINT",
	"VARREDURA_ORFAS_INTERVALO", "VARREDURA_ORFAS_LIMIAR",
	"VARREDURA_ORFAS_POLITICA", "VARREDURA_ORFAS_LOTE",
	"HTTP_TEMPO_LIMITE_CABECALHO", "HTTP_TEMPO_LIMITE_LEITURA",
	"HTTP_TEMPO_LIMITE_ESCRITA", "HTTP_TEMPO_LIMITE_OCIOSO", "HTTP_MAX_HEADER_BYTES",
}

// ambienteLimpo remove todas as variáveis conhecidas e as restaura ao final.
func ambienteLimpo(t *testing.T) {
	t.Helper()
	anteriores := make(map[string]string, len(variaveis))
	for _, v := range variaveis {
		if valor, existia := os.LookupEnv(v); existia {
			anteriores[v] = valor
		}
		_ = os.Unsetenv(v)
	}
	t.Cleanup(func() {
		for _, v := range variaveis {
			if valor, ok := anteriores[v]; ok {
				_ = os.Setenv(v, valor)
			} else {
				_ = os.Unsetenv(v)
			}
		}
	})
}

// comSegredosValidos define apenas as duas variáveis obrigatórias.
func comSegredosValidos(t *testing.T) {
	t.Helper()
	t.Setenv("DATABASE_URL", "postgres://usuario:senha@banco:5432/recorte")
	t.Setenv("API_KEY", "chave-de-teste")
}

func carregar(t *testing.T) (*config.Config, error) {
	t.Helper()
	return config.Carregar(context.Background())
}

// -------------------------------------------------------------------------
// Padrões
// -------------------------------------------------------------------------

func TestPadroesReproduzemOLegado(t *testing.T) {
	ambienteLimpo(t)
	comSegredosValidos(t)

	cfg, err := carregar(t)
	if err != nil {
		t.Fatalf("carregar: %v", err)
	}

	casos := []struct {
		nome     string
		obtido   any
		esperado any
	}{
		// DIVERGE do legado, que ligava em 192.168.42.1 — endereço que só
		// existe na rede daquele serviço e impedia rodar local. `0.0.0.0` é um
		// superconjunto: atende também naquele endereço quando ele existe.
		{"ServidorIP", cfg.ServidorIP, "0.0.0.0"},
		{"ServidorPorta", cfg.ServidorPorta, uint16(6001)},
		{"Endereco", cfg.Endereco(), "0.0.0.0:6001"},
		// Os padrões de API_KEY e DATABASE_URL não entram aqui: este teste roda
		// com `comSegredosValidos`, que os define. Quem os cobre é
		// TestAmbienteVazioCarrega.
		{"ConfigEstrita", cfg.ConfigEstrita, false},
		{"MaxUploadBytes", cfg.MaxUploadBytes, int64(0)},
		{"MaxImportacoesConcorrentes", cfg.MaxImportacoesConcorrentes, 0},
		{"IndexMemoriaBytes", cfg.IndexMemoriaBytes, int64(500_000_000)},
		{"ShutdownTimeout", cfg.ShutdownTimeout, time.Duration(0)},
		{"IdempotenciaPorHash", cfg.IdempotenciaPorHash, false},
		{"GravacaoEmLote", cfg.GravacaoEmLote, false},
		{"ValidarAssinaturaPDF", cfg.ValidarAssinaturaPDF, false},
		{"VarreduraOrfas", cfg.VarreduraOrfas, false},
		{"RespostaProblemJSON", cfg.RespostaProblemJSON, false},
		{"RateLimitRPS", cfg.RateLimitRPS, 0},
		{"StatusEndpoint", cfg.StatusEndpoint, false},
		{"HealthEndpoints", cfg.HealthEndpoints, false},
		{"VarreduraOrfasIntervalo", cfg.VarreduraOrfasIntervalo, 15 * time.Minute},
		{"VarreduraOrfasLimiar", cfg.VarreduraOrfasLimiar, time.Hour},
		{"VarreduraOrfasPolitica", cfg.VarreduraOrfasPolitica, "observar"},
		{"VarreduraOrfasLote", cfg.VarreduraOrfasLote, 100},
		{"LogNivel", cfg.LogNivel, slog.LevelInfo},
		{"LogFormato", cfg.LogFormato, "json"},
		{"OTLPEndpoint", cfg.OTLPEndpoint, ""},
	}

	for _, c := range casos {
		if c.obtido != c.esperado {
			t.Errorf("%s = %v, esperado %v", c.nome, c.obtido, c.esperado)
		}
	}
}

// -------------------------------------------------------------------------
// A19 — porta inválida cai no padrão sem erro
// -------------------------------------------------------------------------

func TestPortaReproduzDefeitoA19(t *testing.T) {
	casos := []struct {
		valor          string
		esperada       uint16
		erroSemEstrita bool
		erroComEstrita bool
		observacao     string
	}{
		{"6001", 6001, false, false, "valor normal"},
		{"8080", 8080, false, false, "valor normal"},
		{"abc", 6001, false, true, "não numérico — DEFEITO PRESERVADO (A19)"},
		{"", 6001, false, true, "vazio"},
		{"70000", 6001, false, true, "acima de u16::MAX"},
		{"-1", 6001, false, true, "negativo"},
		{"6001.5", 6001, false, true, "decimal"},
		{"0", 0, false, false, "porta efêmera — valor válido, ver ESPECIFICACAO §1.1"},
		{"65535", 65535, false, false, "limite superior válido"},
	}

	for _, c := range casos {
		t.Run("sem_estrita/"+c.valor, func(t *testing.T) {
			ambienteLimpo(t)
			comSegredosValidos(t)
			t.Setenv("SERVIDOR_PORTA", c.valor)

			cfg, err := carregar(t)
			if c.erroSemEstrita {
				if err == nil {
					t.Fatalf("esperava erro para SERVIDOR_PORTA=%q (%s)", c.valor, c.observacao)
				}
				return
			}
			if err != nil {
				t.Fatalf("SERVIDOR_PORTA=%q (%s) não deveria falhar: %v", c.valor, c.observacao, err)
			}
			if cfg.ServidorPorta != c.esperada {
				t.Errorf("SERVIDOR_PORTA=%q -> %d, esperado %d (%s)",
					c.valor, cfg.ServidorPorta, c.esperada, c.observacao)
			}
		})

		t.Run("com_estrita/"+c.valor, func(t *testing.T) {
			ambienteLimpo(t)
			comSegredosValidos(t)
			t.Setenv("CONFIG_ESTRITA", "true")
			t.Setenv("SERVIDOR_PORTA", c.valor)

			_, err := carregar(t)
			if c.erroComEstrita && err == nil {
				t.Errorf("com CONFIG_ESTRITA=true, SERVIDOR_PORTA=%q deveria falhar (%s)",
					c.valor, c.observacao)
			}
			if !c.erroComEstrita && err != nil {
				t.Errorf("com CONFIG_ESTRITA=true, SERVIDOR_PORTA=%q não deveria falhar: %v",
					c.valor, err)
			}
		})
	}
}

// -------------------------------------------------------------------------
// O serviço sobe com o ambiente VAZIO
// -------------------------------------------------------------------------

// TestAmbienteVazioCarrega é o requisito de "rodar local sem preparo".
//
// Até aqui, `DATABASE_URL` e `API_KEY` eram obrigatórias e o serviço recusava
// subir sem elas. Agora as duas têm padrão — o da chave é o MESMO valor que o
// serviço em Rust trazia como constante no código-fonte.
func TestAmbienteVazioCarrega(t *testing.T) {
	ambienteLimpo(t)

	cfg, err := carregar(t)
	if err != nil {
		t.Fatalf("o ambiente vazio deveria carregar: %v", err)
	}

	if cfg.APIKey.Revelar() != config.APIKeyPadrao {
		t.Errorf("APIKey = %q; esperava o padrão", cfg.APIKey.Revelar())
	}
	if cfg.DatabaseURL.Revelar() != config.DatabaseURLPadrao {
		t.Errorf("DatabaseURL = %q; esperava o padrão", cfg.DatabaseURL.Revelar())
	}
}

// TestAmbienteSobrescreveOsPadroes: o padrão existe para o caso vazio, não para
// competir com o que o operador definir.
func TestAmbienteSobrescreveOsPadroes(t *testing.T) {
	ambienteLimpo(t)
	t.Setenv("API_KEY", "chave-de-producao")
	t.Setenv("DATABASE_URL", "postgres://outro:outro@10.0.0.9:5432/prod?sslmode=require")
	t.Setenv("SERVIDOR_IP", config.ServidorIPDoLegado)

	cfg, err := carregar(t)
	if err != nil {
		t.Fatalf("carregar: %v", err)
	}
	if cfg.APIKey.Revelar() != "chave-de-producao" {
		t.Errorf("APIKey = %q; o ambiente tem de vencer o padrão", cfg.APIKey.Revelar())
	}
	if !strings.Contains(cfg.DatabaseURL.Revelar(), "10.0.0.9") {
		t.Errorf("DatabaseURL = %q; o ambiente tem de vencer o padrão", cfg.DatabaseURL.Revelar())
	}
	// E o endereço do legado continua alcançável por configuração.
	if cfg.ServidorIP != "192.168.42.1" {
		t.Errorf("ServidorIP = %q; SERVIDOR_IP deveria restaurar o endereço do legado", cfg.ServidorIP)
	}
}

func TestErrosDeVariasVariaveisSaoAcumulados(t *testing.T) {
	ambienteLimpo(t)
	t.Setenv("MAX_UPLOAD_BYTES", "muitos")
	t.Setenv("SHUTDOWN_TIMEOUT", "pra sempre")
	t.Setenv("LOG_NIVEL", "gritante")
	t.Setenv("LOG_FORMATO", "xml")
	t.Setenv("GRAVACAO_EM_LOTE", "talvez")

	_, err := carregar(t)
	if err == nil {
		t.Fatal("esperava erro")
	}
	erroCfg, ok := config.ComoErroDeConfiguracao(err)
	if !ok {
		t.Fatalf("esperava *ErroDeConfiguracao, obtive %T", err)
	}
	// As cinco malformadas. Nenhuma variável é obrigatória: `DATABASE_URL` e
	// `API_KEY` passaram a ter padrão.
	if len(erroCfg.Problemas) != 5 {
		t.Errorf("esperava 5 problemas acumulados, obtive %d:\n%v",
			len(erroCfg.Problemas), erroCfg.Problemas)
	}
}

// -------------------------------------------------------------------------
// Redação de segredos
// -------------------------------------------------------------------------

func TestSegredosNuncaVazamEmNenhumaSaida(t *testing.T) {
	const (
		senhaReal = "S3nh4-Sup3r-S3cr3t4-do-Banco"
		chaveReal = "01956cb2-2f85-7440-9767-1a6651c10e0f"
	)

	ambienteLimpo(t)
	t.Setenv("DATABASE_URL", "postgres://appuser:"+senhaReal+"@db.interno:5432/recorte?sslmode=require")
	t.Setenv("API_KEY", chaveReal)

	cfg, err := carregar(t)
	if err != nil {
		t.Fatalf("carregar: %v", err)
	}

	jsonBytes, err := json.Marshal(map[string]any{
		"database_url": cfg.DatabaseURL,
		"api_key":      cfg.APIKey,
	})
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	var registro strings.Builder
	slog.New(slog.NewJSONHandler(&registro, nil)).Info("configuração", "cfg", cfg)

	// O verbo passa por variável de propósito: a redundância de fmt.Sprintf("%s", x)
	// é exatamente o caminho que este teste precisa exercitar, e o staticcheck
	// a sinalizaria se o formato fosse constante.
	comVerbo := func(verbo string, v any) string { return fmt.Sprintf(verbo, v) }

	saidas := map[string]string{
		"Config.String()":   cfg.String(),
		"Config.LogValue()": fmt.Sprintf("%v", cfg.LogValue()),
		"registro slog":     registro.String(),
		"json.Marshal":      string(jsonBytes),
		"%v da URL":         comVerbo("%v", cfg.DatabaseURL),
		"%s da URL":         comVerbo("%s", cfg.DatabaseURL),
		"%#v da URL":        comVerbo("%#v", cfg.DatabaseURL),
		"%v da chave":       comVerbo("%v", cfg.APIKey),
		"%s da chave":       comVerbo("%s", cfg.APIKey),
		"%#v da chave":      comVerbo("%#v", cfg.APIKey),
		"URL.String()":      cfg.DatabaseURL.String(),
		"chave.String()":    cfg.APIKey.String(),
	}

	// Além do valor inteiro, checamos prefixos: revelar os primeiros
	// caracteres de uma credencial é revelar parte dela.
	proibidos := []string{
		senhaReal, senhaReal[:12], senhaReal[:8],
		chaveReal, chaveReal[:16], chaveReal[:8],
	}

	for origem, texto := range saidas {
		for _, p := range proibidos {
			if strings.Contains(texto, p) {
				t.Errorf("VAZAMENTO em %s: contém %q\n  saída: %s", origem, p, texto)
			}
		}
	}

	// A URL redigida deve preservar o que é útil em diagnóstico.
	redigida := cfg.DatabaseURL.String()
	for _, util := range []string{"db.interno", "5432", "recorte", "appuser"} {
		if !strings.Contains(redigida, util) {
			t.Errorf("a URL redigida deveria preservar %q para diagnóstico; obtive %s", util, redigida)
		}
	}

	// E Revelar deve continuar devolvendo o valor real.
	if !strings.Contains(cfg.DatabaseURL.Revelar(), senhaReal) {
		t.Error("Revelar() deveria devolver a URL completa")
	}
	if cfg.APIKey.Revelar() != chaveReal {
		t.Error("Revelar() deveria devolver a chave completa")
	}
}

// TestErroDeConfiguracaoNaoEcoaSegredo.
//
// A propriedade continua valendo mesmo agora que os dois segredos têm padrão:
// a mensagem de erro acumulada é montada com o NOME das variáveis, e um valor
// secreto definido no ambiente não pode aparecer nela por tabela.
func TestErroDeConfiguracaoNaoEcoaSegredo(t *testing.T) {
	const chaveReal = "chave-secreta-que-nao-pode-vazar"
	const urlReal = "postgres://usuario:senha-secreta@10.0.0.9:5432/prod"

	ambienteLimpo(t)
	t.Setenv("API_KEY", chaveReal)
	t.Setenv("DATABASE_URL", urlReal)
	// Uma variável malformada, para que HAJA erro a inspecionar.
	t.Setenv("LOG_NIVEL", "gritante")

	_, err := carregar(t)
	if err == nil {
		t.Fatal("esperava erro para LOG_NIVEL inválido")
	}
	msg := err.Error()
	for _, segredo := range []string{chaveReal, "senha-secreta", urlReal} {
		if strings.Contains(msg, segredo) {
			t.Errorf("a mensagem de erro vazou %q: %s", segredo, msg)
		}
	}
	for _, nome := range []string{"LOG_NIVEL"} {
		if !strings.Contains(msg, nome) {
			t.Errorf("a mensagem deveria mencionar %s: %s", nome, msg)
		}
	}
}

func TestURLMalformadaFicaTotalmenteOpaca(t *testing.T) {
	casos := []struct {
		nome string
		url  string
	}{
		{"sem esquema", "usuario:senha@host/base"},
		{"lixo", "::::"},
		{"só texto", "isto-nao-e-uma-url-com-senha-embutida"},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			u := config.URLSegredo(c.url)
			if got := u.String(); got != config.MarcadorRedigido {
				t.Errorf("URL malformada deveria ficar opaca; obtive %q", got)
			}
		})
	}
}

// -------------------------------------------------------------------------
// Chaves de recurso e demais analisadores
// -------------------------------------------------------------------------

func TestChavesDeRecursoLigam(t *testing.T) {
	ambienteLimpo(t)
	comSegredosValidos(t)
	for _, v := range []string{
		"IDEMPOTENCIA_POR_HASH", "GRAVACAO_EM_LOTE", "VALIDAR_ASSINATURA_PDF",
		"VARREDURA_ORFAS", "RESPOSTA_PROBLEM_JSON", "STATUS_ENDPOINT",
		"HEALTH_ENDPOINTS", "CONFIG_ESTRITA",
	} {
		t.Setenv(v, "true")
	}
	t.Setenv("MAX_UPLOAD_BYTES", "104857600")
	t.Setenv("MAX_IMPORTACOES_CONCORRENTES", "4")
	t.Setenv("RATE_LIMIT_RPS", "50")
	t.Setenv("SHUTDOWN_TIMEOUT", "45s")

	cfg, err := carregar(t)
	if err != nil {
		t.Fatalf("carregar: %v", err)
	}
	if !cfg.IdempotenciaPorHash || !cfg.GravacaoEmLote || !cfg.ValidarAssinaturaPDF ||
		!cfg.VarreduraOrfas || !cfg.RespostaProblemJSON || !cfg.StatusEndpoint ||
		!cfg.HealthEndpoints || !cfg.ConfigEstrita {
		t.Error("todas as chaves booleanas deveriam estar ligadas")
	}
	if cfg.MaxUploadBytes != 104857600 {
		t.Errorf("MaxUploadBytes = %d", cfg.MaxUploadBytes)
	}
	if cfg.MaxImportacoesConcorrentes != 4 {
		t.Errorf("MaxImportacoesConcorrentes = %d", cfg.MaxImportacoesConcorrentes)
	}
	if cfg.RateLimitRPS != 50 {
		t.Errorf("RateLimitRPS = %d", cfg.RateLimitRPS)
	}
	if cfg.ShutdownTimeout != 45*time.Second {
		t.Errorf("ShutdownTimeout = %s", cfg.ShutdownTimeout)
	}
}

func TestDuracaoAceitaSegundosSimples(t *testing.T) {
	casos := map[string]time.Duration{
		"30":    30 * time.Second,
		"30s":   30 * time.Second,
		"2m":    2 * time.Minute,
		"1h30m": 90 * time.Minute,
		"0":     0,
	}
	for entrada, esperado := range casos {
		t.Run(entrada, func(t *testing.T) {
			ambienteLimpo(t)
			comSegredosValidos(t)
			t.Setenv("SHUTDOWN_TIMEOUT", entrada)
			cfg, err := carregar(t)
			if err != nil {
				t.Fatalf("SHUTDOWN_TIMEOUT=%q: %v", entrada, err)
			}
			if cfg.ShutdownTimeout != esperado {
				t.Errorf("SHUTDOWN_TIMEOUT=%q -> %s, esperado %s", entrada, cfg.ShutdownTimeout, esperado)
			}
		})
	}
}

func TestNivelDeLog(t *testing.T) {
	casos := map[string]slog.Level{
		"debug":   slog.LevelDebug,
		"DEBUG":   slog.LevelDebug,
		"info":    slog.LevelInfo,
		"warn":    slog.LevelWarn,
		"warning": slog.LevelWarn,
		"error":   slog.LevelError,
		"erro":    slog.LevelError,
	}
	for entrada, esperado := range casos {
		t.Run(entrada, func(t *testing.T) {
			ambienteLimpo(t)
			comSegredosValidos(t)
			t.Setenv("LOG_NIVEL", entrada)
			cfg, err := carregar(t)
			if err != nil {
				t.Fatalf("LOG_NIVEL=%q: %v", entrada, err)
			}
			if cfg.LogNivel != esperado {
				t.Errorf("LOG_NIVEL=%q -> %v, esperado %v", entrada, cfg.LogNivel, esperado)
			}
		})
	}
}

func TestValoresNegativosSaoRejeitados(t *testing.T) {
	for _, v := range []string{"MAX_UPLOAD_BYTES", "MAX_IMPORTACOES_CONCORRENTES", "RATE_LIMIT_RPS"} {
		t.Run(v, func(t *testing.T) {
			ambienteLimpo(t)
			comSegredosValidos(t)
			t.Setenv(v, "-1")
			if _, err := carregar(t); err == nil {
				t.Errorf("%s=-1 deveria falhar", v)
			}
		})
	}
}

func TestContextoCanceladoInterrompe(t *testing.T) {
	ambienteLimpo(t)
	comSegredosValidos(t)
	ctx, cancelar := context.WithCancel(context.Background())
	cancelar()
	if _, err := config.Carregar(ctx); err == nil {
		t.Error("contexto cancelado deveria interromper a carga")
	}
}

// TestTemposLimiteHTTPPadrao fixa a escolha da fase F9: leitura e escrita
// nascem SEM limite, porque qualquer valor finito cortaria o envio de um diário
// grande por enlace lento. Ver internal/adapter/httpapi/servidor.go e D-04.
func TestTemposLimiteHTTPPadrao(t *testing.T) {
	ambienteLimpo(t)
	t.Setenv("DATABASE_URL", "postgres://u:s@h:5432/d")
	t.Setenv("API_KEY", "chave")

	cfg, err := config.Carregar(context.Background())
	if err != nil {
		t.Fatalf("Carregar: %v", err)
	}

	if cfg.HTTPTempoLimiteDeCabecalho != 10*time.Second {
		t.Errorf("cabeçalho = %v; esperava 10s", cfg.HTTPTempoLimiteDeCabecalho)
	}
	if cfg.HTTPTempoLimiteDeLeitura != 0 {
		t.Errorf("leitura = %v; deve nascer SEM limite", cfg.HTTPTempoLimiteDeLeitura)
	}
	if cfg.HTTPTempoLimiteDeEscrita != 0 {
		t.Errorf("escrita = %v; deve nascer SEM limite", cfg.HTTPTempoLimiteDeEscrita)
	}
	if cfg.HTTPTempoLimiteOcioso != 120*time.Second {
		t.Errorf("ocioso = %v; esperava 120s", cfg.HTTPTempoLimiteOcioso)
	}
	if cfg.HTTPMaxHeaderBytes != 1<<20 {
		t.Errorf("max_header_bytes = %d", cfg.HTTPMaxHeaderBytes)
	}
}

// TestTemposLimiteHTTPConfiguraveis: quem souber o tamanho máximo real dos
// documentos (D-04) pode fechar os dois zeros.
func TestTemposLimiteHTTPConfiguraveis(t *testing.T) {
	ambienteLimpo(t)
	t.Setenv("DATABASE_URL", "postgres://u:s@h:5432/d")
	t.Setenv("API_KEY", "chave")
	t.Setenv("HTTP_TEMPO_LIMITE_LEITURA", "5m")
	t.Setenv("HTTP_TEMPO_LIMITE_ESCRITA", "90")

	cfg, err := config.Carregar(context.Background())
	if err != nil {
		t.Fatalf("Carregar: %v", err)
	}
	if cfg.HTTPTempoLimiteDeLeitura != 5*time.Minute {
		t.Errorf("leitura = %v; esperava 5m", cfg.HTTPTempoLimiteDeLeitura)
	}
	// Inteiro simples é interpretado como segundos.
	if cfg.HTTPTempoLimiteDeEscrita != 90*time.Second {
		t.Errorf("escrita = %v; esperava 90s", cfg.HTTPTempoLimiteDeEscrita)
	}
}

// -------------------------------------------------------------------------
// Fase F11 — ajustes da varredura de órfãs
// -------------------------------------------------------------------------

// TestPoliticasAcompanhamOCasoDeUso guarda a duplicação deliberada.
//
// A lista de políticas vive em dois lugares: aqui, para a validação da
// configuração, e em internal/usecase, que é quem as implementa. Fazer
// internal/config depender de internal/usecase inverteria a direção das
// dependências por causa de duas cadeias de texto — mas duas listas soltas
// divergem em silêncio, e este teste é o que impede isso.
func TestPoliticasAcompanhamOCasoDeUso(t *testing.T) {
	doCasoDeUso := make([]string, 0, len(usecase.PoliticasDeVarredura))
	for _, p := range usecase.PoliticasDeVarredura {
		doCasoDeUso = append(doCasoDeUso, string(p))
	}

	if len(doCasoDeUso) != len(config.PoliticasDeVarreduraAceitas) {
		t.Fatalf("config aceita %v; o caso de uso implementa %v",
			config.PoliticasDeVarreduraAceitas, doCasoDeUso)
	}
	for i, esperada := range doCasoDeUso {
		if config.PoliticasDeVarreduraAceitas[i] != esperada {
			t.Errorf("posição %d: config diz %q, o caso de uso diz %q",
				i, config.PoliticasDeVarreduraAceitas[i], esperada)
		}
	}

	// E o padrão da configuração precisa ser uma delas.
	if !usecase.PoliticaValida(config.VarreduraPoliticaPadrao) {
		t.Errorf("o padrão %q não é uma política implementada", config.VarreduraPoliticaPadrao)
	}
}

// TestPoliticaDesconhecidaEhRecusada.
func TestPoliticaDesconhecidaEhRecusada(t *testing.T) {
	ambienteLimpo(t)
	comSegredosValidos(t)
	// `reprocessar` é a tentação óbvia — e é justamente a que não existe,
	// porque o documento não é arquivado. Ver docs/DECISOES-ABERTAS.md, D-21.
	t.Setenv("VARREDURA_ORFAS_POLITICA", "reprocessar")

	_, err := carregar(t)
	if err == nil {
		t.Fatal("carregar aceitou uma política desconhecida")
	}
	if !strings.Contains(err.Error(), "VARREDURA_ORFAS_POLITICA") {
		t.Errorf("erro = %v; deveria nomear a variável", err)
	}
}

// TestVarreduraLigadaExigeIntervaloELimiarPositivos.
//
// Zero é aceito nas demais durações porque significa "sem limite". Aqui não: um
// intervalo de zero faria o laço girar sem pausa, e um limiar de zero
// consideraria presa TODA importação em curso — que é o pior desfecho possível,
// já que a política de erro é irreversível.
func TestVarreduraLigadaExigeIntervaloELimiarPositivos(t *testing.T) {
	casos := []struct {
		nome     string
		variavel string
	}{
		{"intervalo zero", "VARREDURA_ORFAS_INTERVALO"},
		{"limiar zero", "VARREDURA_ORFAS_LIMIAR"},
	}

	for _, caso := range casos {
		t.Run(caso.nome, func(t *testing.T) {
			ambienteLimpo(t)
			comSegredosValidos(t)
			t.Setenv("VARREDURA_ORFAS", "true")
			t.Setenv(caso.variavel, "0")

			_, err := carregar(t)
			if err == nil {
				t.Fatalf("carregar aceitou %s=0 com a varredura ligada", caso.variavel)
			}
			if !strings.Contains(err.Error(), caso.variavel) {
				t.Errorf("erro = %v; deveria nomear %s", err, caso.variavel)
			}
		})
	}
}

// TestVarreduraDesligadaNaoValidaOsAjustes.
//
// Com a chave no padrão, os ajustes não têm efeito algum — e recusar o arranque
// por causa deles seria transformar uma chave desligada em quebra de serviço.
func TestVarreduraDesligadaNaoValidaOsAjustes(t *testing.T) {
	ambienteLimpo(t)
	comSegredosValidos(t)
	t.Setenv("VARREDURA_ORFAS_INTERVALO", "0")
	t.Setenv("VARREDURA_ORFAS_LIMIAR", "0")

	if _, err := carregar(t); err != nil {
		t.Errorf("carregar recusou ajustes irrelevantes com a varredura desligada: %v", err)
	}
}

// TestAjustesDaVarreduraLigam.
func TestAjustesDaVarreduraLigam(t *testing.T) {
	ambienteLimpo(t)
	comSegredosValidos(t)
	t.Setenv("VARREDURA_ORFAS", "true")
	t.Setenv("VARREDURA_ORFAS_INTERVALO", "5m")
	t.Setenv("VARREDURA_ORFAS_LIMIAR", "2h")
	t.Setenv("VARREDURA_ORFAS_POLITICA", "erro")
	t.Setenv("VARREDURA_ORFAS_LOTE", "25")

	cfg, err := carregar(t)
	if err != nil {
		t.Fatalf("carregar: %v", err)
	}
	if cfg.VarreduraOrfasIntervalo != 5*time.Minute {
		t.Errorf("intervalo = %s", cfg.VarreduraOrfasIntervalo)
	}
	if cfg.VarreduraOrfasLimiar != 2*time.Hour {
		t.Errorf("limiar = %s", cfg.VarreduraOrfasLimiar)
	}
	if cfg.VarreduraOrfasPolitica != "erro" {
		t.Errorf("política = %q", cfg.VarreduraOrfasPolitica)
	}
	if cfg.VarreduraOrfasLote != 25 {
		t.Errorf("lote = %d", cfg.VarreduraOrfasLote)
	}
}
