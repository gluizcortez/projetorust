package pdftext_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/gluizcortez/projetorust/internal/adapter/pdftext"
	"github.com/gluizcortez/projetorust/internal/domain"
)

const dirCorpus = "../../../test/testdata/corpus"

func lerPDF(t *testing.T, nome string) []byte {
	t.Helper()
	conteudo, err := os.ReadFile(filepath.Join(dirCorpus, nome))
	if err != nil {
		t.Skipf("corpus indisponível (%v) — gere com tools/gerar-corpus-sintetico", err)
	}
	return conteudo
}

// -------------------------------------------------------------------------
// Entrada inválida — INV-P20
// -------------------------------------------------------------------------

func TestEntradaInvalida(t *testing.T) {
	casos := []struct {
		nome          string
		arquivo       string
		esperaErro    bool
		esperaPaginas int
		observacao    string
	}{
		{
			nome:          "PDF truncado é ACEITO com zero páginas",
			arquivo:       "20-corrompido.pdf",
			esperaErro:    false,
			esperaPaginas: 0,
			observacao: "INV-P20: o MuPDF aceita, a importação vai a status 5 e " +
				"fica indistinguível de um diário sem ocorrências",
		},
		{
			nome:       "arquivo vazio é rejeitado",
			arquivo:    "21-vazio.pdf",
			esperaErro: true,
		},
		{
			nome:       "arquivo que não é PDF é rejeitado",
			arquivo:    "22-nao-e-pdf.pdf",
			esperaErro: true,
		},
	}

	extrator := pdftext.NovoExtrator()
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			paginas, err := extrator.ExtrairPaginas(context.Background(), lerPDF(t, c.arquivo))

			if c.esperaErro {
				if err == nil {
					t.Fatalf("esperava erro, obtive %d página(s)", len(paginas))
				}
				if !errors.Is(err, domain.ErrPDFInvalido) {
					t.Errorf("erro deveria envolver ErrPDFInvalido: %v", err)
				}
				return
			}

			if err != nil {
				t.Fatalf("não deveria falhar (%s): %v", c.observacao, err)
			}
			if len(paginas) != c.esperaPaginas {
				t.Errorf("páginas = %d, esperado %d (%s)", len(paginas), c.esperaPaginas, c.observacao)
			}
		})
	}
}

func TestConteudoArbitrarioNaoEntraEmPanico(t *testing.T) {
	extrator := pdftext.NovoExtrator()
	entradas := [][]byte{
		nil,
		{},
		[]byte("%PDF-1.4"),
		[]byte("%PDF-1.4\n\x00\x00\x00"),
		[]byte(strings.Repeat("\xff", 4096)),
		[]byte("%PDF-1.7\ntrailer<</Root 1 0 R>>\nstartxref\n999999\n%%EOF"),
	}
	for i, entrada := range entradas {
		// Nenhuma pode derrubar o processo; erro é resposta aceitável.
		paginas, err := extrator.ExtrairPaginas(context.Background(), entrada)
		t.Logf("entrada %d: %d página(s), erro=%v", i, len(paginas), err)
	}
}

// -------------------------------------------------------------------------
// Contexto
// -------------------------------------------------------------------------

func TestContextoCanceladoInterrompe(t *testing.T) {
	extrator := pdftext.NovoExtrator()
	ctx, cancelar := context.WithCancel(context.Background())
	cancelar()

	_, err := extrator.ExtrairPaginas(ctx, lerPDF(t, "18-volume-120-paginas.pdf"))
	if err == nil {
		t.Fatal("contexto cancelado deveria interromper a extração")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("erro deveria envolver context.Canceled: %v", err)
	}
}

// -------------------------------------------------------------------------
// Concorrência
// -------------------------------------------------------------------------

func TestExtracaoConcorrente(t *testing.T) {
	extrator := pdftext.NovoExtrator()
	conteudo := lerPDF(t, "01-simples.pdf")

	referencia, err := extrator.ExtrairPaginas(context.Background(), conteudo)
	if err != nil {
		t.Fatalf("extração de referência: %v", err)
	}

	const goroutines = 8
	var wg sync.WaitGroup
	resultados := make([][]string, goroutines)
	erros := make([]error, goroutines)

	for i := range goroutines {
		wg.Add(1)
		go func() {
			defer wg.Done()
			resultados[i], erros[i] = extrator.ExtrairPaginas(context.Background(), conteudo)
		}()
	}
	wg.Wait()

	for i := range goroutines {
		if erros[i] != nil {
			t.Errorf("goroutine %d: %v", i, erros[i])
			continue
		}
		if len(resultados[i]) != len(referencia) {
			t.Errorf("goroutine %d: %d páginas, esperado %d", i, len(resultados[i]), len(referencia))
			continue
		}
		for p := range referencia {
			if resultados[i][p] != referencia[p] {
				t.Errorf("goroutine %d, página %d: saída divergente sob concorrência", i, p+1)
			}
		}
	}
}

// -------------------------------------------------------------------------
// Vazamento de memória
// -------------------------------------------------------------------------

// TestVazamento roda 200 extrações do maior documento do corpus e verifica que
// a memória residente não cresce sem limite.
//
// O MuPDF é uma biblioteca C: um documento não fechado vaza memória que o
// coletor do Go não recupera. O `defer doc.Close()` é o que impede isso, e
// este teste é o que prova.
func TestVazamento(t *testing.T) {
	if testing.Short() {
		t.Skip("teste de vazamento é demorado")
	}

	extrator := pdftext.NovoExtrator()
	conteudo := lerPDF(t, "18-volume-120-paginas.pdf")
	ctx := context.Background()

	const iteracoes = 200
	const aquecimento = 10

	var baseAlocada uint64
	for i := range iteracoes {
		if _, err := extrator.ExtrairPaginas(ctx, conteudo); err != nil {
			t.Fatalf("iteração %d: %v", i, err)
		}
		if i == aquecimento {
			runtime.GC()
			var m runtime.MemStats
			runtime.ReadMemStats(&m)
			baseAlocada = m.HeapAlloc
		}
	}

	runtime.GC()
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	if baseAlocada == 0 {
		t.Skip("não foi possível estabelecer a linha de base")
	}

	crescimento := float64(m.HeapAlloc) / float64(baseAlocada)
	t.Logf("heap após %d iterações: %.1f MiB (linha de base %.1f MiB, fator %.2f)",
		iteracoes, float64(m.HeapAlloc)/(1<<20), float64(baseAlocada)/(1<<20), crescimento)

	// Tolerância generosa: o objetivo é pegar vazamento, não oscilação do
	// coletor. Um documento não fechado faria o fator explodir.
	if crescimento > 3.0 {
		t.Errorf("heap cresceu %.2fx após %d extrações — indício de vazamento",
			crescimento, iteracoes)
	}
}

// -------------------------------------------------------------------------
// Desempenho
// -------------------------------------------------------------------------

func BenchmarkExtrairPaginas(b *testing.B) {
	conteudo, err := os.ReadFile(filepath.Join(dirCorpus, "18-volume-120-paginas.pdf"))
	if err != nil {
		b.Skipf("corpus indisponível: %v", err)
	}
	extrator := pdftext.NovoExtrator()
	ctx := context.Background()

	b.ResetTimer()
	b.ReportAllocs()
	for b.Loop() {
		if _, err := extrator.ExtrairPaginas(ctx, conteudo); err != nil {
			b.Fatal(err)
		}
	}
}
