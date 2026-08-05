// Comando avaliar-extracao compara ligações Go para MuPDF contra o corpus
// dourado, byte a byte.
//
// É o artefato do passo 1 do procedimento da fase F5, que exige a AVALIAÇÃO
// antes de qualquer implementação. O relatório resultante está em
// docs/F5-AVALIACAO-EXTRACAO.md.
//
// O oráculo é test/testdata/expected/*.paginas-brutas.json — o texto ANTES da
// normalização, capturado na fase F0. A normalização é a fase F6; misturá-las
// tornaria uma falha de extração indistinguível de uma de normalização.
//
//	go run ./tools/avaliar-extracao [dir-corpus] [dir-esperado]
package main

import (
	"encoding/json"
	"fmt"
	"html"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/gen2brain/go-fitz"
)

// Estratégias avaliadas.
//
//	texto  doc.Text(n) — a saída pronta do MuPDF
//	html   doc.HTML(n) — um <p> por LINHA, remontado como o laço do legado
type estrategia struct {
	nome    string
	extrair func(doc *fitz.Document, pagina int) (string, error)
}

var (
	padraoParagrafo = regexp.MustCompile(`(?s)<p[^>]*>(.*?)</p>`)
	padraoTag       = regexp.MustCompile(`<[^>]*>`)
)

// linhasDoHTML remonta o texto da página a partir da saída HTML, reproduzindo
// o laço de reference/main.rs:492-505: cada linha é aparada, recebe um \n, e
// as linhas são concatenadas sem separador entre blocos.
func linhasDoHTML(doc *fitz.Document, pagina int) (string, error) {
	bruto, err := doc.HTML(pagina, false)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	for _, m := range padraoParagrafo.FindAllStringSubmatch(bruto, -1) {
		linha := html.UnescapeString(padraoTag.ReplaceAllString(m[1], ""))
		b.WriteString(strings.TrimSpace(linha))
		b.WriteString("\n")
	}
	return b.String(), nil
}

var estrategias = []estrategia{
	{"texto", func(d *fitz.Document, p int) (string, error) { return d.Text(p) }},
	{"html", linhasDoHTML},
}

type esperado struct {
	Documento    string   `json:"documento"`
	TotalPaginas int      `json:"total_paginas"`
	Paginas      []string `json:"paginas"`
}

type resultado struct {
	estrategia    estrategia
	documento     string
	paginasOK     int
	paginasTotal  int
	erro          error
	divergencias  []divergencia
	classesDeErro map[string]int
}

type divergencia struct {
	pagina   int
	posicao  int
	obtido   string
	esperado string
	classe   string
}

func main() {
	dirCorpus := "test/testdata/corpus"
	dirEsperado := "test/testdata/expected"
	if len(os.Args) > 1 {
		dirCorpus = os.Args[1]
	}
	if len(os.Args) > 2 {
		dirEsperado = os.Args[2]
	}

	arquivos, err := filepath.Glob(filepath.Join(dirEsperado, "*.paginas-brutas.json"))
	if err != nil || len(arquivos) == 0 {
		fmt.Fprintf(os.Stderr, "nenhum oráculo em %s — rode tools/capturar-corpus antes\n", dirEsperado)
		os.Exit(1)
	}
	sort.Strings(arquivos)

	for _, e := range estrategias {
		avaliarEstrategia(e, dirCorpus, arquivos)
	}
}

func avaliarEstrategia(e estrategia, dirCorpus string, arquivos []string) {
	var resultados []resultado
	classesGlobais := map[string]int{}

	for _, arquivo := range arquivos {
		var esp esperado
		bruto, err := os.ReadFile(arquivo)
		if err != nil {
			fmt.Fprintf(os.Stderr, "lendo %s: %v\n", arquivo, err)
			continue
		}
		if err := json.Unmarshal(bruto, &esp); err != nil {
			fmt.Fprintf(os.Stderr, "analisando %s: %v\n", arquivo, err)
			continue
		}

		r := avaliar(e, filepath.Join(dirCorpus, esp.Documento+".pdf"), esp)
		for classe, n := range r.classesDeErro {
			classesGlobais[classe] += n
		}
		resultados = append(resultados, r)
	}

	relatar(e, resultados, classesGlobais)
}

func avaliar(e estrategia, caminhoPDF string, esp esperado) resultado {
	r := resultado{
		estrategia:    e,
		documento:     esp.Documento,
		paginasTotal:  esp.TotalPaginas,
		classesDeErro: map[string]int{},
	}

	doc, err := fitz.New(caminhoPDF)
	if err != nil {
		r.erro = err
		return r
	}
	defer func() { _ = doc.Close() }()

	if doc.NumPage() != esp.TotalPaginas {
		r.erro = fmt.Errorf("contagem de páginas: go-fitz %d, legado %d",
			doc.NumPage(), esp.TotalPaginas)
		return r
	}

	for i := 0; i < esp.TotalPaginas; i++ {
		obtido, err := r.estrategia.extrair(doc, i)
		if err != nil {
			r.erro = fmt.Errorf("página %d: %w", i+1, err)
			return r
		}
		alvo := esp.Paginas[i]

		if obtido == alvo {
			r.paginasOK++
			continue
		}

		pos := primeiraDivergencia(obtido, alvo)
		classe := classificar(obtido, alvo, pos)
		r.classesDeErro[classe]++

		// Guarda no máximo três divergências por documento: o suficiente para
		// diagnosticar, sem inundar o relatório.
		if len(r.divergencias) < 3 {
			r.divergencias = append(r.divergencias, divergencia{
				pagina:   i + 1,
				posicao:  pos,
				obtido:   contexto(obtido, pos),
				esperado: contexto(alvo, pos),
				classe:   classe,
			})
		}
	}
	return r
}

func primeiraDivergencia(a, b string) int {
	n := min(len(a), len(b))
	for i := range n {
		if a[i] != b[i] {
			return i
		}
	}
	return n
}

// contexto devolve 80 bytes de cada lado da posição, com caracteres de
// controle escapados para que espaço e quebra de linha fiquem visíveis.
func contexto(s string, pos int) string {
	inicio := max(0, pos-80)
	fim := min(len(s), pos+80)
	antes := escapar(s[inicio:pos])
	depois := escapar(s[pos:fim])
	return antes + "◆" + depois
}

func escapar(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch r {
		case '\n':
			b.WriteString("\\n")
		case '\t':
			b.WriteString("\\t")
		case '\r':
			b.WriteString("\\r")
		case ' ':
			b.WriteString("·")
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// classificar tenta nomear a causa raiz da divergência.
func classificar(obtido, alvo string, pos int) string {
	byteObtido := byte(0)
	byteAlvo := byte(0)
	if pos < len(obtido) {
		byteObtido = obtido[pos]
	}
	if pos < len(alvo) {
		byteAlvo = alvo[pos]
	}

	semEspacos := func(s string) string {
		return strings.Join(strings.FieldsFunc(s, func(r rune) bool {
			return r == ' ' || r == '\n' || r == '\t'
		}), "")
	}

	switch {
	case pos >= len(alvo):
		return "sobra no fim (go-fitz produz mais texto)"
	case pos >= len(obtido):
		return "falta no fim (go-fitz produz menos texto)"
	case semEspacos(obtido) == semEspacos(alvo):
		return "apenas espaçamento e quebras de linha"
	case byteObtido == '\n' || byteAlvo == '\n':
		return "segmentação de linhas"
	case byteObtido == ' ' || byteAlvo == ' ':
		return "espaço em branco"
	default:
		return "conteúdo de caractere"
	}
}

func relatar(e estrategia, resultados []resultado, classes map[string]int) {
	docsOK, docsDiv, docsErro := 0, 0, 0
	pagsOK, pagsTotal := 0, 0

	fmt.Println("================================================================")
	fmt.Printf(" AVALIAÇÃO DA EXTRAÇÃO — estratégia %q contra o corpus dourado\n", e.nome)
	fmt.Println("================================================================")
	fmt.Println()
	fmt.Printf("%-42s %-10s %s\n", "DOCUMENTO", "PÁGINAS", "RESULTADO")
	fmt.Println(strings.Repeat("-", 78))

	for _, r := range resultados {
		pagsOK += r.paginasOK
		pagsTotal += r.paginasTotal

		switch {
		case r.erro != nil:
			docsErro++
			fmt.Printf("%-42s %-10s ERRO: %v\n", r.documento, "-", r.erro)
		case r.paginasOK == r.paginasTotal:
			docsOK++
			fmt.Printf("%-42s %3d/%-6d IDÊNTICO\n", r.documento, r.paginasOK, r.paginasTotal)
		default:
			docsDiv++
			fmt.Printf("%-42s %3d/%-6d DIVERGE\n", r.documento, r.paginasOK, r.paginasTotal)
		}
	}

	fmt.Println(strings.Repeat("-", 78))
	fmt.Printf("documentos idênticos byte a byte : %d de %d\n", docsOK, len(resultados))
	fmt.Printf("documentos divergentes           : %d\n", docsDiv)
	fmt.Printf("documentos com erro              : %d\n", docsErro)
	fmt.Printf("páginas idênticas byte a byte    : %d de %d\n", pagsOK, pagsTotal)
	fmt.Println()

	if len(classes) > 0 {
		fmt.Println("CLASSES DE DIVERGÊNCIA")
		fmt.Println(strings.Repeat("-", 78))
		var nomes []string
		for c := range classes {
			nomes = append(nomes, c)
		}
		sort.Slice(nomes, func(i, j int) bool { return classes[nomes[i]] > classes[nomes[j]] })
		for _, c := range nomes {
			fmt.Printf("  %4d páginas  %s\n", classes[c], c)
		}
		fmt.Println()
	}

	fmt.Println("PRIMEIRA DIVERGÊNCIA POR DOCUMENTO (80 bytes de contexto, ◆ marca a posição)")
	fmt.Println(strings.Repeat("-", 78))
	mostrados := 0
	for _, r := range resultados {
		if len(r.divergencias) == 0 || mostrados >= 4 {
			continue
		}
		mostrados++
		d := r.divergencias[0]
		fmt.Printf("\n%s — página %d, byte %d\n", r.documento, d.pagina, d.posicao)
		fmt.Printf("  classe   : %s\n", d.classe)
		fmt.Printf("  go-fitz  : %s\n", d.obtido)
		fmt.Printf("  legado   : %s\n", d.esperado)
	}
	fmt.Println()
}
