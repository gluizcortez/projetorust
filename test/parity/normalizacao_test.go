package parity_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/gluizcortez/projetorust/internal/adapter/pdftext"
	"github.com/gluizcortez/projetorust/internal/adapter/searchidx"
)

// TestNormalizacao compara o texto NORMALIZADO com o capturado do legado,
// byte a byte, para todo o corpus.
//
// A entrada é o texto BRUTO do mesmo oráculo, não o extraído pelo Go: isola a
// normalização da extração, que já tem seu próprio teste (fase F5).
func TestNormalizacao(t *testing.T) {
	brutos := indexarPorDocumento(carregarOraculos(t, ".paginas-brutas.json"))
	normalizados := carregarOraculos(t, ".paginas.json")

	var paginasOK, paginasTotal int

	for _, esp := range normalizados {
		t.Run(esp.Documento, func(t *testing.T) {
			bruto, ok := brutos[esp.Documento]
			if !ok {
				t.Fatalf("sem oráculo bruto para %s", esp.Documento)
			}
			if len(bruto.Paginas) != len(esp.Paginas) {
				t.Fatalf("páginas: bruto %d, normalizado %d", len(bruto.Paginas), len(esp.Paginas))
			}

			paginasTotal += len(esp.Paginas)
			for i := range esp.Paginas {
				obtido := pdftext.Normalizar(bruto.Paginas[i])
				if obtido == esp.Paginas[i] {
					paginasOK++
					continue
				}
				pos := primeiraDivergencia(obtido, esp.Paginas[i])
				t.Errorf(
					"página %d DIVERGE do legado no byte %d\n  obtido : %s\n  legado : %s",
					i+1, pos, contexto(obtido, pos), contexto(esp.Paginas[i], pos))
			}
		})
	}

	t.Logf("paridade de normalização: %d de %d páginas idênticas byte a byte", paginasOK, paginasTotal)
}

// TestTokenizacao compara os termos do índice com os capturados do legado.
func TestTokenizacao(t *testing.T) {
	normalizados := indexarPorDocumento(carregarOraculos(t, ".paginas.json"))

	arquivos, err := filepath.Glob(filepath.Join(dirEsperado, "*.tokens.json"))
	if err != nil || len(arquivos) == 0 {
		t.Skip("nenhum oráculo de termos — gere o corpus")
	}

	var paginasOK, paginasTotal, termosTotal int

	for _, arquivo := range arquivos {
		esp := carregarTokens(t, arquivo)
		t.Run(esp.Documento, func(t *testing.T) {
			pags, ok := normalizados[esp.Documento]
			if !ok {
				t.Fatalf("sem oráculo normalizado para %s", esp.Documento)
			}
			if len(pags.Paginas) != len(esp.TermosPorPagina) {
				t.Fatalf("páginas: normalizado %d, termos %d",
					len(pags.Paginas), len(esp.TermosPorPagina))
			}

			paginasTotal += len(esp.TermosPorPagina)
			for i, esperados := range esp.TermosPorPagina {
				obtidos := searchidx.Tokenizar(pags.Paginas[i])
				termosTotal += len(esperados)

				if len(obtidos) != len(esperados) {
					t.Errorf("página %d: %d termos, legado %d\n  primeiros obtidos : %v\n  primeiros legado  : %v",
						i+1, len(obtidos), len(esperados),
						primeiros(obtidos, 8), primeiros(esperados, 8))
					continue
				}
				divergiu := false
				for j := range esperados {
					if obtidos[j] != esperados[j] {
						t.Errorf("página %d, termo %d: obtido %q, legado %q",
							i+1, j, obtidos[j], esperados[j])
						divergiu = true
						break
					}
				}
				if !divergiu {
					paginasOK++
				}
			}
		})
	}

	t.Logf("paridade de tokenização: %d de %d páginas idênticas (%d termos conferidos)",
		paginasOK, paginasTotal, termosTotal)
}

func primeiros(s []string, n int) []string {
	if len(s) < n {
		return s
	}
	return s[:n]
}

func indexarPorDocumento(esperados []paginasEsperadas) map[string]paginasEsperadas {
	m := make(map[string]paginasEsperadas, len(esperados))
	for _, e := range esperados {
		m[e.Documento] = e
	}
	return m
}

type tokensEsperados struct {
	Documento       string     `json:"documento"`
	TotalPaginas    int        `json:"total_paginas"`
	TermosPorPagina [][]string `json:"termos_por_pagina"`
}

func carregarTokens(t *testing.T, arquivo string) tokensEsperados {
	t.Helper()
	bruto, err := os.ReadFile(arquivo)
	if err != nil {
		t.Fatalf("lendo %s: %v", arquivo, err)
	}
	var esp tokensEsperados
	if err := jsonUnmarshal(bruto, &esp); err != nil {
		t.Fatalf("analisando %s: %v", arquivo, err)
	}
	return esp
}
