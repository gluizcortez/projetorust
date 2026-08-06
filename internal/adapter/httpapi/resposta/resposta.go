// Package resposta escreve o corpo das respostas do serviço.
//
// Existem duas implementações. `Texto` reproduz o legado byte a byte e é o
// PADRÃO. `ProblemJSON` segue a RFC 7807 e só entra com a chave
// RESPOSTA_PROBLEM_JSON ligada — é evolução, e como toda evolução nasce
// desligada.
//
// A escolha acontece UMA VEZ, na montagem do roteador, e não em cada
// manipulador: um `if` por manipulador seria uma chance a mais de os dois
// formatos divergirem.
package resposta

import (
	"encoding/json"
	"log/slog"
	"net/http"
)

// TipoTexto é o Content-Type de todas as respostas do serviço legado.
//
// MEDIDO, não inferido: `tools/sonda-http` reconstruiu o roteador do Salvo e
// confirmou que as cinco respostas de contrato — 200 do /ping, 401 das duas
// formas, 400 das críticas, 422 e 200 do /pdf — saem todas com este valor,
// tanto para `res.render(Text::Plain(...))` quanto para `res.render(&String)` e
// `res.render(&'static str)`. Ver docs/DECISOES-ABERTAS.md, D-10.
const TipoTexto = "text/plain; charset=utf-8"

// TipoProblemJSON é o Content-Type da RFC 7807.
const TipoProblemJSON = "application/problem+json"

// Escritor traduz um resultado do serviço em corpo de resposta.
//
// Os textos NUNCA são reformatados: chegam prontos do domínio e vão para o
// corpo como estão, sem ponto final acrescentado e sem correção de acentuação.
type Escritor interface {
	// Escrever emite a resposta com o código e o texto literal do legado.
	//
	// `criticas` acompanha apenas a resposta 400 e é ignorada pelo formato de
	// texto — existe porque o problem+json a expõe como lista.
	Escrever(w http.ResponseWriter, r *http.Request, codigo int, texto string, criticas []string)
}

// Texto é o formato do legado: o literal, em text/plain.
type Texto struct{}

var _ Escritor = Texto{}

// Escrever emite o texto exatamente como recebido.
func (Texto) Escrever(w http.ResponseWriter, _ *http.Request, codigo int, texto string, _ []string) {
	w.Header().Set("Content-Type", TipoTexto)
	w.WriteHeader(codigo)
	// O erro de escrita não tem para onde ir: o cabeçalho já foi enviado e a
	// conexão pertence ao cliente. Quem registra é o middleware de registro,
	// que observa o tamanho escrito.
	_, _ = w.Write([]byte(texto))
}

// ProblemJSON é a evolução: RFC 7807, atrás da chave RESPOSTA_PROBLEM_JSON.
//
// Muda o Content-Type e o corpo de TODAS as respostas de erro, então ligá-la
// quebra qualquer cliente que analise o texto. É a razão de nascer desligada.
type ProblemJSON struct {
	// Logger recebe a falha de serialização, que não deveria acontecer.
	Logger *slog.Logger
}

var _ Escritor = ProblemJSON{}

// problema é o documento da RFC 7807.
type problema struct {
	// Type é sempre "about:blank": o serviço não publica uma taxonomia de
	// erros, e a RFC manda usar esse valor quando não há uma.
	Type   string `json:"type"`
	Title  string `json:"title"`
	Status int    `json:"status"`
	// Detail carrega o texto literal do legado, para que a informação não se
	// perca ao trocar de formato.
	Detail string `json:"detail"`
	// Criticas é a extensão específica do serviço: a lista que no formato de
	// texto vem concatenada por vírgula. Omitida quando não há.
	Criticas []string `json:"criticas,omitempty"`
}

// Escrever emite o documento problem+json.
func (p ProblemJSON) Escrever(
	w http.ResponseWriter, _ *http.Request, codigo int, texto string, criticas []string,
) {
	doc := problema{
		Type:     "about:blank",
		Title:    http.StatusText(codigo),
		Status:   codigo,
		Detail:   texto,
		Criticas: criticas,
	}

	corpo, err := json.Marshal(doc)
	if err != nil {
		// Inalcançável: o documento só tem cadeias e um inteiro. Se acontecer,
		// a resposta cai para o formato do legado em vez de ficar sem corpo.
		if p.Logger != nil {
			p.Logger.Error("falha ao serializar problem+json; caindo para texto",
				slog.Any("erro", err))
		}
		Texto{}.Escrever(w, nil, codigo, texto, criticas)
		return
	}

	w.Header().Set("Content-Type", TipoProblemJSON)
	w.WriteHeader(codigo)
	_, _ = w.Write(corpo)
}

// Escolher devolve o escritor conforme a chave de configuração.
func Escolher(problemJSON bool, logger *slog.Logger) Escritor {
	if problemJSON {
		return ProblemJSON{Logger: logger}
	}
	return Texto{}
}
