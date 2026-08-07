package searchidx

import (
	"fmt"
	"regexp"
	"strings"
	"sync"

	"github.com/gluizcortez/projetorust/src/domain"
)

// substituicaoDoOperador é o que o legado injeta no lugar de cada `&`
// (reference/main.rs:394):
//
//	key.replace('&', r"\s*&\s*")
//
// A substituição é feita com o `\s` LITERAL, exatamente como no Rust, e só
// depois traduzida — ver traduzirClassesPerl. Injetar aqui a classe já
// expandida quebraria a fidelidade quando a expressão terminasse em `\`
// imediatamente antes do `&`: no Rust a barra escaparia o `\` do `\s`, e com a
// classe expandida ela escaparia um `[`.
const substituicaoDoOperador = `\s*&\s*`

// intervalosDeEspacoDoRust são as 25 runas da propriedade Unicode White_Space,
// que é EXATAMENTE o conjunto que o `\s` do crate `regex` aceita — verificado
// runa a runa em todo o Unicode pela sonda da fase F7.
//
// O `\s` do RE2 é `[\t\n\f\r ]`: cinco runas, sem sequer o `\v`. Traduzir `\s`
// por `\s` faria o filtro do operador `&` DEIXAR DE CASAR texto que hoje casa,
// sempre que o separador ao redor do `&` fosse um espaço não quebrável, um
// espaço ideográfico ou uma tabulação vertical — todos possíveis na saída do
// MuPDF (INV-P10).
//
// Escrito sem NENHUM caractere de espaço literal: esta cadeia atravessa
// StripExtended, que descartaria um espaço literal aqui dentro.
const intervalosDeEspacoDoRust = `\x{09}-\x{0D}\x{20}\x{85}\x{A0}\x{1680}` +
	`\x{2000}-\x{200A}\x{2028}\x{2029}\x{202F}\x{205F}\x{3000}`

// classeDeEspacoDoRust é a forma usável fora de uma classe de caracteres.
const classeDeEspacoDoRust = "[" + intervalosDeEspacoDoRust + "]"

// intervalosNaoEspacoDoRust é o COMPLEMENTO de intervalosDeEspacoDoRust sobre
// todo o Unicode — o `\S` do Rust.
//
// Existe porque o RE2 não aninha classes negadas: `[a\Sb]` não pode virar
// `[a[^...]b]`. Com os intervalos complementados explicitamente, a tradução
// funciona dentro e fora de classe.
//
// Escrito à mão e verificado por varredura exaustiva em
// TestClasseDeNaoEspacoEOComplemento — a aritmética de borda de onze intervalos
// não é coisa para confiar na leitura.
const intervalosNaoEspacoDoRust = `\x{00}-\x{08}\x{0E}-\x{1F}\x{21}-\x{84}` +
	`\x{86}-\x{9F}\x{A1}-\x{167F}\x{1681}-\x{1FFF}\x{200B}-\x{2027}` +
	`\x{202A}-\x{202E}\x{2030}-\x{205E}\x{2060}-\x{2FFF}\x{3001}-\x{10FFFF}`

// classeDeNaoEspacoDoRust é a forma usável fora de uma classe de caracteres.
const classeDeNaoEspacoDoRust = "[" + intervalosNaoEspacoDoRust + "]"

// classesPerlSemTraducaoExata são as classes Perl cuja definição no Rust não
// tem forma exata em RE2.
//
// O `\w` do Rust é `[\p{Alphabetic}\p{M}\p{Nd}\p{Pc}\p{Join_Control}]`.
// `Alphabetic` e `Join_Control` são propriedades DERIVADAS do Unicode, e o
// `\p{...}` do RE2 só expõe categorias gerais e sistemas de escrita. O `\b` e o
// `\B` são piores ainda: usam o `\w` INTERNO do motor, que no RE2 é ASCII e não
// é configurável.
//
// Traduzi-las por aproximação criaria uma divergência SILENCIOSA — a pior
// espécie, porque grava recorte errado sem que ninguém perceba. Recusar a
// expressão troca isso por uma falha ALTA, no mesmo regime em que o RE2 já
// recusa `(?-x)`. Ver docs/DECISOES-ABERTAS.md, D-19, que descreve também o
// conserto exato (tabela de intervalos gerada a partir do próprio motor do
// Rust) caso alguma expressão real venha a precisar.
var classesPerlSemTraducaoExata = map[rune]string{
	'w': `\w`,
	'W': `\W`,
	'b': `\b`,
	'B': `\B`,
}

// CompilarFiltro traduz uma expressão de perfil no filtro do operador `&` do
// legado, pronto para uso.
//
// Reproduz reference/main.rs:392-398 em quatro passos, NESTA ORDEM, que é a do
// legado:
//
//  1. cada `&` vira `\s*&\s*`;
//  2. o resultado passa por StripExtended, que replica a flag `x` (INV-P01);
//  3. as classes Perl que o RE2 define de forma mais estreita que o Rust são
//     traduzidas;
//  4. compila com `(?im)` — o `x` já foi consumido no passo 2.
//
// Inverter os passos 1 e 2 daria o mesmo resultado em toda entrada exceto uma
// (`&` precedido de `\`), e não há razão para correr o risco.
func CompilarFiltro(expressao string) (*regexp.Regexp, error) {
	padrao := strings.ReplaceAll(expressao, "&", substituicaoDoOperador)
	padrao = StripExtended(padrao)

	padrao, err := traduzirClassesPerl(padrao)
	if err != nil {
		return nil, fmt.Errorf("%w: expressão %q: %w", domain.ErrExpressaoInvalida, expressao, err)
	}

	re, err := regexp.Compile("(?im)" + padrao)
	if err != nil {
		// O legado faz `.unwrap()` aqui e ENTRA EM PÂNICO. O efeito observável
		// do pânico NÃO é o mesmo de um erro — ver docs/DECISOES-ABERTAS.md,
		// D-19, e o comentário de cacheDeFiltros.Obter.
		return nil, fmt.Errorf("%w: expressão %q compila como %q: %w",
			domain.ErrExpressaoInvalida, expressao, padrao, err)
	}
	return re, nil
}

// traduzirClassesPerl reescreve as classes Perl cuja definição no RE2 é mais
// ESTREITA que no crate `regex` do Rust.
//
//	\s  Rust: as 25 runas de White_Space   RE2: [\t\n\f\r ]
//	\S  Rust: o complemento delas          RE2: o complemento de cinco runas
//	\d  Rust: \p{Nd}                       RE2: [0-9]
//	\D  Rust: o complemento de \p{Nd}      RE2: [^0-9]
//
// As quatro têm forma exata em RE2, dentro e fora de classe de caracteres, e
// são traduzidas. As de classesPerlSemTraducaoExata não têm, e a expressão é
// RECUSADA em vez de traduzida por aproximação.
//
// A varredura é sensível a escape e a classe de caracteres: `\\s` é uma barra
// literal seguida de `s`, não a classe; e dentro de `[...]` a classe entra sem
// os colchetes, que o RE2 não aninha.
//
// O `\S` deste comentário não é decorativo: ele custou 13 divergências
// SILENCIOSAS em 250.000 casos do teste de propriedade da fase F7, todas por
// `\S` ter sido deixado de fora numa primeira versão. O texto que justificava
// a omissão estava errado — o complemento explícito resolve o aninhamento.
func traduzirClassesPerl(padrao string) (string, error) {
	// O caminho rápido precisa das DUAS condições. Testar só a barra invertida
	// pularia a varredura em `[a[bc]]`, que não tem barra alguma e é justamente
	// a classe aninhada que a varredura existe para recusar.
	if !strings.ContainsAny(padrao, `\[`) {
		return padrao, nil
	}

	var b strings.Builder
	b.Grow(len(padrao) + 64)

	emClasse := false
	runas := []rune(padrao)
	for i := 0; i < len(runas); i++ {
		r := runas[i]

		if r == '\\' {
			if i+1 >= len(runas) {
				b.WriteRune(r) // escape incompleto: o compilador recusa
				break
			}
			i++
			if nome, semTraducao := classesPerlSemTraducaoExata[runas[i]]; semTraducao {
				return "", fmt.Errorf(
					"a classe %s do crate `regex` não tem forma exata em RE2 "+
						"e traduzi-la por aproximação seria divergência silenciosa "+
						"(ver docs/DECISOES-ABERTAS.md, D-19)", nome)
			}
			switch runas[i] {
			case 's':
				b.WriteString(escolherForma(emClasse, intervalosDeEspacoDoRust, classeDeEspacoDoRust))
			case 'S':
				b.WriteString(escolherForma(emClasse, intervalosNaoEspacoDoRust, classeDeNaoEspacoDoRust))
			case 'd':
				b.WriteString(`\p{Nd}`)
			case 'D':
				b.WriteString(`\P{Nd}`)
			default:
				b.WriteRune('\\')
				b.WriteRune(runas[i])
			}
			continue
		}

		switch {
		case r == '[' && !emClasse:
			emClasse = true
		case r == '[' && emClasse:
			// CLASSE ANINHADA. O crate `regex` aceita `[a[bc]]` e lê a UNIÃO
			// dos dois conjuntos; o RE2 não tem o conceito e lê `[` como um
			// caractere literal da classe externa. Os dois compilam e produzem
			// autômatos DIFERENTES — divergência silenciosa.
			//
			// Não há tradução: reproduzir a união exigiria um analisador de
			// classes com álgebra de conjuntos, para emitir os intervalos
			// achatados. Recusar é a mesma política de classesPerlSemTraducaoExata.
			//
			// Recusa junto, de propósito, a classe POSIX `[[:alpha:]]`, que os
			// dois motores tratam igual. É uma recusa a mais numa construção
			// que uma razão social não contém, em troca de uma regra simples o
			// bastante para estar obviamente correta.
			return "", fmt.Errorf(
				"classe de caracteres aninhada na posição %d: o crate `regex` lê "+
					"como união de conjuntos e o RE2 lê o `[` como literal, "+
					"sem tradução possível (ver docs/DECISOES-ABERTAS.md, D-19)", i)
		case r == ']' && emClasse:
			emClasse = false
		}
		b.WriteRune(r)
	}

	return b.String(), nil
}

// escolherForma devolve os intervalos crus quando já se está dentro de uma
// classe de caracteres, e a classe entre colchetes quando não.
func escolherForma(emClasse bool, crus, entreColchetes string) string {
	if emClasse {
		return crus
	}
	return entreColchetes
}

// cacheDeFiltros guarda a expressão regular já compilada de cada expressão de
// perfil.
//
// Resolve o achado A08: o legado compila a expressão regular DENTRO do laço de
// resultados (reference/main.rs:394), uma vez por página que casou. Compilar é
// caro e o resultado é sempre o mesmo.
//
// O cache é uma otimização PURA — não muda resultado nenhum, o que é a razão de
// não estar atrás de chave de configuração como as evoluções da fase F11.
//
// Cresce sem limite, e isso é deliberado: a chave é `expressao_nm` de
// `tb_perfil_variacao`, cujo número de valores distintos é limitado pela
// própria tabela. Não é entrada arbitrária de usuário.
type cacheDeFiltros struct {
	mu        sync.RWMutex
	compilado map[string]resultadoDeCompilacao
}

// resultadoDeCompilacao memoriza também a FALHA. Recompilar uma expressão
// inválida a cada página só produziria o mesmo erro mais devagar.
type resultadoDeCompilacao struct {
	re  *regexp.Regexp
	err error
}

func novoCacheDeFiltros() *cacheDeFiltros {
	return &cacheDeFiltros{compilado: make(map[string]resultadoDeCompilacao)}
}

// Obter devolve o filtro compilado da expressão, compilando na primeira vez.
//
// PARIDADE — a compilação é PREGUIÇOSA de propósito. No legado ela acontece
// dentro do laço sobre os acertos (main.rs:392-398), então uma expressão com
// `&` e sintaxe inválida que não produz NENHUM acerto de frase JAMAIS é
// compilada e JAMAIS entra em pânico. Chamar Obter antes de saber que há pelo
// menos um acerto transformaria um caso hoje bem-sucedido em falha. Ver
// docs/INVARIANTES.md, INV-P23.
func (c *cacheDeFiltros) Obter(expressao string) (*regexp.Regexp, error) {
	c.mu.RLock()
	r, achou := c.compilado[expressao]
	c.mu.RUnlock()
	if achou {
		return r.re, r.err
	}

	re, err := CompilarFiltro(expressao)

	c.mu.Lock()
	c.compilado[expressao] = resultadoDeCompilacao{re: re, err: err}
	c.mu.Unlock()

	return re, err
}
