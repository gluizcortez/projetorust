package httpapi_test

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gluizcortez/projetorust/internal/adapter/httpapi"
	"github.com/gluizcortez/projetorust/internal/adapter/httpapi/resposta"
	"github.com/gluizcortez/projetorust/internal/domain"
	"github.com/gluizcortez/projetorust/internal/domain/domaintest"
	"github.com/gluizcortez/projetorust/internal/usecase"
)

const chaveDeTeste = "01956cb2-2f85-7440-9767-1a6651c10e0f"

func loggerMudo() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// executorSincrono roda a tarefa na hora, para que o teste observe o efeito sem
// esperar por goroutine.
type executorSincrono struct {
	Processadas []int64
}

func (e *executorSincrono) Submeter(
	_ context.Context, ctxDaTarefa context.Context, tarefa func(context.Context),
) error {
	tarefa(ctxDaTarefa)
	return nil
}

// pilha monta o roteador sobre o caso de uso REAL, com dublês só nas portas de
// infraestrutura.
//
// É deliberado não usar um dublê de Ingestor: a validação — e portanto a ordem
// e os literais das críticas, que são o contrato — vive no caso de uso. Um
// dublê no lugar dele testaria a fiação e não o contrato.
type pilha struct {
	roteador    http.Handler
	importacoes *domaintest.RepositorioImportacaoFalso
	executor    *executorSincrono
	processadas []int64
}

func montarPilha(t *testing.T, ajustar func(*httpapi.Dependencias, *domaintest.RepositorioImportacaoFalso)) *pilha {
	t.Helper()

	p := &pilha{
		importacoes: &domaintest.RepositorioImportacaoFalso{
			Diario:   &domaintest.Diario{},
			IDGerado: 42,
		},
		executor: &executorSincrono{},
	}

	ingestao, err := usecase.NovaIngestao(usecase.DependenciasDaIngestao{
		Importacoes: p.importacoes,
		Executor:    p.executor,
		Logger:      loggerMudo(),
		Processar: func(_ context.Context, id int64, _ []byte) {
			p.processadas = append(p.processadas, id)
		},
	})
	if err != nil {
		t.Fatalf("NovaIngestao: %v", err)
	}

	deps := httpapi.Dependencias{
		Ingestor: ingestao,
		Logger:   loggerMudo(),
		APIKey:   chaveDeTeste,
	}
	if ajustar != nil {
		ajustar(&deps, p.importacoes)
	}

	h, err := httpapi.NovoRouter(deps)
	if err != nil {
		t.Fatalf("NovoRouter: %v", err)
	}
	p.roteador = h
	return p
}

func roteador(t *testing.T) http.Handler {
	t.Helper()
	return montarPilha(t, nil).roteador
}

// -------------------------------------------------------------------------
// Construção de corpos multipart
// -------------------------------------------------------------------------

const fronteira = "----testeF9"

type parteMultipart struct {
	nome string
	// disposicao, quando não vazia, substitui a Content-Disposition inteira —
	// é como se testam os casos de `filename` ausente, vazio e com espaço.
	disposicao string
	valor      string
}

func corpoMultipart(partes ...parteMultipart) string {
	var b strings.Builder
	for _, p := range partes {
		disposicao := p.disposicao
		if disposicao == "" {
			disposicao = `form-data; name="` + p.nome + `"`
		}
		b.WriteString("--" + fronteira + "\r\n")
		b.WriteString("Content-Disposition: " + disposicao + "\r\n\r\n")
		b.WriteString(p.valor + "\r\n")
	}
	b.WriteString("--" + fronteira + "--\r\n")
	return b.String()
}

// submissaoCompleta monta um corpo com os quatro campos válidos e o arquivo.
func submissaoCompleta() string {
	return corpoMultipart(
		parteMultipart{nome: "data-caderno", valor: "2024-03-15"},
		parteMultipart{nome: "data-disponibilizacao", valor: "2024-03-16"},
		parteMultipart{nome: "id-usuario", valor: "7"},
		parteMultipart{nome: "id-caderno", valor: "9"},
		parteMultipart{
			nome:       "pdf",
			disposicao: `form-data; name="pdf"; filename="diario.pdf"`,
			valor:      "%PDF-1.4 conteudo",
		},
	)
}

func requisicaoPDF(corpo string, chave *string) *http.Request {
	r := httptest.NewRequest(http.MethodPost, httpapi.RotaPDF, strings.NewReader(corpo))
	r.Header.Set("Content-Type", "multipart/form-data; boundary="+fronteira)
	if chave != nil {
		r.Header.Set("X-API-KEY", *chave)
	}
	return r
}

func texto(s string) *string { return &s }

// -------------------------------------------------------------------------
// A tabela de contrato
// -------------------------------------------------------------------------

// TestContratoDeRespostas é o critério de aceite central da fase F9: as sete
// respostas possíveis, com corpo comparado BYTE A BYTE e Content-Type conferido.
//
// Cada linha cita a origem em reference/main.rs e, quando o valor foi medido em
// vez de deduzido, a sonda `tools/sonda-http` que o mediu.
func TestContratoDeRespostas(t *testing.T) {
	const criticasCompletas = "Data do caderno não informada," +
		"Data de disponibilização não informada," +
		"Id do usuário não informado," +
		"Id do caderno não informado," +
		"PDF não enviado"

	casos := []struct {
		nome        string
		origem      string
		requisicao  func() *http.Request
		ajustar     func(*httpapi.Dependencias, *domaintest.RepositorioImportacaoFalso)
		codigo      int
		contentType string
		corpo       string
	}{
		{
			nome:        "GET /ping",
			origem:      "main.rs:112-115",
			requisicao:  func() *http.Request { return httptest.NewRequest(http.MethodGet, httpapi.RotaPing, nil) },
			codigo:      http.StatusOK,
			contentType: resposta.TipoTexto,
			corpo:       "pong",
		},
		{
			nome:        "R1 — sem X-API-KEY",
			origem:      "main.rs:125-128",
			requisicao:  func() *http.Request { return requisicaoPDF(submissaoCompleta(), nil) },
			codigo:      http.StatusUnauthorized,
			contentType: resposta.TipoTexto,
			corpo:       "Faltou a X-API-KEY",
		},
		{
			nome:        "R2 — X-API-KEY diferente",
			origem:      "main.rs:120-123",
			requisicao:  func() *http.Request { return requisicaoPDF(submissaoCompleta(), texto("errada")) },
			codigo:      http.StatusUnauthorized,
			contentType: resposta.TipoTexto,
			corpo:       "X-API-KEY inválida",
		},
		{
			nome:       "R3 — os cinco campos ausentes",
			origem:     "main.rs:218-224",
			requisicao: func() *http.Request { return requisicaoPDF(corpoMultipart(), texto(chaveDeTeste)) },
			codigo:     http.StatusBadRequest, contentType: resposta.TipoTexto,
			corpo: criticasCompletas,
		},
		{
			nome:       "R4 — falha ao registrar",
			origem:     "main.rs:239-244",
			requisicao: func() *http.Request { return requisicaoPDF(submissaoCompleta(), texto(chaveDeTeste)) },
			ajustar: func(_ *httpapi.Dependencias, r *domaintest.RepositorioImportacaoFalso) {
				r.ErroRegistrar = domain.ErrPersistencia
			},
			codigo: http.StatusUnprocessableEntity, contentType: resposta.TipoTexto,
			corpo: "Erro ao processar o PDF",
		},
		{
			nome:       "R5 — sucesso",
			origem:     "main.rs:339-340",
			requisicao: func() *http.Request { return requisicaoPDF(submissaoCompleta(), texto(chaveDeTeste)) },
			codigo:     http.StatusOK, contentType: resposta.TipoTexto,
			corpo: "PDF carregado com sucesso",
		},
		{
			nome:       "R6 — rota inexistente (D-08, medido)",
			origem:     "catcher do Salvo",
			requisicao: func() *http.Request { return httptest.NewRequest(http.MethodGet, "/naoexiste", nil) },
			codigo:     http.StatusNotFound, contentType: "text/html",
			corpo: catcher404HTML,
		},
		{
			nome:       "R7 — método não permitido (D-08, medido)",
			origem:     "catcher do Salvo",
			requisicao: func() *http.Request { return httptest.NewRequest(http.MethodPost, httpapi.RotaPing, nil) },
			codigo:     http.StatusMethodNotAllowed, contentType: "text/html",
			corpo: catcher405HTML,
		},
	}

	t.Log("| resposta | origem | status | Content-Type |")
	t.Log("|---|---|---|---|")
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			w := httptest.NewRecorder()
			montarPilha(t, c.ajustar).roteador.ServeHTTP(w, c.requisicao())
			res := w.Result()
			defer func() { _ = res.Body.Close() }()

			if res.StatusCode != c.codigo {
				t.Errorf("status = %d; esperava %d", res.StatusCode, c.codigo)
			}
			if obtido := res.Header.Get("Content-Type"); obtido != c.contentType {
				t.Errorf("Content-Type = %q; esperava %q", obtido, c.contentType)
			}
			corpo, err := io.ReadAll(res.Body)
			if err != nil {
				t.Fatalf("lendo o corpo: %v", err)
			}
			if string(corpo) != c.corpo {
				t.Errorf("corpo diverge byte a byte\n  obtido   (%d bytes) = %q\n  esperado (%d bytes) = %q",
					len(corpo), string(corpo), len(c.corpo), c.corpo)
			}
		})
		t.Logf("| %s | %s | %d | %s |", c.nome, c.origem, c.codigo, c.contentType)
	}
}

// TestCriticasNaOrdemContratual cobre a ordem e os literais de cada posição.
func TestCriticasNaOrdemContratual(t *testing.T) {
	casos := []struct {
		nome   string
		partes []parteMultipart
		corpo  string
	}{
		{
			nome:   "todos ausentes",
			partes: nil,
			corpo: "Data do caderno não informada,Data de disponibilização não informada," +
				"Id do usuário não informado,Id do caderno não informado,PDF não enviado",
		},
		{
			nome: "datas inválidas, ids ausentes, PDF presente",
			partes: []parteMultipart{
				{nome: "data-caderno", valor: "quinze de março"},
				{nome: "data-disponibilizacao", valor: "ontem"},
				{nome: "pdf", disposicao: `form-data; name="pdf"; filename="d.pdf"`, valor: "%PDF"},
			},
			corpo: "Data do caderno é inválida,Data de disponibilização é inválida," +
				"Id do usuário não informado,Id do caderno não informado",
		},
		{
			nome: "só o id-usuario inválido",
			partes: []parteMultipart{
				{nome: "data-caderno", valor: "2024-03-15"},
				{nome: "data-disponibilizacao", valor: "2024-03-16"},
				{nome: "id-usuario", valor: "sete"},
				{nome: "id-caderno", valor: "9"},
				{nome: "pdf", disposicao: `form-data; name="pdf"; filename="d.pdf"`, valor: "%PDF"},
			},
			corpo: "Id do usuário é inválido",
		},
		{
			nome: "id-caderno acima de int32 é inválido, não ausente",
			partes: []parteMultipart{
				{nome: "data-caderno", valor: "2024-03-15"},
				{nome: "data-disponibilizacao", valor: "2024-03-16"},
				{nome: "id-usuario", valor: "7"},
				{nome: "id-caderno", valor: "2147483648"},
				{nome: "pdf", disposicao: `form-data; name="pdf"; filename="d.pdf"`, valor: "%PDF"},
			},
			corpo: "Id do caderno é inválido",
		},
		{
			nome: "campo presente e VAZIO é inválido, não ausente",
			partes: []parteMultipart{
				{nome: "data-caderno", valor: ""},
				{nome: "data-disponibilizacao", valor: "2024-03-16"},
				{nome: "id-usuario", valor: "7"},
				{nome: "id-caderno", valor: "9"},
				{nome: "pdf", disposicao: `form-data; name="pdf"; filename="d.pdf"`, valor: "%PDF"},
			},
			corpo: "Data do caderno é inválida",
		},
	}

	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			w := httptest.NewRecorder()
			r := requisicaoPDF(corpoMultipart(c.partes...), texto(chaveDeTeste))
			roteador(t).ServeHTTP(w, r)

			if w.Code != http.StatusBadRequest {
				t.Fatalf("status = %d; esperava 400. Corpo: %q", w.Code, w.Body.String())
			}
			if w.Body.String() != c.corpo {
				t.Errorf("corpo\n  obtido   = %q\n  esperado = %q", w.Body.String(), c.corpo)
			}
		})
	}
}

// TestINVP21DatasPermissivasSaoAceitas: `%Y-%m-%d` do chrono é permissivo, e o
// contrato depende disso — os seis formatos abaixo são ACEITOS hoje e o
// `time.Parse` do Go os rejeitaria, mudando o corpo da resposta 400.
func TestINVP21DatasPermissivasSaoAceitas(t *testing.T) {
	for _, data := range []string{
		"2024-3-15", "2024-03-5", "24-03-15", "  2024-03-15", "+2024-03-15", "-2024-03-15",
	} {
		t.Run(data, func(t *testing.T) {
			corpo := corpoMultipart(
				parteMultipart{nome: "data-caderno", valor: data},
				parteMultipart{nome: "data-disponibilizacao", valor: "2024-03-16"},
				parteMultipart{nome: "id-usuario", valor: "7"},
				parteMultipart{nome: "id-caderno", valor: "9"},
				parteMultipart{nome: "pdf", disposicao: `form-data; name="pdf"; filename="d.pdf"`, valor: "%PDF"},
			)
			w := httptest.NewRecorder()
			roteador(t).ServeHTTP(w, requisicaoPDF(corpo, texto(chaveDeTeste)))

			if w.Code != http.StatusOK {
				t.Errorf("status = %d, corpo %q; o legado aceita esta data (INV-P21)",
					w.Code, w.Body.String())
			}
		})
	}
}
