package httpapi_test

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// -------------------------------------------------------------------------
// Como o caminho é comparado — a segunda rodada de medição
// -------------------------------------------------------------------------
//
// A primeira sonda (F9) mediu rota inexistente, método não permitido e o
// `Content-Type` de cada resposta. Ela NÃO mediu caminho com espaço nem com
// percentual-codificação, e `normalizarCaminho` dizia — erradamente — que o
// segmento `.` havia sido medido e era ignorado.
//
// O que trouxe o assunto de volta foi um relato de campo: um `curl` com um
// espaço sobrando na URL virou `GET /ping%20` e recebeu 404.
//
// `tools/sonda-http --bin sonda-caminho` mediu os 27 casos abaixo contra o
// Salvo 0.95.2 de verdade, escrevendo a linha de requisição byte a byte num
// socket — nenhum cliente HTTP no meio, porque é justamente a forma CRUA do
// caminho que está em jogo. Os `status` desta tabela são o que o legado
// respondeu. Reproduzir a medição:
//
//	cargo run --release --manifest-path tools/sonda-http/Cargo.toml \
//	    --bin sonda-caminho
//
// # As três correções que a medição forçou
//
//  1. `/./ping` responde 404, não 200. `normalizarCaminho` descartava `.`
//     junto com os segmentos vazios; o Salvo descarta só os vazios.
//  2. `/ping%2F` responde 404. Em Go, `r.URL.Path` já vem DECODIFICADO, então
//     `%2F` virava barra ANTES do fatiamento e o caminho colapsava para
//     `/ping` — 200 onde o legado dá 404. A comparação passou a ser sobre
//     `r.URL.EscapedPath()`, decodificando SEGMENTO A SEGMENTO, que é a ordem
//     do Salvo.
//  3. `GET /` responde 405, não 404 — a raiz existe como rota e não tem
//     método. `catcher.go` já documentava isso desde a F9; o roteador não
//     fazia.
//
// # Por que socket cru e não httptest.NewRequest
//
// `httptest.NewRequest("/ping%2F")` analisa o alvo e o teste passaria a medir
// o analisador de URL do Go, não o roteador. Com o socket, o byte que sai
// daqui é o byte que o servidor lê.
//
// # A ressalva da versão
//
// Vale para o Salvo 0.95.2, fixado em `tools/sonda-http/Cargo.lock`. A versão
// de produção do legado é a decisão aberta D-15.

// casoDeCaminho é uma linha da tabela medida.
type casoDeCaminho struct {
	// caminho é a forma CRUA, exatamente como vai para a linha de requisição.
	caminho string
	status  int
	// corpo é o corpo esperado quando a rota atende; vazio quer dizer catcher.
	corpo string
	nota  string
}

// caminhosMedidos é a saída de `sonda-caminho`, congelada.
//
// Nenhuma linha aqui é inferida: cada `status` foi lido de uma resposta do
// Salvo. Alterar uma linha exige rodar a sonda de novo.
var caminhosMedidos = []casoDeCaminho{
	// --- o que a sonda da F9 já havia medido, aqui reconfirmado ---
	{"/ping", http.StatusOK, "pong", "o caminho exato"},
	{"/ping/", http.StatusOK, "pong", "barra ao final"},
	{"/ping//", http.StatusOK, "pong", "duas barras ao final"},
	{"//ping", http.StatusOK, "pong", "barra dobrada no início"},
	{"/ping/x", http.StatusNotFound, "", "dois segmentos não casam com um"},
	{"/PING", http.StatusNotFound, "", "o caminho é sensível a maiúsculas"},
	{"/naoexiste", http.StatusNotFound, "", "rota inexistente"},

	// --- a raiz: 405, não 404 ---
	{"/", http.StatusMethodNotAllowed, "", "a raiz é rota sem método"},

	// --- o segmento ponto NÃO é ignorado ---
	{"/./ping", http.StatusNotFound, "", "o segmento `.` conta como segmento"},
	{"/./././ping", http.StatusNotFound, "", "idem, repetido"},

	// --- espaço e afins: a origem do relato de campo ---
	{"/ping%20", http.StatusNotFound, "", "espaço ao final"},
	{"/%20ping", http.StatusNotFound, "", "espaço no início"},
	{"/ping%20%20", http.StatusNotFound, "", "dois espaços ao final"},
	{"/ping+", http.StatusNotFound, "", "`+` só é espaço em query, não em caminho"},
	{"/ping%09", http.StatusNotFound, "", "tabulação ao final"},
	{"/ping%0A", http.StatusNotFound, "", "quebra de linha ao final"},

	// --- a barra codificada: o caso que separa as duas ordens de decodificação ---
	{"/ping%2F", http.StatusNotFound, "", "barra codificada NÃO vira separador"},
	{"/%2Fping", http.StatusNotFound, "", "idem, no início"},
	{"/ping%2F%2F", http.StatusNotFound, "", "idem, repetida"},

	// --- mas a codificação de um caractere comum É resolvida ---
	{"/pi%6Eg", http.StatusOK, "pong", "o `n` codificado casa com `n`"},
	{"/%70ing", http.StatusOK, "pong", "o `p` codificado casa com `p`"},

	// --- ponto-ponto não é resolvido ---
	{"/ping/..", http.StatusNotFound, "", "`..` não sobe um nível"},
	{"/x/../ping", http.StatusNotFound, "", "idem, atravessando"},
	{"/ping%2E", http.StatusNotFound, "", "ponto codificado ao final"},

	// --- a raiz em suas várias formas: zero segmentos é a raiz ---
	{"//", http.StatusMethodNotAllowed, "", "duas barras é a raiz"},
	{"///", http.StatusMethodNotAllowed, "", "três barras também"},
	{"/.", http.StatusNotFound, "", "`.` sozinho é UM segmento, não zero"},
	{"/./", http.StatusNotFound, "", "idem"},

	// --- outros bytes ---
	{"/ping%00", http.StatusNotFound, "", "byte nulo ao final"},
	{"/%2Eping", http.StatusNotFound, "", "ponto codificado no início"},
	{"/%70%69%6E%67", http.StatusOK, "pong", "`ping` inteiro codificado"},
	{"/ping%2f", http.StatusNotFound, "", "barra codificada em minúsculas"},
	{"/ping%23f", http.StatusNotFound, "", "cerquilha CODIFICADA é literal"},
	{"/ping?x=1", http.StatusOK, "pong", "a query não faz parte do caminho"},
}

// metodosMedidos separa "a rota não existe" (404) de "existe sem o método"
// (405) — a distinção que faz a raiz responder 405.
var metodosMedidos = []casoDeCaminho{
	{"/", http.StatusMethodNotAllowed, "", "POST na raiz"},
	{"//", http.StatusMethodNotAllowed, "", "POST em duas barras"},
	{"/naoexiste", http.StatusNotFound, "", "POST em rota inexistente"},
}

// divergenciasConhecidas são os caminhos em que o porte NÃO responde o que o
// legado responde, e a diferença está FORA do alcance do roteador.
//
// As duas causas, ambas anteriores a qualquer manipulador:
//
//  1. PERCENTUAL INVÁLIDO (`/ping%zz`). `url.ParseRequestURI` recusa e o
//     `net/http` responde 400 sozinho. A requisição nunca chega ao roteador,
//     então nenhum middleware e nenhum `Handler` alcança o caso — só um
//     invólucro de `net.Listener` reescrevendo a linha de requisição, o que
//     significaria reimplementar o enquadramento do HTTP a troco de um código
//     de erro.
//
//  2. FRAGMENTO CRU (`/ping#f`). O Salvo descarta `#…` e roteia `/ping`; o Go
//     mantém a cerquilha no caminho. E não dá para separar os dois: MEDIDO,
//     `EscapedPath()` devolve `/ping%23f` tanto para `/ping#f` quanto para
//     `/ping%23f`, que no legado respondem 200 e 404. Distingui-los exigiria
//     rotear por `r.RequestURI`, trocando a entrada do roteador inteiro — com
//     a forma absoluta e o `*` do OPTIONS junto — para perseguir um caso que
//     nenhum cliente conforme produz: pela RFC 3986 §3.5 o fragmento NÃO é
//     enviado ao servidor. Só um socket escrito à mão chega aqui.
//
// Ver **D-24** em docs/DECISOES-ABERTAS.md.
//
// A tabela existe para que a divergência fique ASSERTADA, e não só anotada: se
// uma versão futura do Go passar a responder outra coisa, este teste avisa.
var divergenciasConhecidas = []struct {
	caminho    string
	noLegado   int
	noPorte    int
	corpoPorte string
	causa      string
}{
	{"/ping%", http.StatusNotFound, http.StatusBadRequest, "400 Bad Request",
		"percentual solto: url.ParseRequestURI recusa"},
	{"/ping%2", http.StatusNotFound, http.StatusBadRequest, "400 Bad Request",
		"percentual truncado: idem"},
	{"/ping%zz", http.StatusNotFound, http.StatusBadRequest, "400 Bad Request",
		"dígitos inválidos: idem"},

	{"/ping#f", http.StatusOK, http.StatusNotFound, "salvo.rs",
		"fragmento cru: o legado descarta `#…`, o Go mantém"},
	{"/pdf#x", http.StatusMethodNotAllowed, http.StatusNotFound, "salvo.rs",
		"idem, na rota autenticada — e o 404 é MAIS restritivo que o 405"},

	// A tabulação crua os dois recusam com 400; só o CORPO difere — o Salvo
	// devolve vazio, o Go devolve "400 Bad Request". Mesma camada, mesma D-24.
	{"/ping\tx", http.StatusBadRequest, http.StatusBadRequest, "400 Bad Request",
		"tabulação crua: 400 nos dois, corpo diferente"},
}

// TestDivergenciasDeCaminhoSaoAsConhecidas fixa a fronteira do que o porte
// consegue reproduzir. Uma divergência nova aqui é uma regressão; uma que
// SUMA é motivo para apagar a linha e devolver o caso à tabela medida.
func TestDivergenciasDeCaminhoSaoAsConhecidas(t *testing.T) {
	servidor := httptest.NewServer(roteador(t))
	defer servidor.Close()

	for _, d := range divergenciasConhecidas {
		t.Run(d.caminho, func(t *testing.T) {
			status, corpo := requisitarCru(t, servidor.Listener.Addr().String(), d.caminho)

			if d.noPorte != d.noLegado && status == d.noLegado {
				t.Fatalf("o porte agora responde %d como o legado (%s) — "+
					"D-24 encolheu: apague esta linha e mova o caso para "+
					"caminhosMedidos", status, d.causa)
			}
			if status != d.noPorte {
				t.Errorf("status = %d; a divergência registrada é %d (legado: %d, %s)",
					status, d.noPorte, d.noLegado, d.causa)
			}
			if !strings.Contains(corpo, d.corpoPorte) {
				t.Errorf("corpo = %q; esperava conter %q", primeirosBytes(corpo), d.corpoPorte)
			}
		})
	}
}

// TestCaminhoComoOLegadoCompara roda cada caso medido contra o roteador de
// produção, por socket cru.
func TestCaminhoComoOLegadoCompara(t *testing.T) {
	servidor := httptest.NewServer(roteador(t))
	defer servidor.Close()

	for _, c := range caminhosMedidos {
		t.Run(c.caminho, func(t *testing.T) {
			status, corpo := requisitarCru(t, servidor.Listener.Addr().String(), c.caminho)

			if status != c.status {
				t.Errorf("status = %d; o legado responde %d (%s)", status, c.status, c.nota)
			}
			if c.corpo != "" && corpo != c.corpo {
				t.Errorf("corpo = %q; esperava %q", corpo, c.corpo)
			}
			if c.corpo == "" && status >= 400 && !strings.Contains(corpo, "salvo.rs") {
				t.Errorf("corpo não é o catcher do legado: %q", primeirosBytes(corpo))
			}
		})
	}

	for _, c := range metodosMedidos {
		t.Run("POST "+c.caminho, func(t *testing.T) {
			status, _ := requisitarCruCom(
				t, servidor.Listener.Addr().String(), http.MethodPost, c.caminho)
			if status != c.status {
				t.Errorf("status = %d; o legado responde %d (%s)", status, c.status, c.nota)
			}
		})
	}
}

// TestCaminhoNaoEscapaDaRotaPorCodificacao é a leitura de segurança da mesma
// medição: nenhuma forma codificada de `/pdf` deve alcançar `/pdf` por um
// caminho que o legado recusaria — e nenhuma deve alcançá-lo SEM passar pela
// autenticação.
//
// O 405 aqui é o que a F9 mediu para `GET /pdf`: o método perde antes do hoop.
// O que este teste garante é que as formas exóticas do caminho ou não chegam
// (404) ou chegam ao mesmo lugar que `/pdf` chega (405) — nunca a um 200.
func TestCaminhoNaoEscapaDaRotaPorCodificacao(t *testing.T) {
	servidor := httptest.NewServer(roteador(t))
	defer servidor.Close()

	for _, caminho := range []string{
		"/pdf",
		"/pdf/",
		"//pdf",
		"/pdf%2F",
		"/pd%66",
		"/./pdf",
		"/x/../pdf",
		"/pdf%20",
	} {
		t.Run(caminho, func(t *testing.T) {
			status, corpo := requisitarCru(t, servidor.Listener.Addr().String(), caminho)

			if status == http.StatusOK {
				t.Fatalf("GET %s devolveu 200: %q", caminho, primeirosBytes(corpo))
			}
			if strings.Contains(corpo, "X-API-KEY") {
				t.Errorf("a autenticação rodou: o roteamento deveria decidir antes (%q)", corpo)
			}
		})
	}
}

// requisitarCru escreve a linha de requisição byte a byte e lê a resposta.
//
// É a mesma técnica de `tools/sonda-http --bin sonda-caminho`, para que os
// dois lados vejam exatamente os mesmos bytes.
func requisitarCru(t *testing.T, endereco, caminho string) (int, string) {
	t.Helper()
	return requisitarCruCom(t, endereco, http.MethodGet, caminho)
}

func requisitarCruCom(t *testing.T, endereco, metodo, caminho string) (int, string) {
	t.Helper()

	conexao, err := net.DialTimeout("tcp", endereco, 5*time.Second)
	if err != nil {
		t.Fatalf("conectar: %v", err)
	}
	defer func() { _ = conexao.Close() }()

	if err := conexao.SetDeadline(time.Now().Add(10 * time.Second)); err != nil {
		t.Fatalf("prazo: %v", err)
	}

	requisicao := fmt.Sprintf(
		"%s %s HTTP/1.1\r\nHost: localhost\r\nConnection: close\r\n\r\n", metodo, caminho)
	if _, err := conexao.Write([]byte(requisicao)); err != nil {
		t.Fatalf("escrever: %v", err)
	}

	leitor := bufio.NewReader(conexao)
	resposta, err := http.ReadResponse(leitor, nil)
	if err != nil {
		t.Fatalf("ler a resposta: %v", err)
	}
	defer func() { _ = resposta.Body.Close() }()

	corpo, err := io.ReadAll(resposta.Body)
	if err != nil {
		t.Fatalf("ler o corpo: %v", err)
	}
	return resposta.StatusCode, string(corpo)
}

func primeirosBytes(s string) string {
	if len(s) <= 80 {
		return s
	}
	return s[:80] + "…"
}
