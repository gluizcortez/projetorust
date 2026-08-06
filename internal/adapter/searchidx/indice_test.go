package searchidx

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/gluizcortez/projetorust/internal/domain"
)

// construir é o atalho dos testes: indexa e falha na hora se não der.
func construir(t *testing.T, paginas ...string) domain.Indice {
	t.Helper()
	ix, err := NovoIndexador().Construir(context.Background(), paginas)
	if err != nil {
		t.Fatalf("Construir: %v", err)
	}
	t.Cleanup(func() {
		if err := ix.Fechar(); err != nil {
			t.Errorf("Fechar: %v", err)
		}
	})
	return ix
}

// paginasDe roda a busca e devolve só os números de página.
func paginasDe(t *testing.T, ix domain.Indice, expressao string) []uint64 {
	t.Helper()
	recortes, err := ix.Frase(context.Background(), expressao)
	if err != nil {
		t.Fatalf("Frase(%q): %v", expressao, err)
	}
	paginas := make([]uint64, 0, len(recortes))
	for _, r := range recortes {
		paginas = append(paginas, r.Pagina)
	}
	return paginas
}

// TestINVP05BuscaDeFraseNaoESubcadeia é o caso nomeado de INV-P05.
//
// A consulta do legado é uma PhraseQuery sobre posições de termos. O que separa
// dois termos é irrelevante — some na tokenização. O que NÃO pode faltar é a
// separação: dois termos colados são um termo só, e não casam.
func TestINVP05BuscaDeFraseNaoESubcadeia(t *testing.T) {
	ix := construir(t,
		"contratada joao silva ltda",   // 1 — um espaço
		"contratada joao   silva ltda", // 2 — espaços múltiplos
		"contratada joao\nsilva ltda",  // 3 — quebra de linha
		"contratada joao, silva ltda",  // 4 — pontuação
		"contratada joaosilva ltda",    // 5 — colados: NÃO casa
		"contratada silva joao ltda",   // 6 — ordem trocada: NÃO casa
		"contratada joao pedro silva",  // 7 — termo no meio: NÃO casa
		"joao",                         // 8 — só o primeiro termo
	)

	casam := []uint64{1, 2, 3, 4}
	if obtido := paginasDe(t, ix, "joao silva"); !mesmasPaginasTeste(obtido, casam) {
		t.Errorf("`joao silva` devolveu %v, esperava %v", obtido, casam)
	}

	// O caso negativo decisivo: uma implementação por subcadeia devolveria a 5.
	for _, negativa := range []uint64{5, 6, 7, 8} {
		for _, p := range paginasDe(t, ix, "joao silva") {
			if p == negativa {
				t.Errorf("página %d não deveria casar `joao silva`", negativa)
			}
		}
	}

	// E o termo isolado casa em todas as páginas que o contêm — inclusive a 8,
	// mas nunca a 5, onde `joaosilva` é UM termo.
	umTermo := []uint64{1, 2, 3, 4, 6, 7, 8}
	if obtido := paginasDe(t, ix, "joao"); !mesmasPaginasTeste(obtido, umTermo) {
		t.Errorf("`joao` devolveu %v, esperava %v", obtido, umTermo)
	}
}

// TestINVP06ExpressaoQueTokenizaParaVazio é o caso nomeado de INV-P06: zero
// acertos, sem erro. Nunca correspondência universal.
func TestINVP06ExpressaoQueTokenizaParaVazio(t *testing.T) {
	ix := construir(t, "primeira pagina", "segunda pagina")

	for _, expressao := range []string{"---", "", "   ", "###", ".,;:!?", "\n\t"} {
		recortes, err := ix.Frase(context.Background(), expressao)
		if err != nil {
			t.Errorf("Frase(%q) devolveu erro %v; esperava nil", expressao, err)
		}
		if len(recortes) != 0 {
			t.Errorf("Frase(%q) devolveu %d recorte(s); esperava zero",
				expressao, len(recortes))
		}
	}
}

// TestUmaPaginaGeraNoMaximoUmRecorte: no Tantivy cada página é um documento, e
// um documento aparece uma vez no resultado, por mais vezes que a frase ocorra.
func TestUmaPaginaGeraNoMaximoUmRecorte(t *testing.T) {
	ix := construir(t,
		"joao silva joao silva joao silva joao silva",
		"nada aqui",
	)

	recortes, err := ix.Frase(context.Background(), "joao silva")
	if err != nil {
		t.Fatalf("Frase: %v", err)
	}
	if len(recortes) != 1 {
		t.Fatalf("quatro ocorrências geraram %d recorte(s); esperava 1", len(recortes))
	}
	if recortes[0].Pagina != 1 {
		t.Errorf("página = %d; esperava 1", recortes[0].Pagina)
	}
}

// TestNumeracaoDePaginaComecaEmUm fixa reference/main.rs:526.
func TestNumeracaoDePaginaComecaEmUm(t *testing.T) {
	ix := construir(t, "alfa", "beta", "gama")

	for expressao, esperada := range map[string]uint64{"alfa": 1, "beta": 2, "gama": 3} {
		recortes, err := ix.Frase(context.Background(), expressao)
		if err != nil {
			t.Fatalf("Frase(%q): %v", expressao, err)
		}
		if len(recortes) != 1 || recortes[0].Pagina != esperada {
			t.Errorf("%q devolveu %v; esperava a página %d", expressao, recortes, esperada)
		}
	}
}

// TestRecorteCarregaOTextoIntegralDaPagina fixa ESPECIFICACAO §5.4: apesar do
// nome, `Destaque` não é um trecho ao redor da ocorrência.
func TestRecorteCarregaOTextoIntegralDaPagina(t *testing.T) {
	const pagina = "cabecalho do diario\ncontratada joao silva ltda\nrodape"
	ix := construir(t, pagina)

	recortes, err := ix.Frase(context.Background(), "joao silva")
	if err != nil {
		t.Fatalf("Frase: %v", err)
	}
	if len(recortes) != 1 {
		t.Fatalf("esperava 1 recorte, veio %d", len(recortes))
	}
	if recortes[0].Texto != pagina {
		t.Errorf("Texto = %q; esperava o texto integral", recortes[0].Texto)
	}
	if recortes[0].Destaque != pagina {
		t.Errorf("Destaque = %q; esperava o texto integral", recortes[0].Destaque)
	}
}

// TestFraseMaisLongaQueAPagina: a interseção posicional simplesmente esgota.
func TestFraseMaisLongaQueAPagina(t *testing.T) {
	ix := construir(t, "alfa beta")

	if obtido := paginasDe(t, ix, "alfa beta gama delta"); len(obtido) != 0 {
		t.Errorf("devolveu %v; esperava nada", obtido)
	}
	if obtido := paginasDe(t, ix, "alfa beta"); len(obtido) != 1 {
		t.Errorf("a frase completa devolveu %v; esperava a página 1", obtido)
	}
}

// TestTermoRepetidoNaFrase cobre a frase com o mesmo termo duas vezes, em que a
// lista de ocorrências é intersectada consigo mesma deslocada.
func TestTermoRepetidoNaFrase(t *testing.T) {
	ix := construir(t,
		"muito muito bom",     // 1 — casa `muito muito`
		"muito bom muito bom", // 2 — não casa: nunca consecutivos
		"muito muito muito",   // 3 — casa, e casa `muito muito muito`
	)

	if obtido := paginasDe(t, ix, "muito muito"); !mesmasPaginasTeste(obtido, []uint64{1, 3}) {
		t.Errorf("`muito muito` devolveu %v; esperava [1 3]", obtido)
	}
	if obtido := paginasDe(t, ix, "muito muito muito"); !mesmasPaginasTeste(obtido, []uint64{3}) {
		t.Errorf("`muito muito muito` devolveu %v; esperava [3]", obtido)
	}
}

// TestIndiceVazio: PDF truncado produz zero páginas, e isso não é erro
// (INV-P20).
func TestIndiceVazio(t *testing.T) {
	ix, err := NovoIndexador().Construir(context.Background(), nil)
	if err != nil {
		t.Fatalf("Construir com fatia vazia devolveu erro: %v", err)
	}
	recortes, err := ix.Frase(context.Background(), "qualquer coisa")
	if err != nil {
		t.Errorf("Frase em índice vazio: %v", err)
	}
	if len(recortes) != 0 {
		t.Errorf("índice vazio devolveu %d recorte(s)", len(recortes))
	}
	if err := ix.Fechar(); err != nil {
		t.Errorf("Fechar: %v", err)
	}
}

// TestFiltroDoOperadorSoRodaComAcerto é o caso nomeado de INV-P23.
//
// No legado a expressão regular é compilada DENTRO do laço sobre os acertos
// (main.rs:392-398). Uma expressão com `&` e sintaxe inválida que não produz
// acerto nenhum jamais é compilada e jamais entra em pânico. A captura do
// corpus confirmou: `ACME & FILHOS (` só faz o legado morrer nos três
// documentos em que `ACME FILHOS` tem acerto.
func TestFiltroDoOperadorSoRodaComAcerto(t *testing.T) {
	const invalida = "acme & filhos ("

	semAcerto := construir(t, "documento sem nada de interessante")
	recortes, err := semAcerto.Frase(context.Background(), invalida)
	if err != nil {
		t.Errorf("sem acerto de frase, a expressão inválida NÃO deveria ser compilada; veio %v", err)
	}
	if len(recortes) != 0 {
		t.Errorf("esperava zero recortes, veio %d", len(recortes))
	}

	comAcerto := construir(t, "contratada acme filhos ltda")
	if _, err := comAcerto.Frase(context.Background(), invalida); !errors.Is(err, domain.ErrExpressaoInvalida) {
		t.Errorf("com acerto de frase, esperava ErrExpressaoInvalida; veio %v", err)
	}
}

// TestFiltroDoOperadorSoQuandoHaEComercial: a condição do legado é sobre a
// expressão CRUA (main.rs:392), não sobre os termos.
func TestFiltroDoOperadorSoQuandoHaEComercial(t *testing.T) {
	ix := construir(t, "contratada acme filhos ltda")

	// Sem `&` na expressão, o filtro não roda e a frase casa.
	if obtido := paginasDe(t, ix, "acme filhos"); len(obtido) != 1 {
		t.Errorf("`acme filhos` devolveu %v; esperava a página 1", obtido)
	}
	// Com `&`, a frase ainda casa (o `&` é separador para o tokenizador), mas o
	// filtro exige o `&` no TEXTO — que não está lá.
	if obtido := paginasDe(t, ix, "acme & filhos"); len(obtido) != 0 {
		t.Errorf("`acme & filhos` devolveu %v; o texto não tem `&`", obtido)
	}
}

// TestFiltroDoOperadorMantemEDescarta cobre os dois lados do filtro na mesma
// busca: a frase casa nas quatro páginas, e o `&` decide quais sobrevivem.
//
// É o caso de `08-inv-p01-e-comercial` do corpus, reduzido a unidade.
func TestFiltroDoOperadorMantemEDescarta(t *testing.T) {
	ix := construir(t,
		"contratada acme & filhos ltda",     // 1 — mantida
		"contratada acme&filhos ltda",       // 2 — mantida: o `\s*` aceita zero
		"contratada acme   &   filhos ltda", // 3 — mantida
		"contratada acme filhos ltda",       // 4 — DESCARTADA: a frase casa, mas não há `&`
	)

	// Sem o filtro, a frase casaria nas quatro.
	if obtido := paginasDe(t, ix, "acme filhos"); !mesmasPaginasTeste(obtido, []uint64{1, 2, 3, 4}) {
		t.Fatalf("`acme filhos` devolveu %v; esperava as quatro páginas", obtido)
	}

	// Com o filtro, a quarta cai.
	if obtido := paginasDe(t, ix, "acme & filhos"); !mesmasPaginasTeste(obtido, []uint64{1, 2, 3}) {
		t.Errorf("`acme & filhos` devolveu %v; esperava [1 2 3]", obtido)
	}
}

// TestConstruirRespeitaCancelamento: um documento longo não deve sobreviver ao
// desligamento do processo.
func TestConstruirRespeitaCancelamento(t *testing.T) {
	paginas := make([]string, 2000)
	for i := range paginas {
		paginas[i] = "pagina com algum texto para tokenizar"
	}

	ctx, cancelar := context.WithCancel(context.Background())
	cancelar()

	_, err := NovoIndexador().Construir(ctx, paginas)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("Construir devolveu %v; esperava context.Canceled", err)
	}
	if !errors.Is(err, domain.ErrIndiceIndisponivel) {
		t.Errorf("Construir devolveu %v; esperava também ErrIndiceIndisponivel", err)
	}
}

// TestFraseRespeitaCancelamento cobre o mesmo no lado da busca.
func TestFraseRespeitaCancelamento(t *testing.T) {
	ix := construir(t, "joao silva")

	ctx, cancelar := context.WithCancel(context.Background())
	cancelar()

	if _, err := ix.Frase(ctx, "joao silva"); !errors.Is(err, context.Canceled) {
		t.Errorf("Frase devolveu %v; esperava context.Canceled", err)
	}
}

// TestBuscaConcorrente é o critério de aceite de concorrência: 16 buscas
// simultâneas sobre o MESMO índice, sob -race.
//
// O que o teste protege é concreto: a interseção posicional trabalha sobre uma
// CÓPIA da primeira lista de ocorrências justamente porque escreve nela. Sem a
// cópia, duas buscas concorrentes pela mesma expressão corromperiam o índice —
// e o resultado seria não-determinístico, não um pânico.
func TestBuscaConcorrente(t *testing.T) {
	paginas := make([]string, 200)
	for i := range paginas {
		paginas[i] = fmt.Sprintf(
			"diario oficial pagina %d contratada joao silva ltda acme filhos & cia", i+1)
	}
	ix := construir(t, paginas...)

	expressoes := []string{
		"joao silva", "acme filhos", "acme & filhos", "diario oficial",
		"joao", "silva joao", "---", "contratada joao silva ltda",
	}

	// Referência sequencial: o que cada expressão DEVE devolver.
	referencia := make(map[string][]uint64, len(expressoes))
	for _, e := range expressoes {
		referencia[e] = paginasDe(t, ix, e)
	}

	const goroutines = 16
	const rodadas = 50

	var grupo sync.WaitGroup
	erros := make(chan string, goroutines*rodadas)

	for g := range goroutines {
		grupo.Add(1)
		go func(g int) {
			defer grupo.Done()
			for r := range rodadas {
				e := expressoes[(g+r)%len(expressoes)]
				recortes, err := ix.Frase(context.Background(), e)
				if err != nil {
					erros <- fmt.Sprintf("goroutine %d: Frase(%q): %v", g, e, err)
					return
				}
				obtidas := make([]uint64, 0, len(recortes))
				for _, rec := range recortes {
					obtidas = append(obtidas, rec.Pagina)
				}
				if !mesmasPaginasTeste(obtidas, referencia[e]) {
					erros <- fmt.Sprintf("goroutine %d: Frase(%q) devolveu %v; esperava %v",
						g, e, obtidas, referencia[e])
					return
				}
			}
		}(g)
	}

	grupo.Wait()
	close(erros)
	for msg := range erros {
		t.Error(msg)
	}
}

// TestCacheDeFiltrosConcorrente exercita o único estado mutável do pacote.
func TestCacheDeFiltrosConcorrente(t *testing.T) {
	c := novoCacheDeFiltros()
	expressoes := []string{"a & b", "c & d", "e & (", "f & g", "h & ["}

	var grupo sync.WaitGroup
	for g := range 16 {
		grupo.Add(1)
		go func(g int) {
			defer grupo.Done()
			for r := range 100 {
				_, _ = c.Obter(expressoes[(g+r)%len(expressoes)])
			}
		}(g)
	}
	grupo.Wait()

	if len(c.compilado) != len(expressoes) {
		t.Errorf("o cache guardou %d entradas; esperava %d", len(c.compilado), len(expressoes))
	}
}

func mesmasPaginasTeste(a, b []uint64) bool {
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

// -------------------------------------------------------------------------
// Medições
// -------------------------------------------------------------------------

// paginaSintetica monta uma página de diário oficial com volume de texto
// parecido com o real: cerca de 2,5 KB e 350 termos.
func paginaSintetica(numero int) string {
	var b strings.Builder
	b.Grow(2600)
	fmt.Fprintf(&b, "DIARIO OFICIAL DO MUNICIPIO - PAGINA %d\n", numero)
	for linha := range 40 {
		fmt.Fprintf(&b,
			"processo %d/%d contratada acme filhos ltda objeto aquisicao de bens "+
				"e servicos valor global estimado conforme edital joao silva %d\n",
			numero, linha, linha)
	}
	return b.String()
}

// BenchmarkConstruirIndice500Paginas é a linha de base exigida pelo critério de
// aceite: a memória de pico tem de ficar abaixo do escritor de 500 MB que o
// legado aloca por importação (reference/main.rs:519, achado A04).
//
// `-benchmem` relata a alocação por operação; o valor comparável com os 500 MB
// do legado é esse, porque o escritor do Tantivy é reservado de uma vez a cada
// `criar_indice`.
func BenchmarkConstruirIndice500Paginas(b *testing.B) {
	paginas := make([]string, 500)
	for i := range paginas {
		paginas[i] = paginaSintetica(i + 1)
	}
	indexador := NovoIndexador()
	ctx := context.Background()

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		ix, err := indexador.Construir(ctx, paginas)
		if err != nil {
			b.Fatalf("Construir: %v", err)
		}
		if err := ix.Fechar(); err != nil {
			b.Fatalf("Fechar: %v", err)
		}
	}
}

// TestMemoriaDePicoDeUmDocumentoDe500Paginas mede o pico REAL de heap durante a
// construção e falha se ele se aproximar do orçamento do legado.
//
// O limite é 50 MB — um décimo dos 500 MB do escritor do Tantivy. É folgado de
// propósito: o objetivo é pegar uma regressão de ordem de grandeza, não medir
// ruído do coletor de lixo.
func TestMemoriaDePicoDeUmDocumentoDe500Paginas(t *testing.T) {
	if testing.Short() {
		t.Skip("medição de memória é sensível a ruído")
	}

	paginas := make([]string, 500)
	var bytesDeTexto int
	for i := range paginas {
		paginas[i] = paginaSintetica(i + 1)
		bytesDeTexto += len(paginas[i])
	}

	var antes, depois runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&antes)

	ix, err := NovoIndexador().Construir(context.Background(), paginas)
	if err != nil {
		t.Fatalf("Construir: %v", err)
	}
	// Uma busca, para que o índice não seja otimizado para fora.
	if _, err := ix.Frase(context.Background(), "acme filhos"); err != nil {
		t.Fatalf("Frase: %v", err)
	}

	// A medida comparável com o escritor do legado é a RETIDA: o que o índice
	// ocupa enquanto está vivo. O `runtime.GC()` com o índice ainda referenciado
	// descarta o lixo da construção e deixa só isso de pé.
	runtime.GC()
	runtime.ReadMemStats(&depois)
	retido := int64(depois.HeapAlloc) - int64(antes.HeapAlloc)
	movimentado := depois.TotalAlloc - antes.TotalAlloc

	// Mantém o índice vivo até depois da leitura.
	runtime.KeepAlive(ix)
	if err := ix.Fechar(); err != nil {
		t.Errorf("Fechar: %v", err)
	}

	const orcamentoDoLegado = 500 << 20 // main.rs:519
	const limite = 50 << 20

	t.Logf("500 páginas, %.1f MB de texto: %.1f MB RETIDOS pelo índice "+
		"(%.1f%% do orçamento de %d MB do escritor do legado); "+
		"%.1f MB movimentados na construção",
		float64(bytesDeTexto)/(1<<20), float64(retido)/(1<<20),
		100*float64(retido)/orcamentoDoLegado, orcamentoDoLegado>>20,
		float64(movimentado)/(1<<20))

	if retido > limite {
		t.Errorf("o índice reteve %.1f MB, acima do limite de %d MB",
			float64(retido)/(1<<20), limite>>20)
	}
}

// BenchmarkFrase mede a busca em si, que no laço de recorte roda uma vez por
// expressão de perfil — dezenas a milhares de vezes por importação.
func BenchmarkFrase(b *testing.B) {
	paginas := make([]string, 500)
	for i := range paginas {
		paginas[i] = paginaSintetica(i + 1)
	}
	ctx := context.Background()
	ix, err := NovoIndexador().Construir(ctx, paginas)
	if err != nil {
		b.Fatalf("Construir: %v", err)
	}
	b.Cleanup(func() { _ = ix.Fechar() })

	casos := map[string]string{
		"termo-unico":    "contratada",
		"frase-de-dois":  "joao silva",
		"frase-de-cinco": "objeto aquisicao de bens e",
		"com-operador-e": "acme & filhos",
		"sem-acerto":     "expressao que nao existe",
		"tokeniza-vazio": "---",
	}

	for nome, expressao := range casos {
		b.Run(nome, func(b *testing.B) {
			b.ReportAllocs()
			for range b.N {
				if _, err := ix.Frase(ctx, expressao); err != nil {
					b.Fatalf("Frase: %v", err)
				}
			}
		})
	}
}

// BenchmarkFiltroComECemCache quantifica o achado A08: o legado compila a
// expressão regular DENTRO do laço de resultados, uma vez por página que casou.
//
// A comparação mede exatamente o que o cache economiza numa importação.
func BenchmarkFiltroComECemCache(b *testing.B) {
	const expressao = "acme & filhos"

	b.Run("compilando-sempre", func(b *testing.B) {
		b.ReportAllocs()
		for range b.N {
			if _, err := CompilarFiltro(expressao); err != nil {
				b.Fatalf("CompilarFiltro: %v", err)
			}
		}
	})

	b.Run("com-cache", func(b *testing.B) {
		c := novoCacheDeFiltros()
		if _, err := c.Obter(expressao); err != nil {
			b.Fatalf("Obter: %v", err)
		}
		b.ResetTimer()
		b.ReportAllocs()
		for range b.N {
			if _, err := c.Obter(expressao); err != nil {
				b.Fatalf("Obter: %v", err)
			}
		}
	})
}

// TestBuscaDepoisDeFecharNaoEntraEmPanico fixa o contrato de Fechar: soltar as
// referências não pode transformar uma busca tardia em queda do processo.
func TestBuscaDepoisDeFecharNaoEntraEmPanico(t *testing.T) {
	ix, err := NovoIndexador().Construir(context.Background(), []string{"joao silva"})
	if err != nil {
		t.Fatalf("Construir: %v", err)
	}
	if err := ix.Fechar(); err != nil {
		t.Fatalf("Fechar: %v", err)
	}
	// Fechar é idempotente.
	if err := ix.Fechar(); err != nil {
		t.Errorf("segundo Fechar: %v", err)
	}

	recortes, err := ix.Frase(context.Background(), "joao silva")
	if err != nil {
		t.Errorf("busca depois de fechar devolveu erro: %v", err)
	}
	if len(recortes) != 0 {
		t.Errorf("busca depois de fechar devolveu %d recorte(s)", len(recortes))
	}
}
