package pdftext

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/runes"
	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"
)

// As tabelas de exceção Unicode são geradas comparando o Go com o próprio
// Rust, runa a runa. Ver tools/gerar-tabela-diacriticos.
//
// A diretiva vive aqui, e não nas tabelas, porque elas são o ALVO da geração:
// `go generate` as sobrescreve, e a diretiva se perderia junto.
//
//go:generate sh -c "cd ../.. && cargo build --release --quiet --manifest-path tools/gerar-tabela-diacriticos/dump/Cargo.toml && tools/gerar-tabela-diacriticos/dump/target/release/dump-unicode > /tmp/unicode.tsv && go run ./tools/gerar-tabela-diacriticos /tmp/unicode.tsv"

// sequenciaHifenQuebra é o que a junção procura: hífen seguido de quebra de
// linha, exatamente como o padrão `(\w+)(-\n)` do legado.
const sequenciaHifenQuebra = "-\n"

// ehCaractereDePalavra reproduz o `\w` UNICODE do crate `regex`, que o legado
// usa em reference/main.rs:487.
//
// ATENÇÃO — a definição do Rust é
// `[\p{Alphabetic}\p{M}\p{Nd}\p{Pc}\p{Join_Control}]`, e ela diverge do que
// parece óbvio em Go NOS DOIS SENTIDOS. As duas diferenças foram encontradas
// pelo teste de propriedade da fase F6, depois de o corpus inteiro passar:
//
//	INCLUI marcas combinantes  "6"+U+0327 antes de "-\n" JUNTA no legado;
//	                           `[\p{L}\p{N}_]` não juntaria.
//	EXCLUI No e Nl             "¼" antes de "-\n" NÃO junta no legado;
//	                           `\p{N}` do Go inclui No e juntaria.
//
// Nem `\w` do RE2 (que é ASCII, INV-P02) nem `[\p{L}\p{N}_]` servem. As nove
// runas que sobram depois desta aproximação estão em palavra_table.go, gerada
// perguntando ao próprio motor do Rust.
func ehCaractereDePalavra(r rune) bool {
	if decisao, excecao := excecoesDePalavra[r]; excecao {
		return decisao
	}
	return unicode.IsLetter(r) ||
		unicode.Is(unicode.Nl, r) ||
		unicode.Is(unicode.Other_Alphabetic, r) ||
		unicode.IsMark(r) ||
		unicode.Is(unicode.Nd, r) ||
		unicode.Is(unicode.Pc, r) ||
		unicode.Is(unicode.Join_Control, r)
}

// removedorDeMarcas é a decomposição canônica seguida da remoção das marcas
// sem espaçamento — o que o Go oferece nativamente.
//
// Resolve a acentuação do português. NÃO resolve os caracteres que não se
// decompõem; esses vêm de excecoesDeDiacriticos, gerada por comparação com a
// crate do legado.
var removedorDeMarcas = transform.Chain(norm.NFD, runes.Remove(runes.In(unicode.Mn)), norm.NFC)

// JuntarHifens remove o hífen e a quebra de linha que partem uma palavra ao
// fim da linha, reproduzindo `(?imx)(\w+)(-\n)` → `$1` de
// reference/main.rs:498-500.
//
// A junção acontece pela REMOÇÃO da quebra: `"conti-\n"` vira `"conti"`, e a
// linha seguinte é concatenada logo após, produzindo `"continuacao"`. Não há
// operação de emenda explícita.
//
// A varredura é manual, não por expressão regular, porque o RE2 do Go não tem
// como expressar a classe `\w` do Rust: ela mistura a propriedade Alphabetic
// com categorias, e `\p{...}` do RE2 só aceita categorias e scripts.
//
// A semântica de `replace_all` é preservada: a busca é da esquerda para a
// direita, e depois de uma substituição a varredura recomeça DEPOIS do trecho
// consumido. Sem isso, `"a-\n-\nb"` daria resultado diferente do legado.
func JuntarHifens(s string) string {
	if !strings.Contains(s, sequenciaHifenQuebra) {
		return s
	}

	var b strings.Builder
	b.Grow(len(s))

	inicio := 0
	for inicio < len(s) {
		deslocamento := strings.Index(s[inicio:], sequenciaHifenQuebra)
		if deslocamento < 0 {
			b.WriteString(s[inicio:])
			break
		}
		posicao := inicio + deslocamento

		// `\w+` exige ao menos um caractere de palavra imediatamente antes do
		// hífen, e ele precisa estar na região ainda não consumida.
		if posicao > inicio {
			anterior, _ := utf8.DecodeLastRuneInString(s[inicio:posicao])
			if ehCaractereDePalavra(anterior) {
				b.WriteString(s[inicio:posicao]) // descarta o "-\n"
				inicio = posicao + len(sequenciaHifenQuebra)
				continue
			}
		}

		b.WriteString(s[inicio : posicao+len(sequenciaHifenQuebra)])
		inicio = posicao + len(sequenciaHifenQuebra)
	}

	return b.String()
}

// RemoverDiacriticos reproduz `diacritics::remove_diacritics` do legado
// (reference/main.rs:501).
//
// A conversão é POR RUNA, como a da crate: cada caractere consulta primeiro a
// tabela de exceções e, quando não está nela, passa pela decomposição canônica
// do Go. A tabela contém exatamente as runas em que as duas implementações
// discordam — ver INV-P07 e o cabeçalho de diacriticos_table.go.
func RemoverDiacriticos(s string) string {
	// Caminho rápido: ASCII puro não tem o que remover, e é a maior parte do
	// texto de um diário depois da própria normalização.
	if ehASCII(s) {
		return s
	}

	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if mapeado, excecao := excecoesDeDiacriticos[r]; excecao {
			b.WriteString(mapeado)
			continue
		}
		if r < utf8Limite {
			b.WriteRune(r)
			continue
		}
		convertido, _, err := transform.String(removedorDeMarcas, string(r))
		if err != nil {
			b.WriteRune(r)
			continue
		}
		b.WriteString(convertido)
	}
	return b.String()
}

// utf8Limite é o primeiro codepoint fora do ASCII.
const utf8Limite = 0x80

func ehASCII(s string) bool {
	for i := range len(s) {
		if s[i] >= utf8Limite {
			return false
		}
	}
	return true
}

// Normalizar aplica o pipeline do legado, NESTA ORDEM.
//
// A ordem é significativa e está registrada em INV-P08: a junção de hífens
// depende de a classe de caracteres de palavra casar letras acentuadas, o que
// só é verdade enquanto os diacríticos ainda estão lá. Inverter os dois passos
// muda o resultado.
func Normalizar(paginaBruta string) string {
	return RemoverDiacriticos(JuntarHifens(paginaBruta))
}
