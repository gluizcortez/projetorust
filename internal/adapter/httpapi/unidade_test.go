package httpapi_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gluizcortez/projetorust/internal/adapter/httpapi"
	"github.com/gluizcortez/projetorust/internal/adapter/httpapi/middleware"
	"github.com/gluizcortez/projetorust/internal/adapter/httpapi/resposta"
	"github.com/gluizcortez/projetorust/internal/domain"
	"github.com/gluizcortez/projetorust/internal/domain/domaintest"
)

// TestServidorTemTemposLimiteExplicitos fixa a escolha documentada em
// servidor.go: leitura e escrita nascem SEM limite, porque qualquer valor
// finito cortaria o envio de um diário grande por enlace lento.
func TestServidorTemTemposLimiteExplicitos(t *testing.T) {
	op := httpapi.PadroesDoServidor("127.0.0.1:0")
	s := httpapi.NovoServidor(http.NotFoundHandler(), op)

	if s.Addr != "127.0.0.1:0" {
		t.Errorf("Addr = %q", s.Addr)
	}
	if s.ReadHeaderTimeout != httpapi.TempoLimiteDeCabecalhoPadrao {
		t.Errorf("ReadHeaderTimeout = %v; esperava %v",
			s.ReadHeaderTimeout, httpapi.TempoLimiteDeCabecalhoPadrao)
	}
	if s.ReadTimeout != 0 {
		t.Errorf("ReadTimeout = %v; deve nascer SEM limite (documento grande, enlace lento)",
			s.ReadTimeout)
	}
	if s.WriteTimeout != 0 {
		t.Errorf("WriteTimeout = %v; deve nascer SEM limite", s.WriteTimeout)
	}
	if s.IdleTimeout != httpapi.TempoLimiteOciosoPadrao {
		t.Errorf("IdleTimeout = %v", s.IdleTimeout)
	}
	if s.MaxHeaderBytes != httpapi.MaxHeaderBytesPadrao {
		t.Errorf("MaxHeaderBytes = %d", s.MaxHeaderBytes)
	}
	if s.Handler == nil {
		t.Error("Handler não foi montado")
	}
}

// TestServidorAceitaTemposLimiteProprios: quem souber o tamanho máximo real dos
// documentos (D-04) pode fechar os dois zeros.
func TestServidorAceitaTemposLimiteProprios(t *testing.T) {
	s := httpapi.NovoServidor(http.NotFoundHandler(), httpapi.OpcoesDoServidor{
		Endereco:               ":8080",
		TempoLimiteDeCabecalho: time.Second,
		TempoLimiteDeLeitura:   2 * time.Second,
		TempoLimiteDeEscrita:   3 * time.Second,
		TempoLimiteOcioso:      4 * time.Second,
		MaxHeaderBytes:         4096,
	})

	if s.ReadTimeout != 2*time.Second || s.WriteTimeout != 3*time.Second {
		t.Errorf("tempos limite não foram repassados: %+v", s)
	}
}

// TestEscritorDeTextoNaoReformata: o texto vai para o corpo exatamente como
// chega, sem ponto final acrescentado e sem quebra de linha.
func TestEscritorDeTextoNaoReformata(t *testing.T) {
	for _, texto := range []string{
		"pong",
		"X-API-KEY inválida",
		"Data do caderno não informada,PDF não enviado",
		"",
	} {
		w := httptest.NewRecorder()
		resposta.Texto{}.Escrever(w, nil, http.StatusOK, texto, nil)

		if w.Body.String() != texto {
			t.Errorf("corpo = %q; esperava %q", w.Body.String(), texto)
		}
		if obtido := w.Header().Get("Content-Type"); obtido != resposta.TipoTexto {
			t.Errorf("Content-Type = %q", obtido)
		}
	}
}

// TestEscolherEscritor cobre a seleção única, feita na montagem.
func TestEscolherEscritor(t *testing.T) {
	if _, ok := resposta.Escolher(false, nil).(resposta.Texto); !ok {
		t.Error("o padrão deveria ser o texto do legado")
	}
	if _, ok := resposta.Escolher(true, loggerMudo()).(resposta.ProblemJSON); !ok {
		t.Error("com a chave ligada deveria ser problem+json")
	}
}

// TestProblemJSONSoTrocaOsErros.
//
// A chave promete trocar o corpo das respostas de ERRO, e é só isso que ela
// pode fazer: um 200 não é um problema, e a RFC 7807 descreve documento de
// problema. `/ping` respondendo `{"title":"OK"}` seria mudança de contrato
// existente sob uma chave que não a anuncia.
//
// Esta era a asserção INVERTIDA até a fase F11 — o teste exigia problem+json
// também no sucesso, e o código obedecia. O defeito apareceu no teste de
// montagem com todas as chaves ligadas, que é a única configuração em que
// RESPOSTA_PROBLEM_JSON se cruza com uma verificação do corpo de /ping.
func TestProblemJSONSoTrocaOsErros(t *testing.T) {
	casos := []struct {
		nome       string
		requisicao func() *http.Request
		codigo     int
		ehProblema bool
		corpo      string
	}{
		{"ping", func() *http.Request {
			return httptest.NewRequest(http.MethodGet, httpapi.RotaPing, nil)
		}, http.StatusOK, false, httpapi.TextoPong},
		{"sucesso", func() *http.Request {
			return requisicaoPDF(submissaoCompleta(), texto(chaveDeTeste))
		}, http.StatusOK, false, httpapi.TextoSucesso},
		{"sem chave", func() *http.Request {
			return requisicaoPDF(submissaoCompleta(), nil)
		}, http.StatusUnauthorized, true, ""},
		{"validação", func() *http.Request {
			return requisicaoPDF(corpoMultipart(), texto(chaveDeTeste))
		}, http.StatusBadRequest, true, ""},
	}

	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			p := montarPilha(t, func(d *httpapi.Dependencias, _ *domaintest.RepositorioImportacaoFalso) {
				d.RespostaProblemJSON = true
			})
			w := httptest.NewRecorder()
			p.roteador.ServeHTTP(w, c.requisicao())

			if w.Code != c.codigo {
				t.Errorf("status = %d; esperava %d", w.Code, c.codigo)
			}

			tipo := w.Header().Get("Content-Type")
			if c.ehProblema {
				if tipo != resposta.TipoProblemJSON {
					t.Errorf("Content-Type = %q; esperava %q", tipo, resposta.TipoProblemJSON)
				}
				if !strings.Contains(w.Body.String(), `"status":`) {
					t.Errorf("corpo não parece problem+json: %q", w.Body.String())
				}
				return
			}

			if tipo != resposta.TipoTexto {
				t.Errorf("Content-Type = %q; sucesso continua em texto", tipo)
			}
			if w.Body.String() != c.corpo {
				t.Errorf("corpo = %q; esperava o literal do legado %q", w.Body.String(), c.corpo)
			}
		})
	}
}

// TestEhProblemaCortaEm400 fixa a fronteira em um só lugar.
func TestEhProblemaCortaEm400(t *testing.T) {
	casos := map[int]bool{
		http.StatusOK:                  false,
		http.StatusNoContent:           false,
		http.StatusMovedPermanently:    false,
		http.StatusBadRequest:          true,
		http.StatusUnauthorized:        true,
		http.StatusNotFound:            true,
		http.StatusUnprocessableEntity: true,
		http.StatusTooManyRequests:     true,
		http.StatusInternalServerError: true,
		http.StatusServiceUnavailable:  true,
	}
	for codigo, esperado := range casos {
		if obtido := resposta.EhProblema(codigo); obtido != esperado {
			t.Errorf("EhProblema(%d) = %t; esperava %t", codigo, obtido, esperado)
		}
	}
}

// TestFalhaAoRegistrarComOutroErro cobre o ramo `default` do manipulador: o
// legado responde 422 com o mesmo texto para QUALQUER falha de registro.
func TestFalhaAoRegistrarComOutroErro(t *testing.T) {
	p := montarPilha(t, func(_ *httpapi.Dependencias, r *domaintest.RepositorioImportacaoFalso) {
		r.ErroRegistrar = errors.New("qualquer outra coisa")
	})

	w := httptest.NewRecorder()
	p.roteador.ServeHTTP(w, requisicaoPDF(submissaoCompleta(), texto(chaveDeTeste)))

	if w.Code != http.StatusUnprocessableEntity {
		t.Errorf("status = %d; esperava 422", w.Code)
	}
	if w.Body.String() != httpapi.TextoErroAoProcessar {
		t.Errorf("corpo = %q", w.Body.String())
	}
}

// TestParteDesconhecidaEIgnorada: campos que o legado não lê não afetam nada.
func TestParteDesconhecidaEIgnorada(t *testing.T) {
	corpo := corpoMultipart(
		parteMultipart{nome: "campo-inventado", valor: "seja o que for"},
		parteMultipart{nome: "data-caderno", valor: "2024-03-15"},
		parteMultipart{nome: "data-disponibilizacao", valor: "2024-03-16"},
		parteMultipart{nome: "id-usuario", valor: "7"},
		parteMultipart{nome: "id-caderno", valor: "9"},
		parteMultipart{nome: "outro", disposicao: `form-data; name="outro"; filename="x.txt"`, valor: "arquivo alheio"},
		parteMultipart{nome: "pdf", disposicao: `form-data; name="pdf"; filename="d.pdf"`, valor: "%PDF"},
	)

	w := httptest.NewRecorder()
	roteador(t).ServeHTTP(w, requisicaoPDF(corpo, texto(chaveDeTeste)))

	if w.Code != http.StatusOK {
		t.Errorf("status = %d: %q", w.Code, w.Body.String())
	}
}

// TestArquivoRepetidoUsaOPrimeiro.
func TestArquivoRepetidoUsaOPrimeiro(t *testing.T) {
	corpo := corpoMultipart(
		parteMultipart{nome: "data-caderno", valor: "2024-03-15"},
		parteMultipart{nome: "data-disponibilizacao", valor: "2024-03-16"},
		parteMultipart{nome: "id-usuario", valor: "7"},
		parteMultipart{nome: "id-caderno", valor: "9"},
		parteMultipart{nome: "pdf", disposicao: `form-data; name="pdf"; filename="primeiro.pdf"`, valor: "%PDF um"},
		parteMultipart{nome: "pdf", disposicao: `form-data; name="pdf"; filename="segundo.pdf"`, valor: "%PDF dois"},
	)

	p := montarPilha(t, nil)
	w := httptest.NewRecorder()
	p.roteador.ServeHTTP(w, requisicaoPDF(corpo, texto(chaveDeTeste)))

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d: %q", w.Code, w.Body.String())
	}
	if obtido := p.importacoes.Registradas[0].ArquivoPDF; obtido != "primeiro.pdf" {
		t.Errorf("arquivo = %q; esperava o primeiro", obtido)
	}
	if obtido := p.importacoes.Registradas[0].HashSHA256; obtido != hashDe("%PDF um") {
		t.Errorf("o conteúdo gravado não é o do primeiro arquivo")
	}
}

// TestCatcherComCodigoInesperado cobre a guarda de EscreverCatcher.
func TestCatcherComCodigoInesperado(t *testing.T) {
	w := httptest.NewRecorder()
	httpapi.EscreverCatcher(w, httptest.NewRequest(http.MethodGet, "/x", nil), http.StatusTeapot)

	if w.Code != http.StatusTeapot {
		t.Errorf("status = %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "418") {
		t.Errorf("o corpo deveria mencionar o código: %q", w.Body.String())
	}
}

// TestAcceptMalformadoCaiParaHTML: um cabeçalho que não analisa não pode
// derrubar a resposta.
func TestAcceptMalformadoCaiParaHTML(t *testing.T) {
	for _, aceita := range []string{";;;", "///", "text/", "application/json;;="} {
		r := httptest.NewRequest(http.MethodGet, "/naoexiste", nil)
		r.Header.Set("Accept", aceita)
		w := httptest.NewRecorder()
		roteador(t).ServeHTTP(w, r)

		if w.Code != http.StatusNotFound {
			t.Errorf("Accept %q: status = %d", aceita, w.Code)
		}
		if obtido := w.Header().Get("Content-Type"); obtido != "text/html" {
			t.Errorf("Accept %q: Content-Type = %q; esperava a queda para HTML", aceita, obtido)
		}
	}
}

// TestRespostaObservadaRepassaFlush: o encadeamento não pode remover
// capacidades do escritor original.
func TestRespostaObservadaRepassaFlush(t *testing.T) {
	esvaziou := false
	manipulador := middleware.Registro(loggerMudo())(
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			f, ok := w.(http.Flusher)
			if !ok {
				t.Error("o escritor envolvido perdeu http.Flusher")
				return
			}
			_, _ = w.Write([]byte("parcial"))
			f.Flush()
			esvaziou = true
		}),
	)

	w := httptest.NewRecorder()
	manipulador.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/x", nil))

	if !esvaziou {
		t.Error("Flush não foi repassado")
	}
	if w.Body.String() != "parcial" {
		t.Errorf("corpo = %q", w.Body.String())
	}
	if !w.Flushed {
		t.Error("o gravador de teste não registrou o esvaziamento")
	}
}

// TestPanicoDepoisDoCabecalhoNaoTentaReescrever: com o cabeçalho já enviado não
// há como mudar o código; só resta registrar.
func TestPanicoDepoisDoCabecalhoNaoTentaReescrever(t *testing.T) {
	manipulador := middleware.Encadear(
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("parte da resposta"))
			panic("tarde demais")
		}),
		middleware.Registro(loggerMudo()),
		middleware.Recuperacao(loggerMudo()),
	)

	w := httptest.NewRecorder()
	manipulador.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/x", nil))

	if w.Code != http.StatusOK {
		t.Errorf("status = %d; o cabeçalho já tinha sido enviado como 200", w.Code)
	}
	if w.Body.String() != "parte da resposta" {
		t.Errorf("corpo = %q", w.Body.String())
	}
}

// TestErrAbortHandlerERepropagado: é o sinal documentado de desistência
// deliberada, e net/http já sabe silenciá-lo.
func TestErrAbortHandlerERepropagado(t *testing.T) {
	manipulador := middleware.Recuperacao(loggerMudo())(
		http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
			panic(http.ErrAbortHandler)
		}),
	)

	defer func() {
		p := recover()
		if p == nil {
			t.Fatal("ErrAbortHandler foi engolido; net/http precisa vê-lo")
		}
		if !errors.Is(p.(error), http.ErrAbortHandler) { //nolint:errcheck,forcetypeassert // o teste falha antes se não for erro
			t.Errorf("pânico repropagado = %v", p)
		}
	}()

	manipulador.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/x", nil))
}

// TestValidacaoRejeitaAntesDeQualquerEfeito: havendo crítica, nada é registrado.
func TestValidacaoRejeitaAntesDeQualquerEfeito(t *testing.T) {
	p := montarPilha(t, nil)
	w := httptest.NewRecorder()
	p.roteador.ServeHTTP(w, requisicaoPDF(corpoMultipart(), texto(chaveDeTeste)))

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d", w.Code)
	}
	if len(p.importacoes.Registradas) != 0 {
		t.Error("a validação deveria ter rejeitado antes de tocar no repositório")
	}
	if len(p.processadas) != 0 {
		t.Error("nenhum processamento deveria ter sido agendado")
	}
}

// TestSucessoAgendaOProcessamento é o outro lado: aceito, o trabalho vai para o
// executor e a resposta sai antes.
func TestSucessoAgendaOProcessamento(t *testing.T) {
	p := montarPilha(t, nil)
	w := httptest.NewRecorder()
	p.roteador.ServeHTTP(w, requisicaoPDF(submissaoCompleta(), texto(chaveDeTeste)))

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d: %q", w.Code, w.Body.String())
	}
	if len(p.processadas) != 1 || p.processadas[0] != 42 {
		t.Errorf("processadas = %v; esperava a importação 42", p.processadas)
	}
	if ultimo := p.importacoes.StatusGravados(); len(ultimo) != 1 ||
		ultimo[0] != domain.StatusRecebido {
		t.Errorf("status gravados = %v; esperava a escrita redundante de `recebido`", ultimo)
	}
}
