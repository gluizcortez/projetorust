package parity_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/gluizcortez/projetorust/internal/adapter/searchidx"
	"github.com/gluizcortez/projetorust/internal/domain"
)

// buscaEsperada é uma linha de `<documento>.busca.json`: o que `recortar`
// devolveu para UMA expressão, antes de qualquer deduplicação.
type buscaEsperada struct {
	Expressao string `json:"expressao"`
	// Desfecho é "ok", "erro" ou "panico". Ver tools/capturar-corpus/src/modelo.rs.
	Desfecho      string   `json:"desfecho"`
	DetalheDoErro string   `json:"detalhe_do_erro"`
	Paginas       []uint64 `json:"paginas"`
	TextoSHA256   []string `json:"texto_sha256"`
}

type buscasEsperadas struct {
	Documento     string          `json:"documento"`
	TotalPaginas  int             `json:"total_paginas"`
	PaginasSHA256 []string        `json:"paginas_sha256"`
	Buscas        []buscaEsperada `json:"buscas"`
}

func carregarBuscas(t *testing.T) []buscasEsperadas {
	t.Helper()
	arquivos, err := filepath.Glob(filepath.Join(dirEsperado, "*.busca.json"))
	if err != nil {
		t.Fatalf("procurando oráculos de busca: %v", err)
	}
	if len(arquivos) == 0 {
		t.Skipf("nenhum oráculo .busca.json em %s — gere com "+
			"`tools/capturar-corpus/target/release/capturar-corpus "+
			"test/testdata/corpus test/testdata/expected`", dirEsperado)
	}
	sort.Strings(arquivos)

	saida := make([]buscasEsperadas, 0, len(arquivos))
	for _, a := range arquivos {
		bruto, err := os.ReadFile(a) //nolint:gosec // caminho vem do glob sobre o diretório de testes
		if err != nil {
			t.Fatalf("lendo %s: %v", a, err)
		}
		var b buscasEsperadas
		if err := jsonUnmarshal(bruto, &b); err != nil {
			t.Fatalf("analisando %s: %v", a, err)
		}
		saida = append(saida, b)
	}
	return saida
}

// paginasDoDocumento devolve o texto normalizado de cada página, vindo do
// oráculo `<documento>.paginas.json` da fase F6.
//
// O índice é construído a partir do texto CAPTURADO DO LEGADO, não do texto que
// o extrator Go produz. É deliberado: F5 e F6 já provaram que os dois coincidem
// nas 159 páginas do corpus, e usar o oráculo isola o que F7 mede. Uma
// regressão na extração falha nos testes de F5, não aqui.
func paginasDoDocumento(t *testing.T, documento string) []string {
	t.Helper()
	caminho := filepath.Join(dirEsperado, documento+".paginas.json")
	bruto, err := os.ReadFile(caminho) //nolint:gosec // caminho derivado do oráculo
	if err != nil {
		t.Fatalf("lendo %s: %v", caminho, err)
	}
	var p paginasEsperadas
	if err := jsonUnmarshal(bruto, &p); err != nil {
		t.Fatalf("analisando %s: %v", caminho, err)
	}
	return p.Paginas
}

// TestBuscaFrase é o critério de aceite central da fase F7: para toda
// combinação documento × expressão, o CONJUNTO de páginas devolvido pelo índice
// posicional em Go é idêntico ao que o Tantivy devolveu.
func TestBuscaFrase(t *testing.T) {
	esperados := carregarBuscas(t)
	ctx := context.Background()
	indexador := searchidx.NovoIndexador()

	var (
		comparacoes int
		divergentes int
		porDesfecho = map[string]int{}
	)

	for _, doc := range esperados {
		paginas := paginasDoDocumento(t, doc.Documento)

		// Confirma que o Go está indexando exatamente o texto que o legado
		// indexou. Sem isto, uma divergência de busca poderia ser, na verdade,
		// uma divergência de entrada.
		if len(paginas) != doc.TotalPaginas {
			t.Fatalf("%s: %d páginas no oráculo de texto, %d no de busca",
				doc.Documento, len(paginas), doc.TotalPaginas)
		}
		for i, p := range paginas {
			soma := sha256.Sum256([]byte(p))
			if obtido := hex.EncodeToString(soma[:]); obtido != doc.PaginasSHA256[i] {
				t.Fatalf("%s página %d: o texto de entrada diverge entre os oráculos",
					doc.Documento, i+1)
			}
		}

		indice, err := indexador.Construir(ctx, paginas)
		if err != nil {
			t.Fatalf("%s: construindo o índice: %v", doc.Documento, err)
		}

		for _, esperada := range doc.Buscas {
			comparacoes++
			porDesfecho[esperada.Desfecho]++

			recortes, err := indice.Frase(ctx, esperada.Expressao)

			if !conferirBusca(t, doc.Documento, esperada, recortes, err) {
				divergentes++
			}
		}

		if err := indice.Fechar(); err != nil {
			t.Errorf("%s: Fechar: %v", doc.Documento, err)
		}
	}

	t.Logf("%d comparações em %d documentos: %d ok, %d erro, %d pânico no legado",
		comparacoes, len(esperados),
		porDesfecho["ok"], porDesfecho["erro"], porDesfecho["panico"])
	if divergentes > 0 {
		t.Errorf("%d combinações divergentes de %d", divergentes, comparacoes)
	}
}

// conferirBusca compara UM resultado com o oráculo e devolve se houve acordo.
//
// Os três desfechos do legado recebem tratamentos diferentes, e a diferença é o
// assunto de D-19:
//
//	ok     → o Go tem de devolver as MESMAS páginas, sem erro
//	erro   → aspas na expressão; o QueryParser do Tantivy falha e o legado
//	         encerra a importação. A busca em Go NÃO tem analisador de consulta:
//	         a expressão é apenas tokenizada, então não há o que falhar. O teste
//	         registra a divergência estrutural em vez de fingir que não existe.
//	panico → expressão com `&` e sintaxe de expressão regular inválida. O Go
//	         devolve ErrExpressaoInvalida.
func conferirBusca(
	t *testing.T,
	documento string,
	esperada buscaEsperada,
	recortes []domain.Recorte,
	err error,
) bool {
	t.Helper()

	switch esperada.Desfecho {
	case "panico":
		if !errors.Is(err, domain.ErrExpressaoInvalida) {
			t.Errorf("%s / %q: o legado entra em pânico aqui; o Go devolveu err=%v com %d recorte(s)",
				documento, esperada.Expressao, err, len(recortes))
			return false
		}
		return true

	case "erro":
		// INV-P17. Ver o comentário de TestINVP17AspasNaoAbortamABusca: a
		// divergência é conhecida, deliberada e verificada em teste próprio.
		return true

	case "ok":
		if err != nil {
			t.Errorf("%s / %q: o legado devolveu %d página(s); o Go devolveu erro: %v",
				documento, esperada.Expressao, len(esperada.Paginas), err)
			return false
		}
		obtidas := make([]uint64, 0, len(recortes))
		for _, r := range recortes {
			obtidas = append(obtidas, r.Pagina)
		}
		if !mesmasPaginas(obtidas, esperada.Paginas) {
			t.Errorf("%s / %q: conjunto de páginas diverge\n  Go     = %v\n  legado = %v",
				documento, esperada.Expressao, obtidas, esperada.Paginas)
			return false
		}
		// O recorte carrega o texto INTEGRAL da página, e Texto e Destaque são
		// idênticos por construção (ESPECIFICACAO §5.4).
		for i, r := range recortes {
			soma := sha256.Sum256([]byte(r.Destaque))
			if obtido := hex.EncodeToString(soma[:]); obtido != esperada.TextoSHA256[i] {
				t.Errorf("%s / %q página %d: o texto do recorte diverge do legado",
					documento, esperada.Expressao, r.Pagina)
				return false
			}
			if r.Texto != r.Destaque {
				t.Errorf("%s / %q página %d: Texto e Destaque deveriam ser idênticos",
					documento, esperada.Expressao, r.Pagina)
				return false
			}
		}
		return true

	default:
		t.Fatalf("%s / %q: desfecho desconhecido %q no oráculo",
			documento, esperada.Expressao, esperada.Desfecho)
		return false
	}
}

func mesmasPaginas(a, b []uint64) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestINVP17AspasNaoAbortamABusca documenta e fixa a única divergência
// ESTRUTURAL desta fase.
//
// No legado a consulta é montada por interpolação sem escape,
// `format!(r#""{key}""#)` (main.rs:375), então uma aspa dupla na expressão
// desbalanceia a consulta e o QueryParser devolve erro — que aborta a
// importação inteira (INV-P17).
//
// O índice em Go não tem analisador de consulta: a expressão é tokenizada pelo
// mesmo caminho do texto, e a aspa é apenas mais um separador. Não existe erro a
// devolver. A decisão de reproduzir ou não o aborto pertence ao caso de uso e
// está registrada em D-19; aqui o teste apenas prova que a busca em si é bem
// definida e devolve o que a frase pede.
func TestINVP17AspasNaoAbortamABusca(t *testing.T) {
	esperados := carregarBuscas(t)
	ctx := context.Background()
	indexador := searchidx.NovoIndexador()

	var (
		total          int
		bemDefinidas   int
		filtroInvalido int
	)
	for _, doc := range esperados {
		indice, err := indexador.Construir(ctx, paginasDoDocumento(t, doc.Documento))
		if err != nil {
			t.Fatalf("%s: construindo o índice: %v", doc.Documento, err)
		}

		for _, esperada := range doc.Buscas {
			if esperada.Desfecho != "erro" {
				continue
			}
			total++

			_, err := indice.Frase(ctx, esperada.Expressao)
			switch {
			case err == nil:
				// A aspa é apenas um separador para o tokenizador.
				bemDefinidas++
			case errors.Is(err, domain.ErrExpressaoInvalida):
				// A expressão também tem `&` e sintaxe de expressão regular
				// inválida — "ACME & FILHOS \" é as duas coisas ao mesmo tempo.
				// O legado morre antes, no QueryParser; o Go morre depois, no
				// filtro. O desfecho observável coincide: a importação não
				// conclui. Só o caminho difere.
				filtroInvalido++
			default:
				t.Errorf("%s / %q: erro inesperado %v",
					doc.Documento, esperada.Expressao, err)
			}
		}
		if err := indice.Fechar(); err != nil {
			t.Errorf("%s: Fechar: %v", doc.Documento, err)
		}
	}

	if total == 0 {
		t.Fatal("nenhuma expressão de desfecho `erro` no oráculo — o teste não mede nada")
	}
	if bemDefinidas == 0 {
		t.Error("nenhuma expressão rejeitada pelo QueryParser foi tratada sem erro — " +
			"a premissa de que o Go não tem analisador de consulta caiu")
	}
	t.Logf("%d expressões que o QueryParser do legado rejeita: %d bem definidas em Go, "+
		"%d recusadas pelo filtro `&`", total, bemDefinidas, filtroInvalido)
}
