package httpapi_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gluizcortez/projetorust/internal/adapter/httpapi"
	"github.com/gluizcortez/projetorust/internal/adapter/httpapi/middleware"
	"github.com/gluizcortez/projetorust/internal/adapter/httpapi/resposta"
	"github.com/gluizcortez/projetorust/internal/domain"
	"github.com/gluizcortez/projetorust/internal/domain/domaintest"
	"github.com/gluizcortez/projetorust/internal/usecase"
)

// -------------------------------------------------------------------------
// Autenticação
// -------------------------------------------------------------------------

// TestAutenticacaoDistingueAusenteDeInvalida fixa D-09: os dois textos são
// DIFERENTES, e a diferença revela se a chave existe.
//
// É divulgação de informação e é contrato existente. Unificá-los quebraria
// clientes que hoje distinguem os casos, então fica — registrado como risco
// aceito em docs/CONTEXT.md.
func TestAutenticacaoDistingueAusenteDeInvalida(t *testing.T) {
	casos := []struct {
		nome  string
		chave *string
		corpo string
	}{
		{"ausente", nil, middleware.TextoChaveAusente},
		{"errada", texto("errada"), middleware.TextoChaveInvalida},
		{"vazia", texto(""), middleware.TextoChaveInvalida},
		{"prefixo correto", texto(chaveDeTeste[:10]), middleware.TextoChaveInvalida},
		{"com espaço ao final", texto(chaveDeTeste + " "), middleware.TextoChaveInvalida},
		{"correta", texto(chaveDeTeste), ""},
	}

	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			w := httptest.NewRecorder()
			roteador(t).ServeHTTP(w, requisicaoPDF(submissaoCompleta(), c.chave))

			if c.corpo == "" {
				if w.Code != http.StatusOK {
					t.Fatalf("status = %d; a chave correta deveria passar", w.Code)
				}
				return
			}
			if w.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d; esperava 401", w.Code)
			}
			if w.Body.String() != c.corpo {
				t.Errorf("corpo = %q; esperava %q", w.Body.String(), c.corpo)
			}
		})
	}
}

// TestCabecalhoDaChaveEInsensivelAMaiusculas — MEDIDO na sonda: o legado aceita
// `x-api-key` minúsculo, porque a busca em HeaderMap é insensível.
func TestCabecalhoDaChaveEInsensivelAMaiusculas(t *testing.T) {
	for _, nome := range []string{"X-API-KEY", "x-api-key", "X-Api-Key"} {
		t.Run(nome, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodPost, httpapi.RotaPDF,
				strings.NewReader(submissaoCompleta()))
			r.Header.Set("Content-Type", "multipart/form-data; boundary="+fronteira)
			r.Header.Set(nome, chaveDeTeste)

			w := httptest.NewRecorder()
			roteador(t).ServeHTTP(w, r)

			if w.Code != http.StatusOK {
				t.Errorf("status = %d com o cabeçalho %q; esperava 200", w.Code, nome)
			}
		})
	}
}

// TestCabecalhoDaChaveRepetidoUsaOPrimeiro — MEDIDO: `HeaderMap::get` devolve o
// primeiro valor.
func TestCabecalhoDaChaveRepetidoUsaOPrimeiro(t *testing.T) {
	casos := []struct {
		nome    string
		valores []string
		codigo  int
	}{
		{"primeira correta", []string{chaveDeTeste, "errada"}, http.StatusOK},
		{"primeira errada", []string{"errada", chaveDeTeste}, http.StatusUnauthorized},
	}

	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodPost, httpapi.RotaPDF,
				strings.NewReader(submissaoCompleta()))
			r.Header.Set("Content-Type", "multipart/form-data; boundary="+fronteira)
			for _, v := range c.valores {
				r.Header.Add("X-API-KEY", v)
			}

			w := httptest.NewRecorder()
			roteador(t).ServeHTTP(w, r)

			if w.Code != c.codigo {
				t.Errorf("status = %d; esperava %d", w.Code, c.codigo)
			}
		})
	}
}

// TestComparacaoDeChaveEmTempoConstante mede o tempo de resposta contra
// prefixos corretos de 0, 8, 16 e 32 bytes, com 10.000 medições cada.
//
// ⚠ ESTE TESTE NÃO DISTINGUE AS DUAS IMPLEMENTAÇÕES, e isso foi MEDIDO.
//
// Trocando `subtle.ConstantTimeCompare` por `==` de cadeia, ele continua
// passando com as mesmas medianas — 743, 791, 786 e 739 ns, sem tendência. A
// razão é aritmética: a comparação inteira de 36 bytes custa poucos
// nanossegundos dentro de uma requisição de ~750 ns, e o `==` do Go usa
// `memequal`, que compara palavra a palavra. O sinal fica duas ordens de
// grandeza abaixo do ruído.
//
// O teste FICA porque detecta uma regressão grosseira — uma comparação byte a
// byte escrita à mão, ou um `strings.HasPrefix` — e porque documenta a medição.
// Mas quem garante o achado A02 é TestComparacaoDeChaveUsaTempoConstante, que é
// estrutural e foi verificado que FALHA com `==`.
//
// A medição usa a MEDIANA, não a média: o relógio de um ambiente compartilhado
// tem caudas longas que arrastam a média e produzem falha intermitente. E a
// comparação é entre prefixos de tamanho IGUAL — chaves de tamanhos diferentes
// são rejeitadas antes de olhar o conteúdo, o que confundiria a medida.
func TestComparacaoDeChaveEmTempoConstante(t *testing.T) {
	if testing.Short() {
		t.Skip("medição de tempo é demorada e sensível a ruído")
	}

	const medicoes = 10_000
	prefixos := []int{0, 8, 16, 32}

	medianas := make(map[int]time.Duration, len(prefixos))
	for _, corretos := range prefixos {
		// Chave do MESMO tamanho da verdadeira, com `corretos` bytes iguais e o
		// resto trocado.
		var b strings.Builder
		b.WriteString(chaveDeTeste[:corretos])
		b.WriteString(strings.Repeat("z", len(chaveDeTeste)-corretos))
		chave := b.String()

		amostras := make([]time.Duration, 0, medicoes)
		for range medicoes {
			r := httptest.NewRequest(http.MethodPost, httpapi.RotaPDF, strings.NewReader(""))
			r.Header.Set("Content-Type", "multipart/form-data; boundary="+fronteira)
			r.Header.Set("X-API-KEY", chave)

			w := httptest.NewRecorder()
			inicio := time.Now()
			autenticadorNu(t).ServeHTTP(w, r)
			amostras = append(amostras, time.Since(inicio))
		}
		sort.Slice(amostras, func(i, j int) bool { return amostras[i] < amostras[j] })
		medianas[corretos] = amostras[len(amostras)/2]
	}

	var menor, maior time.Duration = math.MaxInt64, 0
	for _, corretos := range prefixos {
		d := medianas[corretos]
		t.Logf("%2d bytes corretos: mediana %v", corretos, d)
		if d < menor {
			menor = d
		}
		if d > maior {
			maior = d
		}
	}

	// Um vazamento por encerramento antecipado faz o tempo crescer de forma
	// MONÓTONA com o prefixo. O limiar é frouxo de propósito: o que interessa é
	// não haver correlação estrutural, não medir nanossegundos num ambiente
	// compartilhado.
	if menor == 0 {
		t.Skip("relógio sem resolução suficiente neste ambiente")
	}
	if razao := float64(maior) / float64(menor); razao > 2.0 {
		t.Errorf("a mediana varia %.2fx entre 0 e 32 bytes corretos; "+
			"a comparação parece encerrar antecipadamente", razao)
	}

	monotona := true
	for i := 1; i < len(prefixos); i++ {
		if medianas[prefixos[i]] <= medianas[prefixos[i-1]] {
			monotona = false
			break
		}
	}
	if monotona {
		t.Errorf("as medianas crescem monotonamente com o prefixo correto (%v), "+
			"que é a assinatura do vazamento por tempo", medianas)
	}
}

// TestComparacaoDeChaveUsaTempoConstante é o guarda REAL do achado A02.
//
// Como a medição de tempo não consegue distinguir as implementações neste
// tamanho de chave — ver o teste acima —, a verificação é ESTRUTURAL: o
// código-fonte da autenticação precisa chamar `subtle.ConstantTimeCompare` e
// não pode comparar a credencial por igualdade.
//
// Verificar código-fonte é grosseiro, e é a escolha certa aqui: o teste falha
// exatamente quando alguém troca a comparação, que é o que importa proteger.
// O repositório já usa a mesma técnica na regra de dependência
// (internal/arch_test.go).
func TestComparacaoDeChaveUsaTempoConstante(t *testing.T) {
	fonte, err := os.ReadFile("middleware/middleware.go")
	if err != nil {
		t.Fatalf("lendo o middleware: %v", err)
	}

	conjunto := token.NewFileSet()
	arquivo, err := parser.ParseFile(conjunto, "middleware.go", fonte, 0)
	if err != nil {
		t.Fatalf("analisando o middleware: %v", err)
	}

	var (
		autenticacao       *ast.FuncDecl
		usaTempoConstante  bool
		comparacoesDiretas []string
	)
	for _, decl := range arquivo.Decls {
		if f, ok := decl.(*ast.FuncDecl); ok && f.Name.Name == "Autenticacao" {
			autenticacao = f
		}
	}
	if autenticacao == nil {
		t.Fatal("função Autenticacao não encontrada — o teste perdeu o alvo")
	}

	ast.Inspect(autenticacao, func(n ast.Node) bool {
		switch v := n.(type) {
		case *ast.SelectorExpr:
			if pacote, ok := v.X.(*ast.Ident); ok &&
				pacote.Name == "subtle" && v.Sel.Name == "ConstantTimeCompare" {
				usaTempoConstante = true
			}
		case *ast.BinaryExpr:
			// Só interessa a comparação da CREDENCIAL. `len(valores) == 0` e
			// `ConstantTimeCompare(...) != 1` são legítimas e não podem ser
			// confundidas com o defeito que se quer impedir.
			if (v.Op == token.EQL || v.Op == token.NEQ) &&
				(mencionaCredencial(conjunto, v.X) || mencionaCredencial(conjunto, v.Y)) {
				comparacoesDiretas = append(comparacoesDiretas,
					conjunto.Position(v.Pos()).String())
			}
		}
		return true
	})

	if !usaTempoConstante {
		t.Error("Autenticacao não chama subtle.ConstantTimeCompare — o achado A02 voltou")
	}
	// Uma comparação `==` dentro da função é sempre suspeita: a única coisa
	// comparada ali é a credencial.
	if len(comparacoesDiretas) > 0 {
		t.Errorf("Autenticacao compara por igualdade em %v; a credencial só pode "+
			"ser comparada em tempo constante", comparacoesDiretas)
	}
}

// nomesDaCredencial são os identificadores que carregam a chave dentro de
// Autenticacao. Comparar qualquer um deles por igualdade é o defeito A02.
var nomesDaCredencial = []string{"chave", "esperada", "recebida", "valores["}

func mencionaCredencial(conjunto *token.FileSet, e ast.Expr) bool {
	var b strings.Builder
	if err := printer.Fprint(&b, conjunto, e); err != nil {
		return false
	}
	texto := b.String()

	// Comparar o RESULTADO de ConstantTimeCompare é o uso correto, e a chamada
	// naturalmente menciona os dois operandos.
	if strings.Contains(texto, "subtle.ConstantTimeCompare") {
		return false
	}

	for _, nome := range nomesDaCredencial {
		if strings.Contains(texto, nome) {
			return true
		}
	}
	return false
}

// autenticadorNu isola o middleware de autenticação do resto da cadeia, para
// que a medição de tempo não some o custo de registro, recuperação e análise de
// multipart — que são ruído em relação ao que se quer medir.
func autenticadorNu(t *testing.T) http.Handler {
	t.Helper()
	negar := func(w http.ResponseWriter, r *http.Request, codigo int, texto string) {
		resposta.Texto{}.Escrever(w, r, codigo, texto, nil)
	}
	return middleware.Autenticacao(chaveDeTeste, negar)(
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		}),
	)
}

// -------------------------------------------------------------------------
// Roteamento — o que a sonda mediu
// -------------------------------------------------------------------------

// TestRoteamentoMedido reproduz, uma a uma, as observações de `tools/sonda-http`
// sobre rota inexistente e método não permitido (D-08).
func TestRoteamentoMedido(t *testing.T) {
	casos := []struct {
		metodo, caminho string
		codigo          int
		observacao      string
	}{
		{http.MethodGet, "/ping", http.StatusOK, "a rota normal"},
		{http.MethodGet, "/ping/", http.StatusOK, "barra ao final é normalizada"},
		{http.MethodGet, "/ping//", http.StatusOK, "barras repetidas ao final"},
		{http.MethodGet, "//ping", http.StatusOK, "barra repetida no início"},
		{http.MethodGet, "/./ping", http.StatusOK, "segmento `.` é ignorado"},
		{http.MethodGet, "/ping/x", http.StatusNotFound, "dois segmentos não casam com um"},
		{http.MethodGet, "/PING", http.StatusNotFound, "o caminho é sensível a maiúsculas"},
		{http.MethodGet, "/naoexiste", http.StatusNotFound, "rota inexistente"},
		{http.MethodGet, "/", http.StatusNotFound, "a raiz não é rota"},
		{http.MethodPost, "/ping", http.StatusMethodNotAllowed, "método não permitido"},
		{http.MethodDelete, "/ping", http.StatusMethodNotAllowed, "idem"},
		{http.MethodGet, "/pdf", http.StatusMethodNotAllowed, "o método perde ANTES da autenticação"},
		{http.MethodPut, "/pdf", http.StatusMethodNotAllowed, "idem"},
	}

	for _, c := range casos {
		t.Run(c.metodo+" "+c.caminho, func(t *testing.T) {
			w := httptest.NewRecorder()
			roteador(t).ServeHTTP(w, httptest.NewRequest(c.metodo, c.caminho, nil))
			if w.Code != c.codigo {
				t.Errorf("status = %d; esperava %d (%s)", w.Code, c.codigo, c.observacao)
			}
		})
	}
}

// TestMetodoNaoPermitidoNaoRodaAutenticacao é a observação mais sutil da sonda:
// `GET /pdf` SEM chave devolve 405, não 401 — o roteamento decide primeiro.
func TestMetodoNaoPermitidoNaoRodaAutenticacao(t *testing.T) {
	w := httptest.NewRecorder()
	roteador(t).ServeHTTP(w, httptest.NewRequest(http.MethodGet, httpapi.RotaPDF, nil))

	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d; esperava 405", w.Code)
	}
	if corpo := w.Body.String(); strings.Contains(corpo, "X-API-KEY") {
		t.Errorf("a autenticação rodou antes do roteamento: %q", corpo)
	}
}

// TestCatcherNegociaConteudo cobre a descoberta que um porte ingênuo perderia
// inteira: o corpo do 404 e do 405 depende do cabeçalho `Accept`.
func TestCatcherNegociaConteudo(t *testing.T) {
	casos := []struct {
		aceita      string
		contentType string
		corpo       string
	}{
		{"", "text/html", catcher404HTML},
		{"*/*", "text/html", catcher404HTML},
		{"text/html", "text/html", catcher404HTML},
		{"application/json", "application/json", catcher404JSON},
		{"text/plain", "text/plain", catcher404Texto},
		{"application/xml", "application/xml", catcher404XML},
		{"application/json, text/plain", "application/json", catcher404JSON},
		{"application/json;q=0.9", "application/json", catcher404JSON},
	}

	for _, c := range casos {
		nome := c.aceita
		if nome == "" {
			nome = "(ausente)"
		}
		t.Run(nome, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "/naoexiste", nil)
			if c.aceita != "" {
				r.Header.Set("Accept", c.aceita)
			}
			w := httptest.NewRecorder()
			roteador(t).ServeHTTP(w, r)

			if w.Code != http.StatusNotFound {
				t.Errorf("status = %d", w.Code)
			}
			if obtido := w.Header().Get("Content-Type"); obtido != c.contentType {
				t.Errorf("Content-Type = %q; esperava %q", obtido, c.contentType)
			}
			if w.Body.String() != c.corpo {
				t.Errorf("corpo\n  obtido   = %q\n  esperado = %q", w.Body.String(), c.corpo)
			}
		})
	}
}

// TestCatcher405NegociaIgual.
func TestCatcher405NegociaIgual(t *testing.T) {
	for _, c := range []struct{ aceita, contentType, corpo string }{
		{"application/json", "application/json", catcher405JSON},
		{"text/plain", "text/plain", catcher405Texto},
	} {
		t.Run(c.aceita, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodPost, httpapi.RotaPing, nil)
			r.Header.Set("Accept", c.aceita)
			w := httptest.NewRecorder()
			roteador(t).ServeHTTP(w, r)

			if w.Code != http.StatusMethodNotAllowed {
				t.Errorf("status = %d", w.Code)
			}
			if obtido := w.Header().Get("Content-Type"); obtido != c.contentType {
				t.Errorf("Content-Type = %q; esperava %q", obtido, c.contentType)
			}
			if w.Body.String() != c.corpo {
				t.Errorf("corpo\n  obtido   = %q\n  esperado = %q", w.Body.String(), c.corpo)
			}
		})
	}
}

// -------------------------------------------------------------------------
// Multipart — a divergência medida entre Salvo e ParseMultipartForm
// -------------------------------------------------------------------------

// TestClassificacaoDaParteDoArquivo é o caso nomeado da divergência que a sonda
// encontrou.
//
// O Salvo trata a parte como ARQUIVO quando a `Content-Disposition` tem o
// parâmetro `filename`, ainda que VAZIO. O `ParseMultipartForm` do Go a trataria
// como valor, e a requisição viraria 400 "PDF não enviado" — resposta diferente
// da que o serviço dá hoje.
//
// Se alguém trocar o analisador manual por `ParseMultipartForm`, a segunda linha
// desta tabela cai.
func TestClassificacaoDaParteDoArquivo(t *testing.T) {
	casos := []struct {
		nome        string
		disposicao  string
		codigo      int
		corpo       string
		nomeGravado string
	}{
		{
			nome:       "filename presente",
			disposicao: `form-data; name="pdf"; filename="diario.pdf"`,
			codigo:     http.StatusOK, corpo: httpapi.TextoSucesso, nomeGravado: "diario.pdf",
		},
		{
			nome:       "filename VAZIO ainda é arquivo",
			disposicao: `form-data; name="pdf"; filename=""`,
			codigo:     http.StatusOK, corpo: httpapi.TextoSucesso, nomeGravado: "",
		},
		{
			nome:       "filename com espaço",
			disposicao: `form-data; name="pdf"; filename=" "`,
			codigo:     http.StatusOK, corpo: httpapi.TextoSucesso, nomeGravado: " ",
		},
		{
			nome:       "sem filename NÃO é arquivo",
			disposicao: `form-data; name="pdf"`,
			codigo:     http.StatusBadRequest, corpo: domain.CriticaPDFAusente,
		},
	}

	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			corpo := corpoMultipart(
				parteMultipart{nome: "data-caderno", valor: "2024-03-15"},
				parteMultipart{nome: "data-disponibilizacao", valor: "2024-03-16"},
				parteMultipart{nome: "id-usuario", valor: "7"},
				parteMultipart{nome: "id-caderno", valor: "9"},
				parteMultipart{nome: "pdf", disposicao: c.disposicao, valor: "%PDF"},
			)

			p := montarPilha(t, nil)
			w := httptest.NewRecorder()
			p.roteador.ServeHTTP(w, requisicaoPDF(corpo, texto(chaveDeTeste)))

			if w.Code != c.codigo {
				t.Fatalf("status = %d; esperava %d. Corpo: %q", w.Code, c.codigo, w.Body.String())
			}
			if w.Body.String() != c.corpo {
				t.Errorf("corpo = %q; esperava %q", w.Body.String(), c.corpo)
			}
			if c.codigo != http.StatusOK {
				return
			}
			if len(p.importacoes.Registradas) != 1 {
				t.Fatalf("houve %d registro(s)", len(p.importacoes.Registradas))
			}
			if obtido := p.importacoes.Registradas[0].ArquivoPDF; obtido != c.nomeGravado {
				t.Errorf("nome gravado = %q; esperava %q", obtido, c.nomeGravado)
			}
		})
	}
}

// TestCampoRepetidoUsaOPrimeiro — MEDIDO contra multipart real.
func TestCampoRepetidoUsaOPrimeiro(t *testing.T) {
	casos := []struct {
		nome              string
		primeiro, segundo string
		codigo            int
	}{
		{"primeiro válido", "2024-03-15", "nao-e-data", http.StatusOK},
		{"primeiro inválido", "nao-e-data", "2024-03-15", http.StatusBadRequest},
	}

	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			corpo := corpoMultipart(
				parteMultipart{nome: "data-caderno", valor: c.primeiro},
				parteMultipart{nome: "data-caderno", valor: c.segundo},
				parteMultipart{nome: "data-disponibilizacao", valor: "2024-03-16"},
				parteMultipart{nome: "id-usuario", valor: "7"},
				parteMultipart{nome: "id-caderno", valor: "9"},
				parteMultipart{nome: "pdf", disposicao: `form-data; name="pdf"; filename="d.pdf"`, valor: "%PDF"},
			)
			w := httptest.NewRecorder()
			roteador(t).ServeHTTP(w, requisicaoPDF(corpo, texto(chaveDeTeste)))

			if w.Code != c.codigo {
				t.Errorf("status = %d; esperava %d. Corpo: %q", w.Code, c.codigo, w.Body.String())
			}
		})
	}
}

// TestConteudoDoArquivoChegaAoCasoDeUso: o resumo SHA-256 é do conteúdo, então
// o que a camada HTTP lê tem de ser exatamente o que foi enviado.
func TestConteudoDoArquivoChegaAoCasoDeUso(t *testing.T) {
	const conteudo = "%PDF-1.4\nlinha com acentuação e \x00 byte nulo\n%%EOF"

	corpo := corpoMultipart(
		parteMultipart{nome: "data-caderno", valor: "2024-03-15"},
		parteMultipart{nome: "data-disponibilizacao", valor: "2024-03-16"},
		parteMultipart{nome: "id-usuario", valor: "7"},
		parteMultipart{nome: "id-caderno", valor: "9"},
		parteMultipart{nome: "pdf", disposicao: `form-data; name="pdf"; filename="d.pdf"`, valor: conteudo},
	)

	p := montarPilha(t, nil)
	w := httptest.NewRecorder()
	p.roteador.ServeHTTP(w, requisicaoPDF(corpo, texto(chaveDeTeste)))

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d: %q", w.Code, w.Body.String())
	}
	// O hash gravado é o do conteúdo exato.
	esperado := hashDe(conteudo)
	if obtido := p.importacoes.Registradas[0].HashSHA256; obtido != esperado {
		t.Errorf("hash = %q; esperava %q — o conteúdo lido difere do enviado", obtido, esperado)
	}
}

// TestCorpoNaoMultipartVira422: o legado faz `unwrap()` e entra em pânico
// (achado A05). Responder 422 com o texto do contrato é a correção.
func TestCorpoNaoMultipartVira422(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, httpapi.RotaPDF, strings.NewReader("nao e multipart"))
	r.Header.Set("Content-Type", "text/plain")
	r.Header.Set("X-API-KEY", chaveDeTeste)

	w := httptest.NewRecorder()
	roteador(t).ServeHTTP(w, r)

	if w.Code != http.StatusUnprocessableEntity {
		t.Errorf("status = %d; esperava 422", w.Code)
	}
	if w.Body.String() != httpapi.TextoErroAoProcessar {
		t.Errorf("corpo = %q", w.Body.String())
	}
}

// -------------------------------------------------------------------------
// Middleware
// -------------------------------------------------------------------------

// TestIdentificadorDeRequisicaoEEcoado.
func TestIdentificadorDeRequisicaoEEcoado(t *testing.T) {
	t.Run("gerado quando ausente", func(t *testing.T) {
		w := httptest.NewRecorder()
		roteador(t).ServeHTTP(w, httptest.NewRequest(http.MethodGet, httpapi.RotaPing, nil))

		id := w.Header().Get(middleware.CabecalhoIDRequisicao)
		if len(id) != 32 {
			t.Errorf("identificador = %q; esperava 32 hexadecimais", id)
		}
	})

	t.Run("aproveitado quando presente", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodGet, httpapi.RotaPing, nil)
		r.Header.Set(middleware.CabecalhoIDRequisicao, "id-do-cliente")

		w := httptest.NewRecorder()
		roteador(t).ServeHTTP(w, r)

		if obtido := w.Header().Get(middleware.CabecalhoIDRequisicao); obtido != "id-do-cliente" {
			t.Errorf("identificador = %q; esperava o do cliente", obtido)
		}
	})

	t.Run("dois pedidos recebem identificadores diferentes", func(t *testing.T) {
		h := roteador(t)
		vistos := map[string]bool{}
		for range 100 {
			w := httptest.NewRecorder()
			h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, httpapi.RotaPing, nil))
			id := w.Header().Get(middleware.CabecalhoIDRequisicao)
			if vistos[id] {
				t.Fatalf("identificador repetido: %q", id)
			}
			vistos[id] = true
		}
	})
}

// TestPanicoViraQuinhentosESobrevive é o critério de aceite: o pânico não
// derruba o processo, e o servidor continua atendendo.
func TestPanicoViraQuinhentosESobrevive(t *testing.T) {
	manipulador := middleware.Encadear(
		http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/explode" {
				panic("defeito proposital")
			}
		}),
		middleware.IDRequisicao(),
		middleware.Registro(loggerMudo()),
		middleware.Recuperacao(loggerMudo()),
	)

	w := httptest.NewRecorder()
	manipulador.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/explode", nil))
	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d; esperava 500", w.Code)
	}
	// O corpo vai VAZIO de propósito: o legado não emite nenhum.
	if w.Body.Len() != 0 {
		t.Errorf("corpo = %q; esperava vazio", w.Body.String())
	}

	// E o manipulador continua atendendo.
	w2 := httptest.NewRecorder()
	manipulador.ServeHTTP(w2, httptest.NewRequest(http.MethodGet, "/ok", nil))
	if w2.Code != http.StatusOK {
		t.Errorf("a requisição seguinte devolveu %d", w2.Code)
	}
}

// TestLimiteDeCorpoDesligadoPorPadrao é a paridade: o legado não tem teto
// (docs/ESPECIFICACAO.md §1.4.6).
func TestLimiteDeCorpoDesligadoPorPadrao(t *testing.T) {
	grande := strings.Repeat("A", 4<<20) // 4 MiB
	corpo := corpoMultipart(
		parteMultipart{nome: "data-caderno", valor: "2024-03-15"},
		parteMultipart{nome: "data-disponibilizacao", valor: "2024-03-16"},
		parteMultipart{nome: "id-usuario", valor: "7"},
		parteMultipart{nome: "id-caderno", valor: "9"},
		parteMultipart{nome: "pdf", disposicao: `form-data; name="pdf"; filename="d.pdf"`, valor: grande},
	)

	w := httptest.NewRecorder()
	roteador(t).ServeHTTP(w, requisicaoPDF(corpo, texto(chaveDeTeste)))

	if w.Code != http.StatusOK {
		t.Errorf("status = %d; sem MAX_UPLOAD_BYTES o corpo não tem teto", w.Code)
	}
}

// TestLimiteDeCorpoLigadoRecusa cobre a EVOLUÇÃO atrás de MAX_UPLOAD_BYTES.
//
// A resposta é a opção B de docs/DECISOES-ABERTAS.md, D-16: 400 com a crítica
// nova, e NÃO o 422 genérico. A fase F9 tinha deixado o 422 como provisório,
// antes de a decisão ser tomada.
//
// A crítica vem SOZINHA porque a leitura aborta no meio do corpo: os campos de
// texto deste corpo são todos válidos, mas ainda que não fossem, não haveria
// como sabê-lo.
func TestLimiteDeCorpoLigadoRecusa(t *testing.T) {
	grande := strings.Repeat("A", 64<<10)
	corpo := corpoMultipart(
		parteMultipart{nome: "data-caderno", valor: "2024-03-15"},
		parteMultipart{nome: "data-disponibilizacao", valor: "2024-03-16"},
		parteMultipart{nome: "id-usuario", valor: "7"},
		parteMultipart{nome: "id-caderno", valor: "9"},
		parteMultipart{nome: "pdf", disposicao: `form-data; name="pdf"; filename="d.pdf"`, valor: grande},
	)

	p := montarPilha(t, func(d *httpapi.Dependencias, _ *domaintest.RepositorioImportacaoFalso) {
		d.MaxUploadBytes = 1024
	})

	w := httptest.NewRecorder()
	p.roteador.ServeHTTP(w, requisicaoPDF(corpo, texto(chaveDeTeste)))

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d; esperava 400 com o limite ligado (D-16, opção B)", w.Code)
	}
	if corpo := w.Body.String(); corpo != domain.CriticaPDFAcimaDoLimite {
		t.Errorf("corpo = %q; esperava %q", corpo, domain.CriticaPDFAcimaDoLimite)
	}
	if len(p.importacoes.Registradas) != 0 {
		t.Error("nada deveria ter sido registrado")
	}
}

// -------------------------------------------------------------------------
// Tarefa de fundo
// -------------------------------------------------------------------------

// TestTarefaSobreviveAoFimDaRequisicao é o critério de aceite: o processamento
// continua depois que a requisição HTTP termina e seu contexto é cancelado.
//
// É paridade — no legado a tarefa é disparada com `tokio::task::spawn` e
// sobrevive à resposta (reference/main.rs:256).
func TestTarefaSobreviveAoFimDaRequisicao(t *testing.T) {
	var (
		mu        sync.Mutex
		visto     error
		concluida = make(chan struct{})
	)

	importacoes := &domaintest.RepositorioImportacaoFalso{
		Diario: &domaintest.Diario{}, IDGerado: 42,
	}
	adiadas := make(chan func(context.Context), 1)

	ingestao, err := usecase.NovaIngestao(usecase.DependenciasDaIngestao{
		Importacoes: importacoes,
		Executor: executorQueAdia(func(tarefa func(context.Context)) {
			adiadas <- tarefa
		}),
		Logger: loggerMudo(),
		Processar: func(ctx context.Context, _ int64, _ []byte) {
			mu.Lock()
			visto = ctx.Err()
			mu.Unlock()
			close(concluida)
		},
	})
	if err != nil {
		t.Fatalf("NovaIngestao: %v", err)
	}

	h, err := httpapi.NovoRouter(httpapi.Dependencias{
		Ingestor: ingestao, Logger: loggerMudo(), APIKey: chaveDeTeste,
	})
	if err != nil {
		t.Fatalf("NovoRouter: %v", err)
	}

	// A requisição roda com um contexto que será cancelado logo em seguida.
	ctx, cancelar := context.WithCancel(context.Background())
	r := requisicaoPDF(submissaoCompleta(), texto(chaveDeTeste)).WithContext(ctx)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d: %q", w.Code, w.Body.String())
	}

	// A requisição terminou. Cancela, e só então roda a tarefa.
	cancelar()

	tarefa := <-adiadas
	go tarefa(context.WithoutCancel(ctx))

	select {
	case <-concluida:
	case <-time.After(5 * time.Second):
		t.Fatal("a tarefa não executou depois do fim da requisição")
	}

	mu.Lock()
	defer mu.Unlock()
	if visto != nil {
		t.Errorf("a tarefa viu ctx.Err() = %v; deveria ser nil apesar do cancelamento", visto)
	}
}

type executorAdiante struct{ guardar func(func(context.Context)) }

func (e executorAdiante) Submeter(
	_ context.Context, _ context.Context, tarefa func(context.Context),
) error {
	e.guardar(tarefa)
	return nil
}

func executorQueAdia(guardar func(func(context.Context))) usecase.Executor {
	return executorAdiante{guardar: guardar}
}

// -------------------------------------------------------------------------
// problem+json — a evolução atrás de chave
// -------------------------------------------------------------------------

// TestProblemJSONSoComAChaveLigada: o padrão é o texto puro do legado.
func TestProblemJSONSoComAChaveLigada(t *testing.T) {
	t.Run("desligado é o legado", func(t *testing.T) {
		w := httptest.NewRecorder()
		roteador(t).ServeHTTP(w, requisicaoPDF(corpoMultipart(), texto(chaveDeTeste)))

		if obtido := w.Header().Get("Content-Type"); obtido != resposta.TipoTexto {
			t.Errorf("Content-Type = %q; esperava o do legado", obtido)
		}
	})

	t.Run("ligado emite RFC 7807", func(t *testing.T) {
		p := montarPilha(t, func(d *httpapi.Dependencias, _ *domaintest.RepositorioImportacaoFalso) {
			d.RespostaProblemJSON = true
		})
		w := httptest.NewRecorder()
		p.roteador.ServeHTTP(w, requisicaoPDF(corpoMultipart(), texto(chaveDeTeste)))

		if w.Code != http.StatusBadRequest {
			t.Fatalf("status = %d", w.Code)
		}
		if obtido := w.Header().Get("Content-Type"); obtido != resposta.TipoProblemJSON {
			t.Errorf("Content-Type = %q; esperava %q", obtido, resposta.TipoProblemJSON)
		}

		var doc struct {
			Type     string   `json:"type"`
			Title    string   `json:"title"`
			Status   int      `json:"status"`
			Detail   string   `json:"detail"`
			Criticas []string `json:"criticas"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &doc); err != nil {
			t.Fatalf("corpo não é JSON: %v — %q", err, w.Body.String())
		}
		if doc.Status != http.StatusBadRequest {
			t.Errorf("status no documento = %d", doc.Status)
		}
		if len(doc.Criticas) != 5 {
			t.Errorf("críticas = %v; esperava as cinco", doc.Criticas)
		}
		// O texto do legado continua acessível, sem reformatação.
		if !strings.HasPrefix(doc.Detail, domain.CriticaDataCadernoAusente) {
			t.Errorf("detail = %q; deveria trazer a mensagem literal", doc.Detail)
		}
	})
}

// TestNovoRouterExigeDependencias.
func TestNovoRouterExigeDependencias(t *testing.T) {
	casos := []struct {
		nome string
		deps httpapi.Dependencias
	}{
		{"tudo vazio", httpapi.Dependencias{}},
		{"sem logger", httpapi.Dependencias{Ingestor: ingestorNulo{}, APIKey: "x"}},
		{"sem chave", httpapi.Dependencias{Ingestor: ingestorNulo{}, Logger: loggerMudo()}},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			if _, err := httpapi.NovoRouter(c.deps); err == nil {
				t.Error("NovoRouter aceitou dependências incompletas")
			}
		})
	}
}

type ingestorNulo struct{}

func (ingestorNulo) Executar(context.Context, usecase.ComandoIngerir) (int64, error) {
	return 0, nil
}

// hashDe repete o cálculo do caso de uso para conferência.
func hashDe(s string) string {
	soma := sha256.Sum256([]byte(s))
	return hex.EncodeToString(soma[:])
}
