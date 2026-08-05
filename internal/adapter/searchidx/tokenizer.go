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

// Tokenizar reproduz o analisador `default` do Tantivy, nos três estágios e
// nesta ordem.
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
func Tokenizar(s string) []string {
	if s == "" {
		return nil
	}

	// A capacidade inicial evita realocação no caminho quente: a função roda
	// sobre o texto inteiro de cada página, de cada documento.
	termos := make([]string, 0, len(s)/8+1)

	inicio := -1
	for i, r := range s {
		if ehAlfanumerico(r) {
			if inicio < 0 {
				inicio = i
			}
			continue
		}
		if inicio >= 0 {
			termos = acrescentar(termos, s[inicio:i])
			inicio = -1
		}
	}
	if inicio >= 0 {
		termos = acrescentar(termos, s[inicio:])
	}

	return termos
}

// acrescentar aplica os estágios 2 e 3 a um termo recém-segmentado.
func acrescentar(termos []string, termo string) []string {
	// Estágio 2 — comprimento em BYTES, explicitamente (INV-P04).
	if len(termo) >= LimiteComprimentoTermo {
		return termos
	}
	// Estágio 3 — minúsculas.
	return append(termos, strings.ToLower(termo))
}
