package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	"github.com/gluizcortez/projetorust/internal/adapter/httpapi/middleware"
	"github.com/gluizcortez/projetorust/internal/adapter/httpapi/resposta"
	"github.com/gluizcortez/projetorust/internal/domain"
	"github.com/gluizcortez/projetorust/internal/usecase"
)

// Textos de resposta. São literais normativos, conferidos linha a linha contra
// reference/main.rs e confirmados pela sonda do Salvo.
const (
	// TextoPong é reference/main.rs:114.
	TextoPong = "pong"
	// TextoSucesso é reference/main.rs:340.
	TextoSucesso = "PDF carregado com sucesso"
	// TextoErroAoProcessar é reference/main.rs:242.
	TextoErroAoProcessar = "Erro ao processar o PDF"
)

// Rotas do serviço.
//
// As três últimas só existem com a chave correspondente ligada.
const (
	RotaPing = "/ping"
	RotaPDF  = "/pdf"

	// RotaRaiz não tem manipulador: no legado ela existe como rota e não tem
	// método, o que o Salvo responde com 405. Ver normalizarCaminho.
	RotaRaiz = "/"

	// RotaImportacao é o prefixo de GET /importacao/{id} (STATUS_ENDPOINT).
	RotaImportacao = "/importacao"
	// RotaHealthLive e RotaHealthReady são de HEALTH_ENDPOINTS.
	RotaHealthLive  = "/health/live"
	RotaHealthReady = "/health/ready"
)

// Ingestor é a porta do caso de uso que o manipulador de upload usa.
//
// Definida aqui, pelo consumidor, para que o pacote HTTP não dependa do tipo
// concreto de usecase.Ingestao — o que também o torna testável com dublê.
type Ingestor interface {
	Executar(ctx context.Context, cmd usecase.ComandoIngerir) (int64, error)
}

// Dependencias reúne o que o roteador precisa.
type Dependencias struct {
	Ingestor Ingestor
	Logger   *slog.Logger

	// APIKey é a credencial exigida em /pdf.
	APIKey string

	// MaxUploadBytes limita o corpo da requisição. Zero DESLIGA, que é o
	// comportamento do legado (docs/ESPECIFICACAO.md §1.4.6).
	MaxUploadBytes int64

	// RespostaProblemJSON troca o corpo das respostas por RFC 7807. Nasce
	// desligada: ligá-la quebra clientes que analisam o texto.
	RespostaProblemJSON bool

	// RateLimitRPS limita requisições por segundo, por chave de API. Zero
	// DESLIGA, que é o legado — ele não tem limitação alguma.
	RateLimitRPS int

	// StatusEndpoint registra GET /importacao/{id}. É ADIÇÃO PURA: nenhuma
	// rota existente muda. Exige Importacoes.
	StatusEndpoint bool

	// HealthEndpoints registra /health/live e /health/ready. `/ping` permanece
	// EXATAMENTE como está — é contrato existente e não verifica nada.
	HealthEndpoints bool

	// Importacoes é consultado por GET /importacao/{id}. Só é exigido quando
	// StatusEndpoint está ligado.
	Importacoes ConsultorDeImportacao

	// Pronto informa se as dependências estão alcançáveis, para /health/ready.
	// Só é exigido quando HealthEndpoints está ligado.
	Pronto func(context.Context) error
}

// NovoRouter monta a cadeia de middleware e as rotas.
//
// # A ordem da cadeia é normativa
//
//	identificador → registro → recuperação → limite de corpo → [autenticação] → manipulador
//
// Ela reproduz docs/ESPECIFICACAO.md §1.2, onde o `Logger` do Salvo envolve o
// `Service` inteiro — e portanto também vê as respostas de rota inexistente — e
// o `RequestId` fica no roteador raiz. A autenticação é só de /pdf
// (reference/main.rs:54).
//
// # O curto-circuito da autenticação é explícito aqui
//
// No Salvo, um hoop que grava um código de erro interrompe a cadeia por um
// efeito colateral de `Response::is_stamped()` — não há `skip_rest()` no
// legado. Em Go o curto-circuito é simplesmente não chamar o próximo, o que
// torna a intenção visível em vez de emergente.
func NovoRouter(d Dependencias) (http.Handler, error) {
	if d.Ingestor == nil {
		return nil, errors.New("httpapi: Ingestor é obrigatório")
	}
	if d.Logger == nil {
		return nil, errors.New("httpapi: Logger é obrigatório")
	}
	if d.APIKey == "" {
		return nil, errors.New("httpapi: APIKey é obrigatória")
	}
	if d.StatusEndpoint && d.Importacoes == nil {
		return nil, errors.New("httpapi: STATUS_ENDPOINT ligado exige Importacoes")
	}
	if d.HealthEndpoints && d.Pronto == nil {
		return nil, errors.New("httpapi: HEALTH_ENDPOINTS ligado exige Pronto")
	}

	escritor := resposta.Escolher(d.RespostaProblemJSON, d.Logger)
	s := &servico{
		ingestor:       d.Ingestor,
		logger:         d.Logger,
		escritor:       escritor,
		maxUploadBytes: d.MaxUploadBytes,
		importacoes:    d.Importacoes,
		pronto:         d.Pronto,
	}

	negar := func(w http.ResponseWriter, r *http.Request, codigo int, texto string) {
		escritor.Escrever(w, r, codigo, texto, nil)
	}

	// O roteamento é feito à mão, e não pelos padrões "GET /ping" do ServeMux,
	// por duas razões medidas:
	//
	//  1. o ServeMux devolveria 405 com corpo próprio e cabeçalho `Allow` —
	//     nada disso é o que o Salvo emite;
	//  2. o ServeMux não normaliza barras como o Salvo. Ver normalizarCaminho.
	autenticado := func(h http.HandlerFunc) http.Handler {
		return middleware.Encadear(h, middleware.Autenticacao(d.APIKey, negar))
	}

	manipuladores := map[string]http.Handler{
		RotaPing: http.HandlerFunc(s.ping),
		RotaPDF:  autenticado(s.uploadPDF),
	}
	metodos := map[string]string{
		RotaPing: http.MethodGet,
		RotaPDF:  http.MethodPost,
	}

	// EVOLUÇÃO — STATUS_ENDPOINT. Adição pura: nenhuma rota existente muda.
	if d.StatusEndpoint {
		manipuladores[RotaImportacao] = autenticado(s.consultarImportacao)
		metodos[RotaImportacao] = http.MethodGet
	}

	// EVOLUÇÃO — HEALTH_ENDPOINTS. `/ping` NÃO é tocado: continua respondendo
	// `pong` sem verificar nada, que é o contrato existente.
	if d.HealthEndpoints {
		manipuladores[RotaHealthLive] = http.HandlerFunc(s.healthLive)
		metodos[RotaHealthLive] = http.MethodGet
		manipuladores[RotaHealthReady] = http.HandlerFunc(s.healthReady)
		metodos[RotaHealthReady] = http.MethodGet
	}

	roteador := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rota := normalizarCaminho(r.URL.EscapedPath())

		// MEDIDO: a raiz responde 405, não 404 — em QUALQUER método, e em
		// qualquer forma que colapse para zero segmentos (`/`, `//`, `///`).
		//
		// No legado ela é rota de verdade: `Router::new()` casa o caminho
		// vazio e não tem `get`/`post`, então o Salvo decide "existe, sem este
		// método". Um `404` aqui seria dizer que a raiz não existe.
		if rota == RotaRaiz {
			EscreverCatcher(w, r, http.StatusMethodNotAllowed)
			return
		}

		if d.StatusEndpoint {
			rota = colapsarImportacao(rota)
		}

		manipulador, existe := manipuladores[rota]
		if !existe {
			EscreverCatcher(w, r, http.StatusNotFound)
			return
		}
		if r.Method != metodos[rota] {
			// MEDIDO: o método perde ANTES da autenticação. `GET /pdf` sem
			// chave devolve 405, não 401.
			EscreverCatcher(w, r, http.StatusMethodNotAllowed)
			return
		}
		manipulador.ServeHTTP(w, r)
	})

	camadas := []middleware.Middleware{
		middleware.IDRequisicao(),
		middleware.Registro(d.Logger),
		middleware.Recuperacao(d.Logger),
		middleware.LimiteDeCorpo(d.MaxUploadBytes),
	}
	// EVOLUÇÃO — RATE_LIMIT_RPS. Fica DEPOIS da recuperação, para que um
	// pânico no limitador não derrube o processo, e ANTES do roteamento, para
	// que a recusa não gaste trabalho algum.
	if d.RateLimitRPS > 0 {
		camadas = append(camadas, middleware.LimitarTaxa(d.RateLimitRPS, negar))
	}

	return middleware.Encadear(roteador, camadas...), nil
}

// colapsarImportacao reduz `/importacao/42` a `/importacao`, para que o
// roteamento por mapa alcance a rota com identificador no caminho.
//
// O identificador em si é lido do caminho pelo manipulador.
func colapsarImportacao(rota string) string {
	if rota == RotaImportacao || strings.HasPrefix(rota, RotaImportacao+"/") {
		return RotaImportacao
	}
	return rota
}

// normalizarCaminho reproduz como o roteador do Salvo compara caminhos.
//
// Recebe o caminho na forma CRUA — `r.URL.EscapedPath()`, não `r.URL.Path` —
// e é essencial que assim seja. Ver "a ordem importa", abaixo.
//
// MEDIDO por `tools/sonda-http --bin sonda-caminho` (Salvo 0.95.2, servidor de
// verdade, linha de requisição escrita byte a byte). A regra é:
//
//	parta em `/`, descarte os segmentos VAZIOS, decodifique cada segmento
//	que sobrou, e junte de volta com `/`.
//
// Daí saem todas as observações:
//
//	/ping   /ping/   /ping//   //ping   /ping///     200 — vazios somem
//	/pi%6Eg   /%70ing   /%70%69%6E%67                200 — o segmento decodifica
//	/ping/x                                          404 — dois segmentos
//	/./ping   /.   /ping/..   /x/../ping             404 — `.` e `..` são segmentos
//	/ping%20   /ping+   /ping%09   /ping%00          404 — não vira `ping`
//	/ping%2F   /%2Fping                              404 — ver abaixo
//	/   //   ///                                     405 — ver abaixo
//
// # A ordem importa: decodificar DEPOIS de partir
//
// Em Go, `r.URL.Path` já vem decodificado, então `%2F` VIRA barra antes de
// qualquer fatiamento e `/ping%2F` colapsa para `/ping` — 200 onde o legado
// responde 404. É por isso que a entrada aqui é a forma crua: o Salvo parte
// primeiro e decodifica depois, e uma barra que nasceu de `%2F` nunca vira
// separador.
//
// O efeito colateral é de segurança, e é o lado bom: nenhuma forma codificada
// alcança uma rota que a forma literal não alcançaria.
//
// # `.` e `..` NÃO são resolvidos
//
// Não por omissão — por medição. `/./ping` responde 404 no legado. A versão
// anterior desta função descartava `.` junto com os vazios e respondia 200,
// afirmando no comentário que isso havia sido medido. Não havia: a sonda da F9
// só mediu `/ping/`. Agora foi.
//
// # Percentual inválido
//
// `/ping%zz` não decodifica; o segmento fica como está e não casa — 404 no
// legado. Em Go a requisição nem chega aqui: o servidor a recusa com 400 na
// análise da URL. É divergência conhecida e sem conserto barato — D-24.
func normalizarCaminho(cru string) string {
	var segmentos []string
	for _, s := range strings.Split(cru, "/") {
		if s == "" {
			continue
		}
		// Erro de decodificação mantém o segmento cru, que é o que o legado
		// faz na prática: sem decodificar, não casa com rota nenhuma.
		if decodificado, err := url.PathUnescape(s); err == nil {
			s = decodificado
		}
		segmentos = append(segmentos, s)
	}
	return "/" + strings.Join(segmentos, "/")
}

// servico agrupa o estado dos manipuladores.
type servico struct {
	ingestor       Ingestor
	logger         *slog.Logger
	escritor       resposta.Escritor
	maxUploadBytes int64

	// As duas abaixo só são preenchidas com STATUS_ENDPOINT e HEALTH_ENDPOINTS
	// ligadas, respectivamente. Nulas, os manipuladores que as usam sequer estão
	// registrados — NovoRouter recusa a montagem incoerente.
	importacoes ConsultorDeImportacao
	pronto      func(context.Context) error
}

// ping reproduz reference/main.rs:112-115. Sem autenticação, sem consultar o
// banco: responde `pong` com o PostgreSQL indisponível.
func (s *servico) ping(w http.ResponseWriter, r *http.Request) {
	s.escritor.Escrever(w, r, http.StatusOK, TextoPong, nil)
}

// uploadPDF reproduz reference/main.rs:132-253 e 339-340.
//
// A resposta de sucesso sai ANTES do processamento, que corre em segundo plano
// (docs/ESPECIFICACAO.md §1.4.3): o cliente não tem como saber o desfecho,
// porque não existe endpoint de consulta de status.
func (s *servico) uploadPDF(w http.ResponseWriter, r *http.Request) {
	lida, err := lerSubmissao(r, s.maxUploadBytes)
	if err != nil {
		// EVOLUÇÃO — MAX_UPLOAD_BYTES. Corpo acima do teto é o único caso de
		// falha de leitura com resposta PRÓPRIA: 400 com a crítica nova, que é a
		// opção B de docs/DECISOES-ABERTAS.md, D-16. Com a chave no padrão (zero)
		// este ramo é INALCANÇÁVEL — não há limite a estourar.
		//
		// A crítica vai SOZINHA, e não ao final das cinco do legado, porque a
		// leitura aborta antes de os campos de texto chegarem: não há como saber
		// se a data do caderno também faltava.
		if errors.Is(err, ErrCorpoAcimaDoLimite) {
			s.logger.WarnContext(r.Context(), "corpo acima do limite configurado",
				slog.Int64("max_upload_bytes", s.maxUploadBytes), slog.Any("erro", err))
			var criticas domain.Criticas
			criticas.Adicionar(domain.CriticaPDFAcimaDoLimite)
			s.escritor.Escrever(w, r, http.StatusBadRequest,
				criticas.Mensagem(), criticas.Itens())
			return
		}

		// O legado faz `unwrap()` na leitura do arquivo (reference/main.rs:233)
		// e ENTRA EM PÂNICO — o cliente recebe a conexão fechada. Responder 422
		// com o texto do contrato é a correção do achado A05: mesmo texto que a
		// outra falha de processamento, nenhum pânico.
		s.logger.ErrorContext(r.Context(), "falha ao ler o corpo da requisição",
			slog.Any("erro", err))
		s.escritor.Escrever(w, r, http.StatusUnprocessableEntity, TextoErroAoProcessar, nil)
		return
	}

	_, err = s.ingestor.Executar(r.Context(), usecase.ComandoIngerir{
		Submissao: lida.submissao,
		Conteudo:  lida.conteudo,
	})

	switch {
	case err == nil:
		s.escritor.Escrever(w, r, http.StatusOK, TextoSucesso, nil)

	case errors.Is(err, domain.ErrValidacao):
		// main.rs:218-224 — as críticas unidas por vírgula SEM espaço.
		var validacao *domain.ErroDeValidacao
		if !errors.As(err, &validacao) {
			// Inalcançável: quem devolve ErrValidacao é o caso de uso, sempre
			// embrulhado em ErroDeValidacao. A guarda evita que uma mudança
			// futura produza uma resposta 400 com corpo vazio.
			s.logger.ErrorContext(r.Context(),
				"erro de validação sem críticas; respondendo 422", slog.Any("erro", err))
			s.escritor.Escrever(w, r, http.StatusUnprocessableEntity, TextoErroAoProcessar, nil)
			return
		}
		s.escritor.Escrever(w, r, http.StatusBadRequest,
			validacao.Criticas.Mensagem(), validacao.Criticas.Itens())

	default:
		// main.rs:239-244 — qualquer falha de registro vira 422 com o mesmo
		// texto. O legado não distingue causas aqui.
		s.logger.ErrorContext(r.Context(), "falha ao processar a submissão", slog.Any("erro", err))
		s.escritor.Escrever(w, r, http.StatusUnprocessableEntity, TextoErroAoProcessar, nil)
	}
}
