package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
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
const (
	RotaPing = "/ping"
	RotaPDF  = "/pdf"
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

	escritor := resposta.Escolher(d.RespostaProblemJSON, d.Logger)
	s := &servico{
		ingestor:       d.Ingestor,
		logger:         d.Logger,
		escritor:       escritor,
		maxUploadBytes: d.MaxUploadBytes,
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
	manipuladores := map[string]http.Handler{
		RotaPing: http.HandlerFunc(s.ping),
		RotaPDF: middleware.Encadear(
			http.HandlerFunc(s.uploadPDF),
			middleware.Autenticacao(d.APIKey, negar),
		),
	}

	roteador := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rota := normalizarCaminho(r.URL.Path)

		manipulador, existe := manipuladores[rota]
		if !existe {
			EscreverCatcher(w, r, http.StatusNotFound)
			return
		}
		if r.Method != metodosPorRota[rota] {
			// MEDIDO: o método perde ANTES da autenticação. `GET /pdf` sem
			// chave devolve 405, não 401.
			EscreverCatcher(w, r, http.StatusMethodNotAllowed)
			return
		}
		manipulador.ServeHTTP(w, r)
	})

	return middleware.Encadear(roteador,
		middleware.IDRequisicao(),
		middleware.Registro(d.Logger),
		middleware.Recuperacao(d.Logger),
		middleware.LimiteDeCorpo(d.MaxUploadBytes),
	), nil
}

// metodosPorRota é o que cada rota aceita (reference/main.rs:52-56).
var metodosPorRota = map[string]string{
	RotaPing: http.MethodGet,
	RotaPDF:  http.MethodPost,
}

// normalizarCaminho reproduz como o roteador do Salvo compara caminhos.
//
// MEDIDO por `tools/sonda-http`: o caminho é partido em segmentos, e segmentos
// VAZIOS e `.` são ignorados. Todos estes chegam ao mesmo manipulador:
//
//	/ping   /ping/   /ping//   //ping   /ping///   /./ping
//
// e `/ping/x` NÃO chega — dois segmentos não casam com um.
//
// O `ServeMux` do Go não faz isso: com o padrão "/ping", uma requisição a
// "/ping/" devolve 404. Sem esta normalização, seis formas que hoje respondem
// `pong` passariam a responder 404.
//
// `..` NÃO é resolvido, de propósito: a sonda não mediu esse caso, e resolver
// travessia de caminho seria inventar comportamento — inclusive de segurança —
// que o legado pode não ter. Um `..` vira segmento literal e o caminho não casa.
func normalizarCaminho(caminho string) string {
	var segmentos []string
	for _, s := range strings.Split(caminho, "/") {
		if s == "" || s == "." {
			continue
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
