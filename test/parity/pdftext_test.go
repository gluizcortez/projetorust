package parity_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/gluizcortez/projetorust/internal/adapter/pdftext"
)

const (
	dirCorpus   = "../testdata/corpus"
	dirEsperado = "../testdata/expected"
)

type paginasEsperadas struct {
	Documento    string   `json:"documento"`
	TotalPaginas int      `json:"total_paginas"`
	Estagio      string   `json:"estagio"`
	Paginas      []string `json:"paginas"`
}

func carregarOraculos(t *testing.T, sufixo string) []paginasEsperadas {
	t.Helper()
	arquivos, err := filepath.Glob(filepath.Join(dirEsperado, "*"+sufixo))
	if err != nil {
		t.Fatalf("procurando oráculos: %v", err)
	}
	if len(arquivos) == 0 {
		t.Skipf("nenhum oráculo %s em %s — gere o corpus com "+
			"`python3 tools/gerar-corpus-sintetico/gerar.py test/testdata/corpus` e "+
			"`tools/capturar-corpus/target/release/capturar-corpus test/testdata/corpus test/testdata/expected`",
			sufixo, dirEsperado)
	}
	sort.Strings(arquivos)

	var saida []paginasEsperadas
	for _, a := range arquivos {
		bruto, err := os.ReadFile(a)
		if err != nil {
			t.Fatalf("lendo %s: %v", a, err)
		}
		var p paginasEsperadas
		if err := json.Unmarshal(bruto, &p); err != nil {
			t.Fatalf("analisando %s: %v", a, err)
		}
		saida = append(saida, p)
	}
	return saida
}

// TestExtracaoTexto é o critério de aceite central da fase F5.
//
// Compara o texto BRUTO — antes da normalização, que é a fase F6 — com o
// capturado do serviço legado, byte a byte, para todo o corpus.
func TestExtracaoTexto(t *testing.T) {
	extrator := pdftext.NovoExtrator()
	ctx := context.Background()

	var paginasOK, paginasTotal int

	for _, esp := range carregarOraculos(t, ".paginas-brutas.json") {
		t.Run(esp.Documento, func(t *testing.T) {
			conteudo, err := os.ReadFile(filepath.Join(dirCorpus, esp.Documento+".pdf"))
			if err != nil {
				t.Fatalf("lendo o PDF: %v", err)
			}

			obtidas, err := extrator.ExtrairPaginas(ctx, conteudo)
			if err != nil {
				t.Fatalf("ExtrairPaginas: %v", err)
			}

			paginasTotal += esp.TotalPaginas
			if len(obtidas) != esp.TotalPaginas {
				t.Fatalf("páginas extraídas = %d, legado = %d", len(obtidas), esp.TotalPaginas)
			}

			for i := range obtidas {
				if obtidas[i] == esp.Paginas[i] {
					paginasOK++
					continue
				}
				pos := primeiraDivergencia(obtidas[i], esp.Paginas[i])
				t.Errorf(
					"página %d DIVERGE do legado no byte %d\n"+
						"  obtido : %s\n"+
						"  legado : %s",
					i+1, pos,
					contexto(obtidas[i], pos), contexto(esp.Paginas[i], pos),
				)
			}
		})
	}

	t.Logf("paridade de extração: %d de %d páginas idênticas byte a byte", paginasOK, paginasTotal)
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

func contexto(s string, pos int) string {
	inicio := max(0, pos-80)
	fim := min(len(s), pos+80)
	var b strings.Builder
	for i, r := range s[inicio:fim] {
		if inicio+i == pos {
			b.WriteString("◆")
		}
		switch r {
		case '\n':
			b.WriteString("\\n")
		case '\t':
			b.WriteString("\\t")
		case ' ':
			b.WriteString("·")
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}
