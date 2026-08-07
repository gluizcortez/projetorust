package httpapi

import (
	"mime"
	"net/http"
	"strconv"
	"strings"
)

// O "catcher" é o que o Salvo responde quando NENHUMA rota atende a requisição.
// Este arquivo o reproduz.
//
// A especificação marcava §1.5 como `INFERIDO — confirmar` e D-08 pedia captura
// empírica. A captura foi feita: `tools/sonda-http` reconstrói o roteador de
// `reference/main.rs:52-60` e mede o que o Salvo devolve. O resultado é bem mais
// específico do que "o padrão do roteador".
//
// # O que foi medido
//
//	GET  /naoexiste     404
//	GET  /              405   ← e não 404: a raiz existe como rota, sem método
//	POST /ping          405
//	GET  /pdf           405   ← o hoop de autenticação NÃO roda: o método perde antes
//	GET  /pdf sem chave 405   ← idem, não 401
//	GET  /ping/         200   ← a barra ao final é normalizada
//	GET  /PING          404   ← o caminho é sensível a maiúsculas
//
// # O catcher NEGOCIA CONTEÚDO
//
// Esta é a descoberta que um porte ingênuo perderia inteira. O corpo depende do
// cabeçalho `Accept`:
//
//	ausente ou */*        text/html          página de 905 bytes (404) ou 944 (405)
//	text/html             text/html          idem
//	application/json      application/json   {"error":{"code":…,"name":…,"brief":…}}
//	text/plain            text/plain         code: …\n\nname: …\n\nbrief: …
//	application/xml       application/xml    <?xml …><Data><code>…</code>…</Data>
//
// `http.NotFound` do Go devolveria `404 page not found\n` em `text/plain` para
// todos os casos — corpo, tipo e negociação errados de uma vez só.
//
// # A ressalva da versão
//
// A medição vale para o Salvo **0.95.2**, fixado em `tools/sonda-http/Cargo.lock`.
// A versão de produção é a decisão aberta **D-15**, e o HTML do catcher não é
// contrato estável entre versões do Salvo. Status, `Content-Type` e a
// negociação são estáveis; o texto exato precisa ser reconfirmado quando D-15
// for respondida. Ver também **D-20**.

// moldeDoCatcher é a página do Salvo com dois pontos de substituição.
//
// Reproduzida BYTE A BYTE do que a sonda mediu, inclusive o rodapé com o link
// para salvo.rs — que é o que o serviço emite hoje. Trocá-lo por outra coisa
// seria alterar corpo de resposta, e a regra do projeto é preservar. A remoção
// está proposta em D-20.
//
// O molde é o MESMO para 404 e 405: as duas páginas medidas diferem apenas no
// título e na frase, conferido por comparação.
const moldeDoCatcher = `<!DOCTYPE html>
<html>
<head>
    <meta charset="utf-8">
    <meta name="viewport" content="width=device-width">
    <title>{{TITULO}}</title>
    <style>
    :root {
        --bg-color: #fff;
        --text-color: #222;
    }
    body {
        background: var(--bg-color);
        color: var(--text-color);
        text-align: center;
    }
    pre { text-align: left; padding: 0 1rem; }
    footer{text-align:center;}
    @media (prefers-color-scheme: dark) {
        :root {
            --bg-color: #222;
            --text-color: #ddd;
        }
        a:link { color: red; }
        a:visited { color: #a8aeff; }
        a:hover {color: #a8aeff;}
        a:active {color: #a8aeff;}
    }
    </style>
</head>
<body>
    <div><h1>{{TITULO}}</h1><h3>{{FRASE}}</h3><hr><footer><a href="https://salvo.rs" target="_blank">salvo</a></footer></div>
</body>
</html>`

// descricaoDoCatcher são o nome e a frase que o Salvo usa em cada código.
//
// Os textos são os do Salvo, em inglês, e não foram traduzidos: são o corpo que
// o serviço emite hoje.
type descricaoDoCatcher struct {
	nome  string
	frase string
}

var descricoesDoCatcher = map[int]descricaoDoCatcher{
	http.StatusNotFound: {
		nome:  "Not Found",
		frase: "The requested resource could not be found.",
	},
	http.StatusMethodNotAllowed: {
		nome:  "Method Not Allowed",
		frase: "The request method is not supported for the requested resource.",
	},
}

// EscreverCatcher emite a resposta de rota inexistente ou método não permitido,
// negociando o formato pelo cabeçalho `Accept`.
func EscreverCatcher(w http.ResponseWriter, r *http.Request, codigo int) {
	d, conhecido := descricoesDoCatcher[codigo]
	if !conhecido {
		// Nenhum outro código chega aqui pelo roteamento. A guarda existe para
		// que um código novo falhe de forma visível em vez de emitir uma página
		// com campos vazios.
		d = descricaoDoCatcher{nome: http.StatusText(codigo), frase: http.StatusText(codigo)}
	}

	tipo, corpo := negociarCatcher(r.Header.Get("Accept"), codigo, d)
	w.Header().Set("Content-Type", tipo)
	w.WriteHeader(codigo)
	_, _ = w.Write([]byte(corpo))
}

// negociarCatcher escolhe formato e corpo a partir do `Accept`.
//
// A ordem de teste reproduz a do Salvo medida pela sonda: JSON, texto, XML e
// HTML por último, que é também o padrão quando o cabeçalho está ausente ou é
// `*/*`.
func negociarCatcher(aceita string, codigo int, d descricaoDoCatcher) (tipo, corpo string) {
	for _, oferta := range analisarAccept(aceita) {
		switch oferta {
		case "application/json":
			return "application/json", corpoJSONDoCatcher(codigo, d)
		case "text/plain":
			return "text/plain", corpoTextoDoCatcher(codigo, d)
		case "application/xml", "text/xml":
			return "application/xml", corpoXMLDoCatcher(codigo, d)
		case "text/html", "*/*":
			return "text/html", corpoHTMLDoCatcher(codigo, d)
		}
	}
	return "text/html", corpoHTMLDoCatcher(codigo, d)
}

// analisarAccept devolve os tipos de mídia do cabeçalho, na ordem em que
// aparecem, ignorando parâmetros de qualidade.
//
// A negociação do Salvo medida pela sonda é por PRESENÇA, não por peso `q`: um
// `Accept: application/json` devolve JSON, e é só isso que o contrato precisa.
// Ordenar por `q` seria mais correto pela RFC e mudaria o corpo em requisições
// que hoje recebem outro — ou seja, não é paridade.
func analisarAccept(cabecalho string) []string {
	if cabecalho == "" {
		return nil
	}
	partes := strings.Split(cabecalho, ",")
	tipos := make([]string, 0, len(partes))
	for _, parte := range partes {
		tipo, _, err := mime.ParseMediaType(strings.TrimSpace(parte))
		if err != nil {
			continue
		}
		tipos = append(tipos, strings.ToLower(tipo))
	}
	return tipos
}

func corpoHTMLDoCatcher(codigo int, d descricaoDoCatcher) string {
	titulo := strconv.Itoa(codigo) + ": " + d.nome
	corpo := strings.ReplaceAll(moldeDoCatcher, "{{TITULO}}", titulo)
	return strings.ReplaceAll(corpo, "{{FRASE}}", d.frase)
}

// corpoJSONDoCatcher monta o documento à mão em vez de usar encoding/json.
//
// O formato do Salvo é fixo e conhecido, e a serialização automática
// reordenaria ou escaparia campos de forma diferente. Os valores são
// constantes deste arquivo — não vêm da requisição —, então não há o que
// escapar.
func corpoJSONDoCatcher(codigo int, d descricaoDoCatcher) string {
	return `{"error":{"code":` + strconv.Itoa(codigo) +
		`,"name":"` + d.nome +
		`","brief":"` + d.frase + `"}}`
}

func corpoTextoDoCatcher(codigo int, d descricaoDoCatcher) string {
	return "code: " + strconv.Itoa(codigo) +
		"\n\nname: " + d.nome +
		"\n\nbrief: " + d.frase
}

func corpoXMLDoCatcher(codigo int, d descricaoDoCatcher) string {
	return `<?xml version="1.0" encoding="UTF-8"?><Data><code>` + strconv.Itoa(codigo) +
		`</code><name>` + d.nome +
		`</name><brief>` + d.frase + `</brief></Data>`
}
