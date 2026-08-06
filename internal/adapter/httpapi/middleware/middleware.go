package middleware

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"log/slog"
	"net/http"
	"runtime/debug"
	"time"

	"github.com/gluizcortez/projetorust/internal/platform/observability"
)

// Middleware é um decorador da cadeia HTTP.
//
// A ordem de aplicação é normativa e está em docs/ESPECIFICACAO.md §1.2. Ver
// httpapi.NovoRouter, que é onde ela é montada.
type Middleware func(http.Handler) http.Handler

// Encadear aplica os middlewares de fora para dentro: o primeiro da lista é o
// mais externo, e portanto o primeiro a ver a requisição.
//
// Escrever a composição assim, e não aninhando chamadas, é o que permite ler a
// cadeia na mesma ordem em que ela executa.
func Encadear(manipulador http.Handler, camadas ...Middleware) http.Handler {
	for i := len(camadas) - 1; i >= 0; i-- {
		manipulador = camadas[i](manipulador)
	}
	return manipulador
}

// -------------------------------------------------------------------------
// Identificador de requisição
// -------------------------------------------------------------------------

// CabecalhoIDRequisicao é o nome do cabeçalho, em minúsculas.
//
// O legado usa `RequestId::new()` do Salvo, que define o cabeçalho de
// REQUISIÇÃO quando ausente, e `upload_pdf` o lê com
// `req.header::<String>("x-request-id").unwrap_or_default()`
// (reference/main.rs:134) — nunca falha, no pior caso é a cadeia vazia.
const CabecalhoIDRequisicao = "x-request-id"

// IDRequisicao gera ou aproveita o identificador da requisição.
//
// Duas diferenças em relação ao legado, ambas de observabilidade e nenhuma de
// contrato:
//
//   - o identificador é ECOADO no cabeçalho da RESPOSTA. O Salvo só o define na
//     requisição. Devolvê-lo é o que permite a quem chamou correlacionar sua
//     requisição com o registro do serviço, e não altera corpo nem código;
//   - vai também para o contexto, de onde o registro estruturado o lê como
//     atributo.
func IDRequisicao() Middleware {
	return func(proximo http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id := r.Header.Get(CabecalhoIDRequisicao)
			if id == "" {
				id = gerarID()
				r.Header.Set(CabecalhoIDRequisicao, id)
			}
			w.Header().Set(CabecalhoIDRequisicao, id)
			proximo.ServeHTTP(w, r.WithContext(observability.ComIDRequisicao(r.Context(), id)))
		})
	}
}

// gerarID produz 16 bytes aleatórios em hexadecimal.
//
// Não é UUID: o formato do identificador não é contrato — o legado usa o do
// Salvo e nada o interpreta —, e evitar uma dependência para gerar
// identificador opaco é o negócio melhor. `crypto/rand.Read` não falha desde o
// Go 1.24; a assinatura sem erro é a da própria biblioteca padrão.
func gerarID() string {
	var b [16]byte
	// Desde o Go 1.24 esta função não falha — a documentação diz que ela sempre
	// devolve len(b), nil. O erro é conferido assim mesmo porque suprimi-lo com
	// nolint seria uma exceção a manter, e o custo de um `if` é nenhum.
	if _, err := rand.Read(b[:]); err != nil {
		// Inalcançável. Se um dia deixar de ser, um identificador previsível é
		// melhor que um pânico: ele não é credencial, só correlaciona registros.
		return "sem-identificador"
	}
	return hex.EncodeToString(b[:])
}

// -------------------------------------------------------------------------
// Registro
// -------------------------------------------------------------------------

// respostaObservada intercepta o código e o tamanho para o registro.
//
// Implementa http.Flusher porque o encadeamento não pode remover capacidades do
// escritor original — um manipulador que precise esvaziar o buffer continua
// podendo.
type respostaObservada struct {
	http.ResponseWriter
	codigo  int
	tamanho int
}

func (r *respostaObservada) WriteHeader(codigo int) {
	r.codigo = codigo
	r.ResponseWriter.WriteHeader(codigo)
}

func (r *respostaObservada) Write(b []byte) (int, error) {
	if r.codigo == 0 {
		// Escrita sem WriteHeader explícito: o Go emite 200.
		r.codigo = http.StatusOK
	}
	n, err := r.ResponseWriter.Write(b)
	r.tamanho += n
	return n, err //nolint:wrapcheck // repassa o erro do escritor original sem alterá-lo
}

func (r *respostaObservada) Flush() {
	if f, ok := r.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Registro emite um evento por requisição, com método, rota, status, duração e
// tamanho como ATRIBUTOS estruturados.
//
// Substitui o `Logger::new()` do Salvo (reference/main.rs:60), que envolve o
// serviço inteiro e portanto também vê as respostas de rota inexistente. A
// posição na cadeia preserva isso.
func Registro(logger *slog.Logger) Middleware {
	return func(proximo http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			inicio := time.Now()
			obs := &respostaObservada{ResponseWriter: w}

			proximo.ServeHTTP(obs, r)

			logger.InfoContext(r.Context(), "requisição atendida",
				slog.String("metodo", r.Method),
				slog.String("rota", r.URL.Path),
				slog.Int("status", obs.codigo),
				slog.Duration("duracao", time.Since(inicio)),
				slog.Int("bytes", obs.tamanho),
			)
		})
	}
}

// -------------------------------------------------------------------------
// Recuperação de pânico
// -------------------------------------------------------------------------

// Recuperacao transforma pânico em 500 e mantém o processo vivo.
//
// NÃO tem equivalente no legado: um pânico dentro de um manipulador do Salvo
// derruba a tarefa da conexão e o cliente recebe a conexão fechada, sem
// resposta. Responder 500 é comportamento NOVO, e deliberado — a alternativa é
// o cliente não saber distinguir "o serviço caiu" de "a rede caiu".
//
// O corpo vai vazio de propósito: qualquer texto seria invenção, já que o
// legado não emite nenhum.
//
// Se o cabeçalho já foi enviado, não há como mudar o código; nesse caso só
// resta registrar, e a conexão fecha com a resposta truncada — que é o que o
// legado também faria.
func Recuperacao(logger *slog.Logger) Middleware {
	return func(proximo http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				p := recover()
				if p == nil {
					return
				}
				// http.ErrAbortHandler é o sinal documentado de que o
				// manipulador desistiu de propósito; repassá-lo é o contrato
				// com o servidor, que já sabe silenciá-lo.
				if p == http.ErrAbortHandler { //nolint:errorlint // comparação por identidade, como manda a documentação de net/http
					panic(p) //nolint:forbidigo // repropagação deliberada
				}

				logger.ErrorContext(r.Context(), "manipulador entrou em pânico",
					slog.String("metodo", r.Method),
					slog.String("rota", r.URL.Path),
					slog.Any("panico", p),
					slog.String("pilha", string(debug.Stack())),
				)

				if obs, ok := w.(*respostaObservada); ok && obs.codigo != 0 {
					return // cabeçalho já enviado
				}
				w.WriteHeader(http.StatusInternalServerError)
			}()

			proximo.ServeHTTP(w, r)
		})
	}
}

// -------------------------------------------------------------------------
// Limite de corpo
// -------------------------------------------------------------------------

// LimiteDeCorpo restringe o tamanho do corpo da requisição.
//
// `maxBytes` igual a zero DESLIGA o limite, que é o comportamento do legado:
// não há teto de tamanho nem validação de tipo (docs/ESPECIFICACAO.md §1.4.6).
// Qualquer valor positivo é EVOLUÇÃO, atrás de MAX_UPLOAD_BYTES.
//
// Excedido o limite, `http.MaxBytesReader` faz a leitura falhar; quem traduz
// isso em resposta é o manipulador, porque só ele sabe qual texto do contrato
// se aplica.
func LimiteDeCorpo(maxBytes int64) Middleware {
	return func(proximo http.Handler) http.Handler {
		if maxBytes <= 0 {
			return proximo
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
			proximo.ServeHTTP(w, r)
		})
	}
}

// -------------------------------------------------------------------------
// Autenticação
// -------------------------------------------------------------------------

// CabecalhoAPIKey é o nome do cabeçalho de credencial.
//
// A busca do Go em http.Header é insensível a maiúsculas, como a do
// `HeaderMap` do Rust — MEDIDO pela sonda: `x-api-key` minúsculo é aceito pelo
// legado.
const CabecalhoAPIKey = "X-API-KEY"

// Textos das duas respostas 401. São literais normativos.
const (
	// TextoChaveAusente é reference/main.rs:127.
	TextoChaveAusente = "Faltou a X-API-KEY"
	// TextoChaveInvalida é reference/main.rs:122.
	TextoChaveInvalida = "X-API-KEY inválida"
)

// Negar escreve uma resposta de recusa. Existe para que a autenticação não
// precise conhecer o pacote de resposta.
type Negar func(w http.ResponseWriter, r *http.Request, codigo int, texto string)

// Autenticacao exige o cabeçalho X-API-KEY.
//
// Porte de reference/main.rs:117-131, com UMA correção que não altera resposta
// alguma: a comparação usa `subtle.ConstantTimeCompare` em vez do `!=` do Rust,
// que encerra no primeiro byte diferente e vaza o tamanho do prefixo correto
// pelo tempo. É o achado A02.
//
// Os DOIS textos permanecem distintos. Dizer se a chave existe ou não é
// divulgação de informação, e é contrato existente — unificá-los quebraria
// clientes que hoje distinguem os casos. Registrado como risco aceito em
// docs/CONTEXT.md e decidido em D-09.
//
// MEDIDO na sonda, e por isso vale registrar: cabeçalho presente e VAZIO cai em
// "inválida", não em "ausente"; cabeçalho REPETIDO usa o primeiro valor.
func Autenticacao(chave string, negar Negar) Middleware {
	esperada := []byte(chave)
	return func(proximo http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			valores := r.Header.Values(CabecalhoAPIKey)
			if len(valores) == 0 {
				negar(w, r, http.StatusUnauthorized, TextoChaveAusente)
				return
			}

			// O primeiro valor, como `HeaderMap::get` do Rust.
			recebida := []byte(valores[0])

			// ConstantTimeCompare devolve 0 quando os tamanhos diferem, sem
			// olhar o conteúdo — o que já não vaza nada além do tamanho, que o
			// próprio cabeçalho revela.
			if subtle.ConstantTimeCompare(recebida, esperada) != 1 {
				negar(w, r, http.StatusUnauthorized, TextoChaveInvalida)
				return
			}

			proximo.ServeHTTP(w, r)
		})
	}
}
