// Comando gerar-tabela-diacriticos emite as tabelas de exceção que fazem o Go
// concordar com o Rust em duas decisões por caractere.
//
// As tabelas NÃO são escritas à mão. O auxiliar em `dump/` percorre todas as
// runas do plano básico multilíngue e imprime o que o Rust decide; este
// programa compara com o que o Go decide nativamente e emite APENAS as
// divergências.
//
//  1. Diacríticos — `diacritics::remove_diacritics` contra
//     norm.NFD + remoção de marcas + norm.NFC (INV-P07).
//  2. Classificação alfanumérica — `char::is_alphanumeric` do Rust, que o
//     SimpleTokenizer do Tantivy usa, contra `unicode.IsLetter ||
//     unicode.IsDigit` do Go (INV-P22).
//  3. Classe de caractere de palavra — o `\w` Unicode do crate `regex`, que a
//     junção de hífens usa, contra a aproximação nativa do Go (INV-P02).
//
// Uso:
//
//	cargo build --release --manifest-path tools/gerar-tabela-diacriticos/dump/Cargo.toml
//	tools/gerar-tabela-diacriticos/dump/target/release/dump-unicode > /tmp/unicode.tsv
//	go run ./tools/gerar-tabela-diacriticos /tmp/unicode.tsv
//
// Invocado por `go generate ./...`.
package main

import (
	"bufio"
	"fmt"
	"go/format"
	"os"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"golang.org/x/text/runes"
	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"
)

const (
	saidaDiacriticos = "internal/adapter/pdftext/diacriticos_table.go"
	saidaAlfanum     = "internal/adapter/searchidx/alfanumerico_table.go"
	saidaPalavra     = "internal/adapter/pdftext/palavra_table.go"
)

type entrada struct {
	runa       rune
	alfanum    bool
	palavra    bool
	mapeamento string
}

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "uso: gerar-tabela-diacriticos <arquivo.tsv>")
		os.Exit(1)
	}

	entradas, err := lerDespejo(os.Args[1])
	if err != nil {
		fmt.Fprintf(os.Stderr, "lendo o despejo: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("runas analisadas: %d\n", len(entradas))

	diac := divergenciasDeDiacriticos(entradas)
	alnum := divergenciasAlfanumericas(entradas)
	pal := divergenciasDePalavra(entradas)

	fmt.Printf("divergências de diacríticos          : %d\n", len(diac))
	fmt.Printf("divergências de classificação alnum  : %d\n", len(alnum))
	fmt.Printf("divergências de caractere de palavra : %d\n", len(pal))

	if err := emitirDiacriticos(diac); err != nil {
		fmt.Fprintf(os.Stderr, "emitindo tabela de diacríticos: %v\n", err)
		os.Exit(1)
	}
	if err := emitirAlfanumerico(alnum); err != nil {
		fmt.Fprintf(os.Stderr, "emitindo tabela alfanumérica: %v\n", err)
		os.Exit(1)
	}
	if err := emitirPalavra(pal); err != nil {
		fmt.Fprintf(os.Stderr, "emitindo tabela de caractere de palavra: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("gerados:\n  %s\n  %s\n  %s\n", saidaDiacriticos, saidaAlfanum, saidaPalavra)
}

func lerDespejo(caminho string) ([]entrada, error) {
	f, err := os.Open(caminho) //nolint:gosec // caminho vem da linha de comando da ferramenta
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

	var saida []entrada
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for sc.Scan() {
		campos := strings.Split(sc.Text(), "\t")
		if len(campos) != 4 {
			return nil, fmt.Errorf("linha malformada: %q", sc.Text())
		}
		cp, err := strconv.ParseUint(campos[0], 16, 32)
		if err != nil {
			return nil, fmt.Errorf("codepoint inválido %q: %w", campos[0], err)
		}
		var mapeado strings.Builder
		if campos[3] != "" {
			for _, hex := range strings.Split(campos[3], ",") {
				v, err := strconv.ParseUint(hex, 16, 32)
				if err != nil {
					return nil, fmt.Errorf("mapeamento inválido %q: %w", hex, err)
				}
				mapeado.WriteRune(rune(v))
			}
		}
		saida = append(saida, entrada{
			runa:       rune(cp),
			alfanum:    campos[1] == "1",
			palavra:    campos[2] == "1",
			mapeamento: mapeado.String(),
		})
	}
	return saida, sc.Err()
}

// removerMarcasNFD é o que o Go faz nativamente: decompõe, descarta as marcas
// sem espaçamento e recompõe.
func removerMarcasNFD(s string) string {
	t := transform.Chain(norm.NFD, runes.Remove(runes.In(unicode.Mn)), norm.NFC)
	saida, _, err := transform.String(t, s)
	if err != nil {
		return s
	}
	return saida
}

func divergenciasDeDiacriticos(entradas []entrada) map[rune]string {
	div := map[rune]string{}
	for _, e := range entradas {
		nativo := removerMarcasNFD(string(e.runa))
		if nativo != e.mapeamento {
			div[e.runa] = e.mapeamento
		}
	}
	return div
}

func divergenciasAlfanumericas(entradas []entrada) map[rune]bool {
	div := map[rune]bool{}
	for _, e := range entradas {
		nativo := unicode.IsLetter(e.runa) || unicode.IsDigit(e.runa)
		if nativo != e.alfanum {
			div[e.runa] = e.alfanum
		}
	}
	return div
}

// aproximacaoDePalavra é a tentativa nativa em Go de reproduzir o `\w` Unicode
// do Rust: [\p{Alphabetic}\p{M}\p{Nd}\p{Pc}\p{Join_Control}].
//
// Alphabetic é L ∪ Nl ∪ Other_Alphabetic. O que sobrar de divergência vai para
// a tabela gerada.
func aproximacaoDePalavra(r rune) bool {
	return unicode.IsLetter(r) ||
		unicode.Is(unicode.Nl, r) ||
		unicode.Is(unicode.Other_Alphabetic, r) ||
		unicode.IsMark(r) ||
		unicode.Is(unicode.Nd, r) ||
		unicode.Is(unicode.Pc, r) ||
		unicode.Is(unicode.Join_Control, r)
}

func divergenciasDePalavra(entradas []entrada) map[rune]bool {
	div := map[rune]bool{}
	for _, e := range entradas {
		if aproximacaoDePalavra(e.runa) != e.palavra {
			div[e.runa] = e.palavra
		}
	}
	return div
}

func emitirPalavra(div map[rune]bool) error {
	var b strings.Builder
	b.WriteString(cabecalho)
	b.WriteString(`package pdftext

// excecoesDePalavra são as runas em que o ` + "`\\w`" + ` Unicode do crate ` + "`regex`" + `,
// usado na junção de hífens do legado, diverge da aproximação nativa em Go.
//
// O ` + "`\\w`" + ` do Rust é [\p{Alphabetic}\p{M}\p{Nd}\p{Pc}\p{Join_Control}]. Duas
// diferenças custaram divergência real, encontradas pelo teste de propriedade
// da fase F6 (INV-P02):
//
//   - INCLUI marcas combinantes: "6" seguido de U+0327 antes de "-\n" JUNTA no
//     legado;
//   - EXCLUI as categorias No e Nl: "¼" antes de "-\n" NÃO junta, embora
//     ` + "`\\p{N}`" + ` do Go as inclua.
//
// O valor é o que o RUST decide.
//
// Gerada por comparação runa a runa de todo o plano básico multilíngue,
// perguntando ao próprio motor de expressões regulares do Rust.
var excecoesDePalavra = map[rune]bool{
`)
	var runas []rune
	for r := range div {
		runas = append(runas, r)
	}
	sort.Slice(runas, func(i, j int) bool { return runas[i] < runas[j] })
	for _, r := range runas {
		b.WriteString(fmt.Sprintf("\t%s: %t, // %s\n", literalRuna(r), div[r], descrever(r)))
	}
	b.WriteString("}\n")
	return gravar(saidaPalavra, b.String())
}

const cabecalho = `// Code generated by tools/gerar-tabela-diacriticos. DO NOT EDIT.
//
// Regenerar:
//
//	cargo build --release --manifest-path tools/gerar-tabela-diacriticos/dump/Cargo.toml
//	tools/gerar-tabela-diacriticos/dump/target/release/dump-unicode > /tmp/unicode.tsv
//	go run ./tools/gerar-tabela-diacriticos /tmp/unicode.tsv
`

func emitirDiacriticos(div map[rune]string) error {
	var b strings.Builder
	b.WriteString(cabecalho)
	b.WriteString(`package pdftext

// excecoesDeDiacriticos são as runas em que a crate ` + "`diacritics`" + ` do legado
// diverge da decomposição canônica do Go.
//
// A decomposição resolve a acentuação do português — á, ç, ã, ê. Não resolve
// caracteres que não se decompõem, como ø, đ, ß, æ e ł, que a crate mapeia por
// tabela própria. Nomes estrangeiros aparecem em diários oficiais, então a
// diferença é observável. Ver docs/INVARIANTES.md, INV-P07.
//
// Gerada por comparação runa a runa de todo o plano básico multilíngue.
var excecoesDeDiacriticos = map[rune]string{
`)
	var runas []rune
	for r := range div {
		runas = append(runas, r)
	}
	sort.Slice(runas, func(i, j int) bool { return runas[i] < runas[j] })
	for _, r := range runas {
		b.WriteString(fmt.Sprintf("\t%s: %s, // %s\n",
			literalRuna(r), strconv.Quote(div[r]), descrever(r)))
	}
	b.WriteString("}\n")
	return gravar(saidaDiacriticos, b.String())
}

func emitirAlfanumerico(div map[rune]bool) error {
	var b strings.Builder
	b.WriteString(cabecalho)
	b.WriteString(`package searchidx

// excecoesAlfanumericas são as runas em que ` + "`char::is_alphanumeric`" + ` do Rust,
// que o SimpleTokenizer do Tantivy usa para quebrar termos, diverge de
// ` + "`unicode.IsLetter(r) || unicode.IsDigit(r)`" + ` do Go.
//
// A classificação do Rust é Alphabetic ∪ {Nd, Nl, No}; a do Go é L ∪ Nd. A
// diferença aparece em expoentes e frações — ² ³ ½ — e em numerais de letra,
// que passam a fazer parte do termo em vez de separá-lo. Ver
// docs/INVARIANTES.md, INV-P22.
//
// O valor é o que o RUST decide.
//
// Gerada por comparação runa a runa de todo o plano básico multilíngue.
var excecoesAlfanumericas = map[rune]bool{
`)
	var runas []rune
	for r := range div {
		runas = append(runas, r)
	}
	sort.Slice(runas, func(i, j int) bool { return runas[i] < runas[j] })
	for _, r := range runas {
		b.WriteString(fmt.Sprintf("\t%s: %t, // %s\n", literalRuna(r), div[r], descrever(r)))
	}
	b.WriteString("}\n")
	return gravar(saidaAlfanum, b.String())
}

// gravar formata o código antes de escrever.
//
// Sem isto a saída do gerador é válida mas NÃO é canônica: o gofmt alinha os
// comentários das entradas do mapa em colunas, e a largura das colunas depende
// do conteúdo. O arquivo gravado divergiria do arquivo formatado, e `gofmt -l`
// acusaria a diferença logo após um `go generate` — exatamente o que aconteceu
// na fase F7.
func gravar(caminho, codigo string) error {
	formatado, err := format.Source([]byte(codigo))
	if err != nil {
		return fmt.Errorf("formatando %s: %w", caminho, err)
	}
	return os.WriteFile(caminho, formatado, 0o600)
}

func literalRuna(r rune) string {
	return fmt.Sprintf("0x%04X", r)
}

// descrever produz um comentário legível: o caractere quando imprimível, e a
// categoria Unicode.
func descrever(r rune) string {
	categoria := "?"
	for nome, tabela := range unicode.Categories {
		if len(nome) == 2 && unicode.Is(tabela, r) {
			categoria = nome
			break
		}
	}
	if unicode.IsPrint(r) && !unicode.IsSpace(r) {
		return fmt.Sprintf("%q %s", r, categoria)
	}
	return fmt.Sprintf("U+%04X %s", r, categoria)
}
