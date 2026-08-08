package searchidx

import (
	"strings"
	"unicode"
)

// LimiteComprimentoTermo é o comprimento a partir do qual um termo é
// DESCARTADO do fluxo — não truncado.
//
// CONFIRMADO NA FONTE, fase F6, contra tantivy 0.22.1:
//
//	tokenizer_manager.rs:59-67  registra o analisador "default" como
//	                            SimpleTokenizer → RemoveLongFilter::limit(40)
//	                                            → LowerCaser
//	remove_long.rs:36           token.text.len() < self.token_length_limit
//	remove_long.rs:28           "a limit in bytes of the UTF-8 representation"
//
// Duas consequências que a documentação da fonte torna explícitas:
//
//   - a comparação é `<`, então o limite DESCARTA comprimento >= 40;
//   - `len()` de uma String em Rust conta BYTES, não runas (INV-P04).
//
// A medição empírica da fase F0 chegou ao mesmo valor de forma independente:
// `α`×19 (38 bytes, 19 runas) é indexado e `α`×20 (40 bytes, 20 runas) não é.
//
// ⚠ Falta confirmar que produção usa a mesma versão do Tantivy — é a decisão
// aberta D-15. O método de reconfirmação é reexecutar tools/capturar-corpus
// após alinhar o Cargo.lock.
const LimiteComprimentoTermo = 40

// ehAlfanumerico reproduz `char::is_alphanumeric()` do Rust, que o
// SimpleTokenizer do Tantivy usa para decidir onde um termo começa e termina.
//
// NÃO é equivalente a `unicode.IsLetter(r) || unicode.IsDigit(r)` (INV-P22):
//
//	Rust : Alphabetic ∪ {Nd, Nl, No}
//	Go   : L ∪ Nd
//
// A diferença aparece em expoentes e frações — `m²` é UM termo no legado e
// dois num porte ingênuo — e em marcas que o Unicode classifica como
// Other_Alphabetic. As 1.265 runas divergentes estão em
// alfanumerico_table.go, gerada por comparação runa a runa.
func ehAlfanumerico(r rune) bool {
	if decisao, excecao := excecoesAlfanumericas[r]; excecao {
		return decisao
	}
	return unicode.IsLetter(r) || unicode.IsDigit(r)
}

// TermoPosicionado é um termo sobrevivente com a POSIÇÃO que o Tantivy lhe
// atribui.
type TermoPosicionado struct {
	Termo string
	// Posicao conta TODOS os termos segmentados, inclusive os descartados por
	// comprimento, a partir de zero.
	Posicao int
}

// TokenizarComPosicao reproduz o analisador `default` do Tantivy e devolve os
// termos sobreviventes COM a numeração que ele atribui — que conta também os
// descartados.
//
// # Os três estágios, nesta ordem
//
//  1. segmentação  — um termo é a maior sequência de runas alfanuméricas;
//     toda outra runa é separador
//  2. comprimento  — descarta termos com 40 BYTES ou mais
//  3. minúsculas
//
// A MESMA função é aplicada ao texto indexado e à expressão de busca — é o que
// o QueryParser do legado faz. Duas implementações divergiriam em silêncio.
//
// A ordem importa: o descarte por comprimento acontece ANTES da conversão para
// minúsculas, e para alguns caracteres a conversão muda o número de bytes.
//
// # Por que a posição não é o índice na fatia de Tokenizar
//
// No Tantivy, a posição é atribuída pelo TOKENIZADOR, e o descarte por
// comprimento é um FILTRO que roda depois. Um termo removido não é
// renumerado: ele deixa um BURACO. Em `alfa <termo-longo> beta`, `alfa` fica na
// posição 0 e `beta` na 2 — e a busca de frase `"alfa beta"`, que exige posições
// consecutivas, NÃO casa.
//
// Numerar pelo índice da fatia filtrada colocaria `beta` na posição 1 e a frase
// passaria a casar. Era o que esta implementação fazia até a fase F12, e o
// corpus dourado não pegava: nenhum dos 28 documentos tem termo longo ENTRE
// dois termos de uma expressão. Quem pegou foi o teste de propriedade do laço,
// em 10.000 casos gerados.
//
// Ver docs/INVARIANTES.md, INV-P03.
func TokenizarComPosicao(s string) []TermoPosicionado {
	if s == "" {
		return nil
	}

	posicionados := make([]TermoPosicionado, 0, len(s)/8+1)
	posicao := 0

	// A posição é incrementada para TODO termo segmentado; só os sobreviventes
	// entram na saída.
	acrescentarPosicionado := func(termo string) {
		if len(termo) < LimiteComprimentoTermo {
			posicionados = append(posicionados, TermoPosicionado{
				Termo:   strings.ToLower(termo),
				Posicao: posicao,
			})
		}
		posicao++
	}

	inicio := -1
	for i, r := range s {
		if ehAlfanumerico(r) {
			if inicio < 0 {
				inicio = i
			}
			continue
		}
		if inicio >= 0 {
			acrescentarPosicionado(s[inicio:i])
			inicio = -1
		}
	}
	if inicio >= 0 {
		acrescentarPosicionado(s[inicio:])
	}

	return posicionados
}
