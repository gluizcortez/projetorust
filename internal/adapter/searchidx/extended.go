package searchidx

import (
	"strings"
	"unicode"
)

// StripExtended reproduz o pré-processamento léxico da flag `x` (modo
// *extended*) do crate `regex` do Rust, que o RE2 do Go não tem.
//
// O legado monta o filtro do operador `&` como
// `format!("(?imx){}", key.replace('&', r"\s*&\s*"))`
// (reference/main.rs:394). A flag `x` faz o motor IGNORAR TODO ESPAÇO LITERAL
// do padrão — e as expressões vêm do banco COM espaços. Para a chave
// "ACME & FILHOS", o padrão efetivo em Rust é `ACME\s*&\s*FILHOS`. Uma
// tradução ingênua produziria `ACME \s*&\s* FILHOS`, que exige dois espaços
// literais a mais e DEIXA DE CASAR com o texto que hoje casa. Ver
// docs/INVARIANTES.md, INV-P01.
//
// # As regras, medidas e não deduzidas
//
// As três regras abaixo foram obtidas comparando o `Hir` de `(?imx)X` com o de
// `(?im)Y` para centenas de pares, com `tools/capturar-corpus/src/bin/
// sonda-extended.rs`. Dois padrões com o mesmo `Hir` são o mesmo autômato.
//
//  1. ESPAÇO EM BRANCO é descartado. O conjunto é exatamente as 25 runas da
//     propriedade Unicode White_Space — o mesmo conjunto que o `\s` do Rust
//     aceita, verificado runa a runa em todo o Unicode. Inclui `\v`, NBSP,
//     U+1680, U+2000-200A, U+2028, U+2029, U+202F, U+205F e U+3000. NÃO inclui
//     o espaço de largura zero U+200B.
//
//  2. `#` inicia um COMENTÁRIO que vai até o próximo `\n` — apenas `\n`, nunca
//     `\r` — ou até o fim do padrão.
//
//  3. `\` ESCAPA a runa seguinte, que sobrevive intacta: `\ ` é um espaço
//     literal e `\#` é um `#` literal. A barra dupla `\\` é uma barra literal,
//     e o `#` que vier depois dela VOLTA a iniciar comentário.
//
// # Duas correções à especificação da fase
//
// O enunciado de F7 afirmava que o modo `x` PRESERVA espaços dentro de classes
// de caracteres `[...]`, como fazem o PCRE e o Python. A medição REFUTA as duas
// metades da afirmação no motor do Rust:
//
//	(?imx)[a b]   ≡  (?im)[ab]     — o espaço é descartado dentro da classe
//	(?imx)[a # b] →  erro de sintaxe: unclosed character class
//
// O `#` também inicia comentário dentro da classe, e o comentário engole o `]`
// que a fecharia. O Rust é o oráculo: a especificação foi corrigida. A
// consequência prática é boa — esta função NÃO precisa rastrear classes de
// caracteres, o que elimina toda uma família de erros de borda.
//
// # Duas divergências residuais, conhecidas e limitadas
//
// Esta é uma transformação LÉXICA sobre a cadeia inteira; no Rust, o descarte
// acontece DURANTE a análise sintática. As duas construções em que isso é
// observável estão registradas em docs/DECISOES-ABERTAS.md, D-19:
//
//   - `(?-x)` e `(?-x:...)` desligam o modo no escopo. O Rust volta a tratar
//     espaço como literal ali dentro; esta função não. O `x` não existe como
//     flag do RE2, então o Go rejeita o padrão na compilação — a divergência é
//     RUIDOSA, não silenciosa.
//   - `(? i)` e `(?P< n >a)`: o Rust REJEITA (o descarte não alcança o interior
//     desses tokens), o Go aceita o resultado já sem espaços. Divergência
//     silenciosa, exige que a expressão contenha sintaxe de grupo.
//
// Nenhuma das duas é alcançável por uma expressão de perfil que não contenha
// sintaxe de agrupamento de expressão regular.
func StripExtended(padrao string) string {
	if !precisaDeCorte(padrao) {
		return padrao
	}

	var b strings.Builder
	b.Grow(len(padrao))

	runas := []rune(padrao)
	for i := 0; i < len(runas); i++ {
		r := runas[i]

		switch {
		case r == '\\':
			// Regra 3 — a barra e a runa seguinte saem intactas. Uma barra no
			// fim do padrão é escape incompleto: sai sozinha e o compilador do
			// Go recusa, como o do Rust recusa.
			b.WriteRune(r)
			if i+1 < len(runas) {
				i++
				b.WriteRune(runas[i])
			}

		case unicode.IsSpace(r):
			// Regra 1 — descartado, dentro ou fora de classe de caracteres.

		case r == '#':
			// Regra 2 — consome até o `\n` INCLUSIVE, ou até o fim.
			for i+1 < len(runas) && runas[i+1] != '\n' {
				i++
			}
			if i+1 < len(runas) {
				i++ // descarta o próprio `\n`
			}

		default:
			b.WriteRune(r)
		}
	}

	return b.String()
}

// precisaDeCorte evita alocar quando não há nada a cortar — o caso comum de
// uma expressão de uma palavra só.
//
// A barra invertida NÃO entra no teste de propósito: sem espaço em branco e
// sem `#` no padrão, nenhum escape muda o resultado.
func precisaDeCorte(padrao string) bool {
	for _, r := range padrao {
		if r == '#' || unicode.IsSpace(r) {
			return true
		}
	}
	return false
}
