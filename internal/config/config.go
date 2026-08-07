package config

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

// Padrões do legado. Ver reference/main.rs:24-25 e docs/ESPECIFICACAO.md §1.1.
const (
	// ServidorIPPadrao DIVERGE do legado, que usava `192.168.42.1`.
	//
	// Aquele endereço só existe na rede em que o serviço original rodava: numa
	// máquina de desenvolvimento ou dentro de um contêiner, ligar nele falha com
	// "cannot assign requested address" e o processo não sobe. Era o principal
	// impedimento para rodar local.
	//
	// `0.0.0.0` é um SUPERCONJUNTO: ele atende em todas as interfaces, inclusive
	// na `192.168.42.1` quando ela existe. Nenhuma requisição que chegava ao
	// serviço antes deixa de chegar agora — o que muda é que ele também atende
	// nas demais. Definir SERVIDOR_IP restaura qualquer endereço específico.
	ServidorIPPadrao    = "0.0.0.0"
	ServidorPortaPadrao = uint16(6001)

	// ServidorIPDoLegado é o endereço que o serviço original usava.
	//
	// Preservado como constante para quem precisar reproduzir exatamente aquele
	// comportamento: `SERVIDOR_IP=192.168.42.1`.
	ServidorIPDoLegado = "192.168.42.1"

	// IndexMemoriaBytesPadrao reproduz o orçamento do escritor de índice do
	// legado (reference/main.rs:519). No índice próprio da fase F7 o valor não
	// tem o mesmo efeito; é mantido para comparação e dimensionamento.
	IndexMemoriaBytesPadrao = int64(500_000_000)
)

// Padrões da varredura de órfãs (VARREDURA_ORFAS). Não têm equivalente no
// legado — ele não varre nada.
const (
	// VarreduraIntervaloPadrao é de quanto em quanto tempo a varredura roda.
	//
	// Quinze minutos é bem mais lento que o tempo de processamento de um
	// diário, então uma importação normal nunca é vista duas vezes pela
	// varredura, e é frequente o bastante para uma presa não passar um turno
	// inteiro despercebida.
	VarreduraIntervaloPadrao = 15 * time.Minute

	// VarreduraLimiarPadrao é a idade mínima de data_inicio para uma importação
	// ser considerada presa.
	//
	// Uma hora é MUITO acima do pior caso conhecido de processamento e é
	// deliberadamente conservador: o custo de marcar como erro uma importação
	// que estava apenas lenta é alto — o documento não existe mais e não há como
	// refazê-la —, enquanto o custo de demorar mais uma hora para detectar uma
	// presa é nenhum, porque ela está parada desde sempre. O valor definitivo
	// depende de D-04, que mede o tempo real de processamento em produção.
	VarreduraLimiarPadrao = time.Hour

	// VarreduraPoliticaPadrao apenas observa: registra e conta, sem alterar
	// estado. Ver usecase.PoliticaObservar.
	VarreduraPoliticaPadrao = "observar"

	// VarreduraLotePadrao é quantas linhas uma passagem tranca.
	VarreduraLotePadrao = 100
)

// APIKeyPadrao é a credencial de `X-API-KEY` quando a variável não é definida.
//
// # É o MESMO valor do serviço em Rust
//
// Lá ela era uma constante no código-fonte — `const API_KEY: &str = "01956..."`
// — e não vinha do ambiente. Aqui ela é um PADRÃO: o valor é o mesmo, e
// definir `API_KEY` continua sobrescrevendo. `docker-compose.yml` a declara
// explicitamente.
//
// # O que isso significa, dito sem rodeio
//
// A chave está no repositório. Quem tem o código tem a credencial. Isso não é
// uma regressão em relação ao original — que a trazia embutida no binário, de
// onde qualquer um a extrairia com `strings` —, mas também não vira segredo por
// estar num arquivo diferente.
//
// **Para uma implantação real, defina `API_KEY` no ambiente.** O padrão existe
// para que o serviço rode sem configuração alguma, não para ser usado como
// credencial de produção.
//
// — ver o parágrafo acima. Suprimido aqui para que a decisão fique num lugar só.
//
//nolint:gosec // G101: é credencial em claro DE PROPÓSITO, e o aviso está certo
const APIKeyPadrao = "01956cb2-2f85-7440-9767-1a6651c10e0f"

// DatabaseURLPadrao aponta para um PostgreSQL local.
//
// O legado exigia `DATABASE_URL` e entrava em pânico sem ela
// (reference/main.rs:36). O padrão daqui aponta para o banco que o
// `docker-compose.yml` sobe, e é o que permite `go run ./cmd/recorte-api`
// funcionar numa máquina de desenvolvimento sem preparo.
//
// Em produção, defina a variável. Um padrão apontando para `localhost` não
// causa dano silencioso — ele falha ruidosamente no arranque, porque não há
// banco ali.
//
// para desenvolvimento. Não há segredo a proteger num banco descartável.
//
//nolint:gosec // G101: a senha é `recorte`, do PostgreSQL que o compose sobe
const DatabaseURLPadrao = "postgres://recorte:recorte@localhost:5432/recorte?sslmode=disable"

// PoliticasDeVarreduraAceitas são os valores de VARREDURA_ORFAS_POLITICA.
//
// A lista é DUPLICADA de usecase.PoliticasDeVarredura de propósito: fazer
// internal/config depender de internal/usecase inverteria a direção das
// dependências por causa de duas cadeias de texto. A duplicação é guardada por
// TestPoliticasAcompanhamOCasoDeUso, que falha se as duas listas divergirem.
var PoliticasDeVarreduraAceitas = []string{"observar", "erro"}

// Config é toda a configuração do serviço.
//
// Não há variável global: a instância é criada em main e injetada. Ver a
// invariante 4 do preâmbulo do projeto.
type Config struct {
	// --- servidor (comportamento do legado) ---
	ServidorIP    string
	ServidorPorta uint16

	// --- segredos ---
	DatabaseURL URLSegredo
	APIKey      Segredo

	// --- tempos limite do servidor HTTP ---
	//
	// O legado não define nenhum: usa os padrões do Salvo/hyper. Os de LEITURA
	// e ESCRITA nascem em ZERO — sem limite — porque qualquer valor finito
	// cortaria o envio de um diário grande por enlace lento, que é mudança de
	// comportamento observável. Fechá-los depende de saber o tamanho máximo
	// real dos documentos, que é a decisão aberta D-04.
	HTTPTempoLimiteDeCabecalho time.Duration
	HTTPTempoLimiteDeLeitura   time.Duration
	HTTPTempoLimiteDeEscrita   time.Duration
	HTTPTempoLimiteOcioso      time.Duration
	HTTPMaxHeaderBytes         int

	// --- observabilidade ---
	LogNivel     slog.Level
	LogFormato   string
	OTLPEndpoint string

	// --- chaves de recurso ---
	//
	// TODAS têm padrão que reproduz exatamente o comportamento do legado.
	// Ligar qualquer uma é mudança de comportamento e exige o teste de paridade
	// nos dois estados. Ver docs/roadmap-migracao-rust-go.html §8.1.
	ConfigEstrita              bool
	MaxUploadBytes             int64
	MaxImportacoesConcorrentes int
	IndexMemoriaBytes          int64
	ShutdownTimeout            time.Duration
	IdempotenciaPorHash        bool
	GravacaoEmLote             bool
	ValidarAssinaturaPDF       bool
	VarreduraOrfas             bool
	RespostaProblemJSON        bool
	RateLimitRPS               int
	StatusEndpoint             bool
	HealthEndpoints            bool

	// --- ajustes da varredura de órfãs ---
	//
	// Só têm efeito com VARREDURA_ORFAS ligada. Os padrões são conservadores de
	// propósito: a política PADRÃO apenas OBSERVA, sem alterar estado algum, para
	// que ligar a chave meça o problema antes de agir sobre ele.
	VarreduraOrfasIntervalo time.Duration
	VarreduraOrfasLimiar    time.Duration
	VarreduraOrfasPolitica  string
	VarreduraOrfasLote      int
}

// Endereco é o ponto de escuta, no formato host:porta.
func (c Config) Endereco() string {
	return net.JoinHostPort(c.ServidorIP, strconv.FormatUint(uint64(c.ServidorPorta), 10))
}

// LogValue satisfaz slog.LogValuer. Os segredos saem redigidos.
func (c Config) LogValue() slog.Value {
	return slog.GroupValue(
		slog.String("servidor", c.Endereco()),
		slog.String("database_url", c.DatabaseURL.String()),
		slog.String("api_key", c.APIKey.String()),
		slog.String("log_nivel", c.LogNivel.String()),
		slog.String("log_formato", c.LogFormato),
		slog.Bool("otel_ativo", c.OTLPEndpoint != ""),
		slog.Group("chaves",
			slog.Bool("config_estrita", c.ConfigEstrita),
			slog.Duration("http_tempo_limite_cabecalho", c.HTTPTempoLimiteDeCabecalho),
			slog.Duration("http_tempo_limite_leitura", c.HTTPTempoLimiteDeLeitura),
			slog.Duration("http_tempo_limite_escrita", c.HTTPTempoLimiteDeEscrita),
			slog.Duration("http_tempo_limite_ocioso", c.HTTPTempoLimiteOcioso),
			slog.Int("http_max_header_bytes", c.HTTPMaxHeaderBytes),
			slog.Int64("max_upload_bytes", c.MaxUploadBytes),
			slog.Int("max_importacoes_concorrentes", c.MaxImportacoesConcorrentes),
			slog.Int64("index_memoria_bytes", c.IndexMemoriaBytes),
			slog.Duration("shutdown_timeout", c.ShutdownTimeout),
			slog.Bool("idempotencia_por_hash", c.IdempotenciaPorHash),
			slog.Bool("gravacao_em_lote", c.GravacaoEmLote),
			slog.Bool("validar_assinatura_pdf", c.ValidarAssinaturaPDF),
			slog.Bool("varredura_orfas", c.VarreduraOrfas),
			slog.Bool("resposta_problem_json", c.RespostaProblemJSON),
			slog.Int("rate_limit_rps", c.RateLimitRPS),
			slog.Bool("status_endpoint", c.StatusEndpoint),
			slog.Bool("health_endpoints", c.HealthEndpoints),
			slog.Duration("varredura_orfas_intervalo", c.VarreduraOrfasIntervalo),
			slog.Duration("varredura_orfas_limiar", c.VarreduraOrfasLimiar),
			slog.String("varredura_orfas_politica", c.VarreduraOrfasPolitica),
			slog.Int("varredura_orfas_lote", c.VarreduraOrfasLote),
		),
	)
}

// String satisfaz fmt.Stringer. Os segredos saem redigidos.
func (c Config) String() string {
	var b strings.Builder
	b.WriteString("Config{")
	b.WriteString("servidor=" + c.Endereco())
	b.WriteString(", " + descreverPara("database_url", c.DatabaseURL))
	b.WriteString(", " + descreverPara("api_key", c.APIKey))
	b.WriteString(", log=" + c.LogNivel.String() + "/" + c.LogFormato)
	fmt.Fprintf(&b, ", otel_ativo=%t", c.OTLPEndpoint != "")
	fmt.Fprintf(&b, ", config_estrita=%t", c.ConfigEstrita)
	fmt.Fprintf(&b, ", http_tempo_limite_cabecalho=%s", c.HTTPTempoLimiteDeCabecalho)
	fmt.Fprintf(&b, ", http_tempo_limite_leitura=%s", c.HTTPTempoLimiteDeLeitura)
	fmt.Fprintf(&b, ", http_tempo_limite_escrita=%s", c.HTTPTempoLimiteDeEscrita)
	fmt.Fprintf(&b, ", http_tempo_limite_ocioso=%s", c.HTTPTempoLimiteOcioso)
	fmt.Fprintf(&b, ", http_max_header_bytes=%d", c.HTTPMaxHeaderBytes)
	fmt.Fprintf(&b, ", max_upload_bytes=%d", c.MaxUploadBytes)
	fmt.Fprintf(&b, ", max_importacoes_concorrentes=%d", c.MaxImportacoesConcorrentes)
	fmt.Fprintf(&b, ", index_memoria_bytes=%d", c.IndexMemoriaBytes)
	fmt.Fprintf(&b, ", shutdown_timeout=%s", c.ShutdownTimeout)
	fmt.Fprintf(&b, ", idempotencia_por_hash=%t", c.IdempotenciaPorHash)
	fmt.Fprintf(&b, ", gravacao_em_lote=%t", c.GravacaoEmLote)
	fmt.Fprintf(&b, ", validar_assinatura_pdf=%t", c.ValidarAssinaturaPDF)
	fmt.Fprintf(&b, ", varredura_orfas=%t", c.VarreduraOrfas)
	fmt.Fprintf(&b, ", resposta_problem_json=%t", c.RespostaProblemJSON)
	fmt.Fprintf(&b, ", rate_limit_rps=%d", c.RateLimitRPS)
	fmt.Fprintf(&b, ", status_endpoint=%t", c.StatusEndpoint)
	fmt.Fprintf(&b, ", health_endpoints=%t", c.HealthEndpoints)
	fmt.Fprintf(&b, ", varredura_orfas_intervalo=%s", c.VarreduraOrfasIntervalo)
	fmt.Fprintf(&b, ", varredura_orfas_limiar=%s", c.VarreduraOrfasLimiar)
	fmt.Fprintf(&b, ", varredura_orfas_politica=%s", c.VarreduraOrfasPolitica)
	fmt.Fprintf(&b, ", varredura_orfas_lote=%d", c.VarreduraOrfasLote)
	b.WriteString("}")
	return b.String()
}

// ErroDeConfiguracao reúne TODOS os problemas encontrados na leitura, para que
// o operador corrija tudo de uma vez em vez de descobrir um por execução.
//
// Nenhuma mensagem contém valor de variável marcada como sensível.
type ErroDeConfiguracao struct {
	Problemas []string
}

func (e *ErroDeConfiguracao) Error() string {
	if len(e.Problemas) == 1 {
		return "configuração inválida: " + e.Problemas[0]
	}
	return fmt.Sprintf(
		"configuração inválida (%d problemas):\n  - %s",
		len(e.Problemas),
		strings.Join(e.Problemas, "\n  - "),
	)
}

// Carregar lê a configuração do ambiente, com .env opcional.
//
// Precedência: o ambiente do processo vence o .env, reproduzindo o
// comportamento de dotenv().ok() do legado (reference/main.rs:32) — a ausência
// do arquivo é silenciosa.
//
// A validação é completa: todos os problemas são acumulados antes de falhar.
func Carregar(ctx context.Context) (*Config, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("carregando configuração: %w", err)
	}

	// godotenv.Load não sobrescreve variáveis já presentes no ambiente, que é a
	// mesma precedência do dotenvy. A ausência do arquivo é ignorada.
	_ = godotenv.Load()

	l := &leitor{}

	cfg := &Config{}

	// CONFIG_ESTRITA é lida primeiro: ela governa como as demais tratam valor
	// malformado.
	cfg.ConfigEstrita = l.booleano("CONFIG_ESTRITA", false)

	// --- servidor: comportamento do legado, preservado ---
	cfg.ServidorIP = l.texto("SERVIDOR_IP", ServidorIPPadrao)
	cfg.ServidorPorta = l.porta("SERVIDOR_PORTA", ServidorPortaPadrao, cfg.ConfigEstrita)

	// --- credencial e conexão ---
	//
	// As duas têm PADRÃO, e isso é uma decisão explícita: o serviço precisa
	// subir sem nenhuma variável definida. Ver APIKeyPadrao e DatabaseURLPadrao
	// para o que cada padrão significa e quando ele NÃO serve.
	cfg.DatabaseURL = URLSegredo(l.texto("DATABASE_URL", DatabaseURLPadrao))
	cfg.APIKey = Segredo(l.texto("API_KEY", APIKeyPadrao))

	// --- observabilidade ---
	cfg.LogNivel = l.nivelDeLog("LOG_NIVEL", slog.LevelInfo)
	cfg.LogFormato = l.enumerado("LOG_FORMATO", "json", "json", "text")
	cfg.OTLPEndpoint = l.texto("OTEL_EXPORTER_OTLP_ENDPOINT", "")

	// --- chaves de recurso: padrão reproduz o legado ---
	cfg.HTTPTempoLimiteDeCabecalho = l.duracao("HTTP_TEMPO_LIMITE_CABECALHO", 10*time.Second)
	cfg.HTTPTempoLimiteDeLeitura = l.duracao("HTTP_TEMPO_LIMITE_LEITURA", 0)
	cfg.HTTPTempoLimiteDeEscrita = l.duracao("HTTP_TEMPO_LIMITE_ESCRITA", 0)
	cfg.HTTPTempoLimiteOcioso = l.duracao("HTTP_TEMPO_LIMITE_OCIOSO", 120*time.Second)
	cfg.HTTPMaxHeaderBytes = l.inteiro("HTTP_MAX_HEADER_BYTES", 1<<20)

	cfg.MaxUploadBytes = l.inteiro64("MAX_UPLOAD_BYTES", 0)
	cfg.MaxImportacoesConcorrentes = l.inteiro("MAX_IMPORTACOES_CONCORRENTES", 0)
	cfg.IndexMemoriaBytes = l.inteiro64("INDEX_MEMORIA_BYTES", IndexMemoriaBytesPadrao)
	cfg.ShutdownTimeout = l.duracao("SHUTDOWN_TIMEOUT", 0)
	cfg.IdempotenciaPorHash = l.booleano("IDEMPOTENCIA_POR_HASH", false)
	cfg.GravacaoEmLote = l.booleano("GRAVACAO_EM_LOTE", false)
	cfg.ValidarAssinaturaPDF = l.booleano("VALIDAR_ASSINATURA_PDF", false)
	cfg.VarreduraOrfas = l.booleano("VARREDURA_ORFAS", false)
	cfg.RespostaProblemJSON = l.booleano("RESPOSTA_PROBLEM_JSON", false)
	cfg.RateLimitRPS = l.inteiro("RATE_LIMIT_RPS", 0)
	cfg.StatusEndpoint = l.booleano("STATUS_ENDPOINT", false)
	cfg.HealthEndpoints = l.booleano("HEALTH_ENDPOINTS", false)

	// Ajustes da varredura. Lidos SEMPRE, para que um valor inválido seja
	// recusado no arranque em vez de na primeira vez que alguém ligar a chave.
	cfg.VarreduraOrfasIntervalo = l.duracao("VARREDURA_ORFAS_INTERVALO", VarreduraIntervaloPadrao)
	cfg.VarreduraOrfasLimiar = l.duracao("VARREDURA_ORFAS_LIMIAR", VarreduraLimiarPadrao)
	cfg.VarreduraOrfasPolitica = l.enumerado("VARREDURA_ORFAS_POLITICA",
		VarreduraPoliticaPadrao, PoliticasDeVarreduraAceitas...)
	cfg.VarreduraOrfasLote = l.inteiro("VARREDURA_ORFAS_LOTE", VarreduraLotePadrao)

	// Zero é aceito nas demais durações porque significa "sem limite". Aqui não:
	// um intervalo de zero faria o laço girar sem pausa, e um limiar de zero
	// consideraria presa toda importação em curso.
	if cfg.VarreduraOrfas {
		if cfg.VarreduraOrfasIntervalo <= 0 {
			l.anotar("VARREDURA_ORFAS_INTERVALO precisa ser positivo com VARREDURA_ORFAS ligada")
		}
		if cfg.VarreduraOrfasLimiar <= 0 {
			l.anotar("VARREDURA_ORFAS_LIMIAR precisa ser positivo com VARREDURA_ORFAS ligada")
		}
	}

	if len(l.problemas) > 0 {
		return nil, &ErroDeConfiguracao{Problemas: l.problemas}
	}
	return cfg, nil
}

// leitor acumula problemas em vez de falhar no primeiro.
type leitor struct {
	problemas []string
}

func (l *leitor) anotar(formato string, args ...any) {
	l.problemas = append(l.problemas, fmt.Sprintf(formato, args...))
}

func (l *leitor) texto(nome, padrao string) string {
	if v, ok := os.LookupEnv(nome); ok && v != "" {
		return v
	}
	return padrao
}

// porta reproduz reference/main.rs:45-48.
//
// DEFEITO PRESERVADO (achado A19): valor não numérico, fora de faixa ou vazio
// cai no padrão SEM erro. A chave CONFIG_ESTRITA transforma isso em falha —
// comportamento novo, desligado por padrão.
//
// O valor "0" é um uint16 válido e faz o sistema atribuir uma porta efêmera;
// é comportamento do legado e não é tratado como erro. Ver ESPECIFICACAO §1.1.
func (l *leitor) porta(nome string, padrao uint16, estrita bool) uint16 {
	bruto, ok := os.LookupEnv(nome)
	if !ok {
		return padrao
	}

	v, err := strconv.ParseUint(bruto, 10, 16)
	if err != nil {
		if estrita {
			l.anotar("%s=%q não é uma porta válida (0-65535)", nome, bruto)
		}
		return padrao
	}
	return uint16(v)
}

// As variáveis abaixo são NOVAS: não há comportamento de legado a preservar,
// então valor malformado é sempre erro, independentemente de CONFIG_ESTRITA.

func (l *leitor) booleano(nome string, padrao bool) bool {
	bruto, ok := os.LookupEnv(nome)
	if !ok || bruto == "" {
		return padrao
	}
	v, err := strconv.ParseBool(bruto)
	if err != nil {
		l.anotar("%s=%q não é booleano (use true/false, 1/0)", nome, bruto)
		return padrao
	}
	return v
}

func (l *leitor) inteiro64(nome string, padrao int64) int64 {
	bruto, ok := os.LookupEnv(nome)
	if !ok || bruto == "" {
		return padrao
	}
	v, err := strconv.ParseInt(bruto, 10, 64)
	if err != nil {
		l.anotar("%s=%q não é um inteiro", nome, bruto)
		return padrao
	}
	if v < 0 {
		l.anotar("%s=%d não pode ser negativo", nome, v)
		return padrao
	}
	return v
}

func (l *leitor) inteiro(nome string, padrao int) int {
	return int(l.inteiro64(nome, int64(padrao)))
}

// duracao aceita a notação do Go ("30s", "2m") e também um inteiro simples,
// interpretado como segundos — é a forma que operadores costumam escrever.
func (l *leitor) duracao(nome string, padrao time.Duration) time.Duration {
	bruto, ok := os.LookupEnv(nome)
	if !ok || bruto == "" {
		return padrao
	}
	if segundos, err := strconv.ParseInt(bruto, 10, 64); err == nil {
		if segundos < 0 {
			l.anotar("%s=%s não pode ser negativa", nome, bruto)
			return padrao
		}
		return time.Duration(segundos) * time.Second
	}
	v, err := time.ParseDuration(bruto)
	if err != nil {
		l.anotar("%s=%q não é uma duração (use 30s, 2m ou um inteiro em segundos)", nome, bruto)
		return padrao
	}
	if v < 0 {
		l.anotar("%s=%q não pode ser negativa", nome, bruto)
		return padrao
	}
	return v
}

func (l *leitor) enumerado(nome, padrao string, permitidos ...string) string {
	bruto, ok := os.LookupEnv(nome)
	if !ok || bruto == "" {
		return padrao
	}
	v := strings.ToLower(strings.TrimSpace(bruto))
	for _, p := range permitidos {
		if v == p {
			return v
		}
	}
	l.anotar("%s=%q não é válido (use %s)", nome, bruto, strings.Join(permitidos, ", "))
	return padrao
}

func (l *leitor) nivelDeLog(nome string, padrao slog.Level) slog.Level {
	bruto, ok := os.LookupEnv(nome)
	if !ok || bruto == "" {
		return padrao
	}
	switch strings.ToLower(strings.TrimSpace(bruto)) {
	case "debug":
		return slog.LevelDebug
	case "info":
		return slog.LevelInfo
	case "warn", "warning":
		return slog.LevelWarn
	case "error", "erro":
		return slog.LevelError
	default:
		l.anotar("%s=%q não é um nível (use debug, info, warn, error)", nome, bruto)
		return padrao
	}
}

// ComoErroDeConfiguracao extrai o erro tipado de uma cadeia de erros.
func ComoErroDeConfiguracao(err error) (*ErroDeConfiguracao, bool) {
	var alvo *ErroDeConfiguracao
	ok := errors.As(err, &alvo)
	return alvo, ok
}
