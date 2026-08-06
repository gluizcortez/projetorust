package usecase_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gluizcortez/projetorust/internal/domain"
	"github.com/gluizcortez/projetorust/internal/domain/domaintest"
	"github.com/gluizcortez/projetorust/internal/usecase"
)

// -------------------------------------------------------------------------
// Cenário: o conjunto de dublês de um teste, montado com padrões utilizáveis.
// -------------------------------------------------------------------------

type cenario struct {
	diario      *domaintest.Diario
	importacoes *domaintest.RepositorioImportacaoFalso
	perfis      *domaintest.RepositorioPerfilFalso
	recortes    *domaintest.RepositorioRecorteFalso
	extrator    *domaintest.ExtratorTextoFalso
	indice      *domaintest.IndiceFalso
	indexador   *domaintest.IndexadorFalso
	metricas    *metricasFalsas
}

func novoCenario() *cenario {
	d := &domaintest.Diario{}
	indice := &domaintest.IndiceFalso{Diario: d, Acertos: map[string][]uint64{}}
	return &cenario{
		diario:      d,
		importacoes: &domaintest.RepositorioImportacaoFalso{Diario: d, IDGerado: 42},
		perfis:      &domaintest.RepositorioPerfilFalso{Diario: d},
		recortes:    &domaintest.RepositorioRecorteFalso{Diario: d},
		extrator:    &domaintest.ExtratorTextoFalso{Diario: d, Paginas: []string{"pagina um", "pagina dois"}},
		indice:      indice,
		indexador:   &domaintest.IndexadorFalso{Diario: d, Indice: indice},
		metricas:    &metricasFalsas{},
	}
}

// pipeline monta o caso de uso com os dublês do cenário.
//
// O registro é descartado: o que este pacote verifica é a SEQUÊNCIA de chamadas
// às portas, não o texto do log.
func (c *cenario) pipeline(t *testing.T) *usecase.Pipeline {
	t.Helper()
	p, err := usecase.NovoPipeline(usecase.DependenciasDoPipeline{
		Importacoes: c.importacoes,
		Perfis:      c.perfis,
		Recortes:    c.recortes,
		Extrator:    c.extrator,
		Indexador:   c.indexador,
		Relogio:     domaintest.RelogioFixo{Instante: time.Unix(1700000000, 0).UTC()},
		Logger:      slog.New(slog.NewTextHandler(io.Discard, nil)),
		Metricas:    c.metricas,
	})
	if err != nil {
		t.Fatalf("NovoPipeline: %v", err)
	}
	return p
}

// metricasFalsas registra as observações para conferência.
type metricasFalsas struct {
	mu        sync.Mutex
	Estagios  []string
	Paginas   []int
	Recortes  []int
	Desfechos []string
}

func (m *metricasFalsas) ObservarEstagio(estagio string, _ time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Estagios = append(m.Estagios, estagio)
}

func (m *metricasFalsas) ObservarPaginas(n int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Paginas = append(m.Paginas, n)
}

func (m *metricasFalsas) ObservarRecortes(n int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Recortes = append(m.Recortes, n)
}

func (m *metricasFalsas) ContarImportacao(desfecho string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Desfechos = append(m.Desfechos, desfecho)
}

func (m *metricasFalsas) desfechos() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string(nil), m.Desfechos...)
}

// conferirSequencia compara o diário com a sequência esperada, item a item.
func conferirSequencia(t *testing.T, d *domaintest.Diario, esperada []string) {
	t.Helper()
	obtida := d.Entradas()

	if len(obtida) != len(esperada) {
		t.Errorf("a sequência tem %d chamadas, esperava %d", len(obtida), len(esperada))
	}
	for i := range max(len(obtida), len(esperada)) {
		var a, b string
		if i < len(obtida) {
			a = obtida[i]
		}
		if i < len(esperada) {
			b = esperada[i]
		}
		if a != b {
			t.Errorf("chamada %d:\n  obtida   = %q\n  esperada = %q", i, a, b)
		}
	}
	if t.Failed() {
		t.Logf("sequência completa obtida:\n  %s", strings.Join(obtida, "\n  "))
	}
}

// -------------------------------------------------------------------------
// A sequência, que é contrato
// -------------------------------------------------------------------------

// TestSequenciaDeChamadas é o critério de aceite central da fase F8: a ordem
// exata das chamadas às portas numa importação bem-sucedida.
//
// Derivada de reference/main.rs:256-338 e de docs/ESPECIFICACAO.md §3.4 e §5.1.
// Qualquer reordenação — inclusive uma que pareça inofensiva, como gravar o
// status 2 depois de extrair — falha aqui.
func TestSequenciaDeChamadas(t *testing.T) {
	c := novoCenario()
	c.perfis.Chaves = []domain.ChavePesquisa{
		{IDPerfil: 7, Expressao: "ALFA"},
		{IDPerfil: 7, Expressao: "BETA"},
	}
	c.indice.Acertos = map[string][]uint64{
		"ALFA": {1},
		"BETA": {2},
	}

	c.pipeline(t).Processar(context.Background(), 42, []byte("conteudo do pdf"))

	conferirSequencia(t, c.diario, []string{
		// main.rs:260-261 — data_inicio ANTES do status 1.
		"Importacao.MarcarInicio(42)",
		"Importacao.AtualizarStatus(42, selecionado)",
		// main.rs:268 — status 2 ANTES de indexar.
		"Importacao.AtualizarStatus(42, indexando)",
		"Extrator.ExtrairPaginas(bytes=15)",
		"Indexador.Construir(paginas=2)",
		// main.rs:272 — status 3 com o índice pronto.
		"Importacao.AtualizarStatus(42, recortando)",
		"Perfil.ChavesPesquisa(42)",
		// main.rs:282-323 — o laço, uma expressão de cada vez.
		`Indice.Frase("ALFA")`,
		`Recorte.Salvar(imp=42, perfil=7, expressao="ALFA", n=1)`,
		`Indice.Frase("BETA")`,
		`Recorte.Salvar(imp=42, perfil=7, expressao="BETA", n=1)`,
		// main.rs:325-326 — status 5 e depois data_fim com o total.
		"Importacao.AtualizarStatus(42, finalizado)",
		"Importacao.MarcarTermino(42, 2)",
		// Sem equivalente no legado: o Index do Tantivy é derrubado com a tarefa.
		"Indice.Fechar()",
	})

	if desfechos := c.metricas.desfechos(); len(desfechos) != 1 || desfechos[0] != usecase.DesfechoFinalizado {
		t.Errorf("desfechos = %v; esperava [%s]", desfechos, usecase.DesfechoFinalizado)
	}
}

// TestDocumentoSemPaginasConcluiComSucesso é INV-P20 visto do pipeline: o PDF
// truncado produz zero páginas, o índice fica vazio e a importação termina em
// FINALIZADO com zero recortes — indistinguível, no banco, de um diário sem
// ocorrências.
func TestDocumentoSemPaginasConcluiComSucesso(t *testing.T) {
	c := novoCenario()
	c.extrator.Paginas = nil
	c.perfis.Chaves = []domain.ChavePesquisa{{IDPerfil: 7, Expressao: "ALFA"}}

	c.pipeline(t).Processar(context.Background(), 42, []byte("pdf truncado"))

	if u := ultimoStatus(t, c); u != domain.StatusFinalizado {
		t.Errorf("status final = %s; esperava finalizado", u)
	}
	if c.importacoes.TotalFinal != 0 {
		t.Errorf("total_recortes = %d; esperava 0", c.importacoes.TotalFinal)
	}
}

// -------------------------------------------------------------------------
// INV-P12 — deduplicação por perfil, atravessando expressões
// -------------------------------------------------------------------------

// TestINVP12MesmoPerfilMesmaPagina é o caso nomeado: perfil 7 com "ALFA" e
// "BETA", ambas acertando a página 3, produz UM recorte, atribuído a "ALFA".
//
// A atribuição não é arbitrária: as chaves chegam ordenadas por
// (id_perfil, expressao_nm), e a primeira expressão a encontrar a página a
// consome.
func TestINVP12MesmoPerfilMesmaPagina(t *testing.T) {
	c := novoCenario()
	c.perfis.Chaves = []domain.ChavePesquisa{
		{IDPerfil: 7, Expressao: "ALFA"},
		{IDPerfil: 7, Expressao: "BETA"},
	}
	c.indice.Acertos = map[string][]uint64{"ALFA": {3}, "BETA": {3}}

	c.pipeline(t).Processar(context.Background(), 42, []byte("pdf"))

	gravacoes := c.recortes.GravacoesObservadas()
	if len(gravacoes) != 1 {
		t.Fatalf("houve %d gravação(ões); esperava 1", len(gravacoes))
	}
	if gravacoes[0].Chave.Expressao != "ALFA" {
		t.Errorf("a página 3 ficou com %q; esperava ALFA — a alfabeticamente anterior",
			gravacoes[0].Chave.Expressao)
	}
	if c.importacoes.TotalFinal != 1 {
		t.Errorf("total_recortes = %d; esperava 1", c.importacoes.TotalFinal)
	}
}

// TestINVP12PerfisDiferentesMesmaPagina é o contraponto: perfis diferentes não
// interferem entre si, e a mesma página gera um recorte para cada.
func TestINVP12PerfisDiferentesMesmaPagina(t *testing.T) {
	c := novoCenario()
	c.perfis.Chaves = []domain.ChavePesquisa{
		{IDPerfil: 7, Expressao: "ALFA"},
		{IDPerfil: 8, Expressao: "BETA"},
	}
	c.indice.Acertos = map[string][]uint64{"ALFA": {3}, "BETA": {3}}

	c.pipeline(t).Processar(context.Background(), 42, []byte("pdf"))

	gravacoes := c.recortes.GravacoesObservadas()
	if len(gravacoes) != 2 {
		t.Fatalf("houve %d gravação(ões); esperava 2", len(gravacoes))
	}
	if c.importacoes.TotalFinal != 2 {
		t.Errorf("total_recortes = %d; esperava 2", c.importacoes.TotalFinal)
	}
}

// TestINVP12ConjuntoAtravessaExpressoesEReiniciaNoPerfil exercita a regra
// inteira de uma vez, com três perfis e páginas parcialmente sobrepostas.
func TestINVP12ConjuntoAtravessaExpressoesEReiniciaNoPerfil(t *testing.T) {
	c := novoCenario()
	c.perfis.Chaves = []domain.ChavePesquisa{
		{IDPerfil: 7, Expressao: "A"}, // páginas 1, 2
		{IDPerfil: 7, Expressao: "B"}, // páginas 2, 3 → só a 3 sobra
		{IDPerfil: 7, Expressao: "C"}, // página 1     → nada sobra
		{IDPerfil: 8, Expressao: "A"}, // páginas 1, 2 → conjunto reiniciado
	}
	c.indice.Acertos = map[string][]uint64{}
	acertos := []struct {
		expressao string
		paginas   []uint64
	}{
		{"A", []uint64{1, 2}}, {"B", []uint64{2, 3}}, {"C", []uint64{1}},
	}
	for _, a := range acertos {
		c.indice.Acertos[a.expressao] = a.paginas
	}

	c.pipeline(t).Processar(context.Background(), 42, []byte("pdf"))

	type esperada struct {
		perfil    int64
		expressao string
		paginas   []uint64
	}
	esperadas := []esperada{
		{7, "A", []uint64{1, 2}},
		{7, "B", []uint64{3}},
		// "C" não aparece: a página 1 já era do perfil 7.
		{8, "A", []uint64{1, 2}},
	}

	gravacoes := c.recortes.GravacoesObservadas()
	if len(gravacoes) != len(esperadas) {
		t.Fatalf("houve %d gravação(ões); esperava %d", len(gravacoes), len(esperadas))
	}
	for i, e := range esperadas {
		g := gravacoes[i]
		if g.Chave.IDPerfil != e.perfil || g.Chave.Expressao != e.expressao {
			t.Errorf("gravação %d: perfil %d expressão %q; esperava %d/%q",
				i, g.Chave.IDPerfil, g.Chave.Expressao, e.perfil, e.expressao)
			continue
		}
		if len(g.Recortes) != len(e.paginas) {
			t.Errorf("gravação %d (%q): %d recorte(s); esperava %d",
				i, e.expressao, len(g.Recortes), len(e.paginas))
			continue
		}
		for j, pagina := range e.paginas {
			if g.Recortes[j].Pagina != pagina {
				t.Errorf("gravação %d, recorte %d: página %d; esperava %d",
					i, j, g.Recortes[j].Pagina, pagina)
			}
		}
	}

	// 2 + 1 + 0 + 2
	if c.importacoes.TotalFinal != 5 {
		t.Errorf("total_recortes = %d; esperava 5", c.importacoes.TotalFinal)
	}
}

// TestINVP13PerfilZeroNaoAlteraOResultado cobre o sentinela explícito que
// substitui o `let mut id_perfil: i64 = 0` do legado.
//
// Com perfil 0 na primeira posição, o legado NÃO reinicia o conjunto — e o
// resultado é o mesmo, porque o conjunto já nasce vazio. O teste fixa a
// equivalência para que a troca do zero literal por um booleano continue sendo
// inofensiva. Ver docs/DECISOES-ABERTAS.md, D-01.
func TestINVP13PerfilZeroNaoAlteraOResultado(t *testing.T) {
	c := novoCenario()
	c.perfis.Chaves = []domain.ChavePesquisa{
		{IDPerfil: 0, Expressao: "A"},
		{IDPerfil: 0, Expressao: "B"},
		{IDPerfil: 1, Expressao: "A"},
	}
	c.indice.Acertos = map[string][]uint64{"A": {1}, "B": {1}}

	c.pipeline(t).Processar(context.Background(), 42, []byte("pdf"))

	gravacoes := c.recortes.GravacoesObservadas()
	if len(gravacoes) != 2 {
		t.Fatalf("houve %d gravação(ões); esperava 2", len(gravacoes))
	}
	if gravacoes[0].Chave.IDPerfil != 0 || gravacoes[0].Chave.Expressao != "A" {
		t.Errorf("primeira gravação = perfil %d, %q", gravacoes[0].Chave.IDPerfil, gravacoes[0].Chave.Expressao)
	}
	if gravacoes[1].Chave.IDPerfil != 1 {
		t.Errorf("segunda gravação = perfil %d; esperava 1", gravacoes[1].Chave.IDPerfil)
	}
}

// TestRecortesSaoOrdenadosPorPagina fixa main.rs:285: a ordenação acontece
// ANTES da deduplicação, e é ela que decide qual página é consumida primeiro.
func TestRecortesSaoOrdenadosPorPagina(t *testing.T) {
	c := novoCenario()
	c.perfis.Chaves = []domain.ChavePesquisa{{IDPerfil: 7, Expressao: "A"}}
	c.indice.Acertos = map[string][]uint64{"A": {9, 2, 5, 1}}

	c.pipeline(t).Processar(context.Background(), 42, []byte("pdf"))

	gravacoes := c.recortes.GravacoesObservadas()
	if len(gravacoes) != 1 {
		t.Fatalf("houve %d gravação(ões); esperava 1", len(gravacoes))
	}
	esperadas := []uint64{1, 2, 5, 9}
	for i, pagina := range esperadas {
		if gravacoes[0].Recortes[i].Pagina != pagina {
			t.Fatalf("recortes gravados fora de ordem: %v", gravacoes[0].Recortes)
		}
	}
}

// -------------------------------------------------------------------------
// Caminhos de erro
// -------------------------------------------------------------------------

func ultimoStatus(t *testing.T, c *cenario) domain.StatusImportacao {
	t.Helper()
	gravados := c.importacoes.StatusGravados()
	if len(gravados) == 0 {
		t.Fatal("nenhum status foi gravado")
	}
	return gravados[len(gravados)-1]
}

// TestFalhaAoExtrairVaiParaErro cobre main.rs:332-335.
func TestFalhaAoExtrairVaiParaErro(t *testing.T) {
	c := novoCenario()
	c.extrator.Erro = domain.ErrPDFInvalido

	c.pipeline(t).Processar(context.Background(), 42, []byte("nao e pdf"))

	if u := ultimoStatus(t, c); u != domain.StatusErro {
		t.Errorf("status final = %s; esperava erro", u)
	}
	conferirSequencia(t, c.diario, []string{
		"Importacao.MarcarInicio(42)",
		"Importacao.AtualizarStatus(42, selecionado)",
		"Importacao.AtualizarStatus(42, indexando)",
		"Extrator.ExtrairPaginas(bytes=9)",
		"Importacao.AtualizarStatus(42, erro)",
	})
}

// TestFalhaAoIndexarVaiParaErro: no legado extração e indexação são a mesma
// função, e as duas falhas têm o mesmo desfecho.
func TestFalhaAoIndexarVaiParaErro(t *testing.T) {
	c := novoCenario()
	c.indexador.Erro = domain.ErrIndiceIndisponivel

	c.pipeline(t).Processar(context.Background(), 42, []byte("pdf"))

	if u := ultimoStatus(t, c); u != domain.StatusErro {
		t.Errorf("status final = %s; esperava erro", u)
	}
}

// TestFalhaAoObterChavesVaiParaErroSemMaisNada cobre main.rs:327-331: status -1
// e NENHUMA outra escrita.
func TestFalhaAoObterChavesVaiParaErroSemMaisNada(t *testing.T) {
	c := novoCenario()
	c.perfis.Erro = domain.ErrPersistencia

	c.pipeline(t).Processar(context.Background(), 42, []byte("pdf"))

	conferirSequencia(t, c.diario, []string{
		"Importacao.MarcarInicio(42)",
		"Importacao.AtualizarStatus(42, selecionado)",
		"Importacao.AtualizarStatus(42, indexando)",
		"Extrator.ExtrairPaginas(bytes=3)",
		"Indexador.Construir(paginas=2)",
		"Importacao.AtualizarStatus(42, recortando)",
		"Perfil.ChavesPesquisa(42)",
		"Importacao.AtualizarStatus(42, erro)",
		"Indice.Fechar()",
	})
	if len(c.recortes.GravacoesObservadas()) != 0 {
		t.Error("nenhum recorte deveria ter sido gravado")
	}
}

// TestINVP14TrabalhoParcialPermanece é o caso nomeado: falha ao gravar a
// terceira de cinco chaves deixa as duas primeiras gravadas, não processa as
// duas últimas e vai a -1. Não há compensação.
func TestINVP14TrabalhoParcialPermanece(t *testing.T) {
	c := novoCenario()
	c.perfis.Chaves = []domain.ChavePesquisa{
		{IDPerfil: 1, Expressao: "A"},
		{IDPerfil: 2, Expressao: "B"},
		{IDPerfil: 3, Expressao: "C"},
		{IDPerfil: 4, Expressao: "D"},
		{IDPerfil: 5, Expressao: "E"},
	}
	c.indice.Acertos = map[string][]uint64{
		"A": {1}, "B": {1}, "C": {1}, "D": {1}, "E": {1},
	}
	c.recortes.ErroNaChamada = 3
	c.recortes.Erro = domain.ErrPersistencia

	c.pipeline(t).Processar(context.Background(), 42, []byte("pdf"))

	gravacoes := c.recortes.GravacoesObservadas()
	if len(gravacoes) != 2 {
		t.Fatalf("permaneceram %d gravação(ões); esperava as 2 primeiras", len(gravacoes))
	}
	for i, esperada := range []string{"A", "B"} {
		if gravacoes[i].Chave.Expressao != esperada {
			t.Errorf("gravação %d = %q; esperava %q", i, gravacoes[i].Chave.Expressao, esperada)
		}
	}

	if u := ultimoStatus(t, c); u != domain.StatusErro {
		t.Errorf("status final = %s; esperava erro", u)
	}
	// As chaves D e E não podem ter sido consultadas.
	for _, entrada := range c.diario.Entradas() {
		if strings.Contains(entrada, `Frase("D")`) || strings.Contains(entrada, `Frase("E")`) {
			t.Errorf("a importação continuou após a falha: %q", entrada)
		}
	}
	// E nem data_fim nem total_recortes foram gravados.
	if c.importacoes.TotalFinal != 0 {
		t.Errorf("MarcarTermino foi chamado com %d; não deveria ter sido chamado",
			c.importacoes.TotalFinal)
	}
}

// TestFalhaAoBuscarVaiParaErro cobre main.rs:318-322.
func TestFalhaAoBuscarVaiParaErro(t *testing.T) {
	c := novoCenario()
	c.perfis.Chaves = []domain.ChavePesquisa{{IDPerfil: 7, Expressao: "A"}}
	c.indice.Erro = errors.New("falha interna do índice")

	c.pipeline(t).Processar(context.Background(), 42, []byte("pdf"))

	if u := ultimoStatus(t, c); u != domain.StatusErro {
		t.Errorf("status final = %s; esperava erro", u)
	}
}

// TestINVP17AspasAbortamAImportacao é o caso nomeado de INV-P17.
//
// No legado a consulta é montada por interpolação sem escape,
// `format!(r#""{key}""#)` (main.rs:375): uma aspa dupla desbalanceia a consulta,
// o QueryParser devolve Err e a importação vai a -1.
//
// O índice em Go não tem analisador de consulta e não falharia sozinho — a
// condição é verificada no caso de uso, ANTES da busca. É o padrão provisório
// de docs/DECISOES-ABERTAS.md, D-05: reproduzir a falha. Se a resposta de D-05
// for "não existem expressões com aspas em produção", esta verificação vira
// código morto e deve ser removida.
func TestINVP17AspasAbortamAImportacao(t *testing.T) {
	c := novoCenario()
	c.perfis.Chaves = []domain.ChavePesquisa{
		{IDPerfil: 7, Expressao: `ACME "LTDA"`},
		{IDPerfil: 8, Expressao: "NUNCA CHEGA AQUI"},
	}
	c.indice.Acertos = map[string][]uint64{"NUNCA CHEGA AQUI": {1}}

	c.pipeline(t).Processar(context.Background(), 42, []byte("pdf"))

	if u := ultimoStatus(t, c); u != domain.StatusErro {
		t.Errorf("status final = %s; esperava erro", u)
	}
	for _, entrada := range c.diario.Entradas() {
		if strings.Contains(entrada, "Indice.Frase") {
			t.Errorf("a busca não deveria ter sido chamada: %q", entrada)
		}
	}
	if len(c.recortes.GravacoesObservadas()) != 0 {
		t.Error("nenhum recorte deveria ter sido gravado")
	}
}

// TestD06ExpressaoInvalidaDeixaAImportacaoPresa fixa o padrão provisório de
// docs/DECISOES-ABERTAS.md, D-06.
//
// No legado, `regex::Regex::new(&exp).unwrap()` (main.rs:395) entra em PÂNICO
// com expressão inválida. O tokio captura o pânico, a tarefa morre e o status
// NÃO é atualizado: a importação fica presa no último gravado, que é
// `recortando`. Não vai a -1 nem a 5.
//
// É um desfecho diferente do erro comum, e por isso o pipeline NÃO grava -1
// aqui. A normalização para -1 é evolução da fase F11, atrás de chave.
func TestD06ExpressaoInvalidaDeixaAImportacaoPresa(t *testing.T) {
	c := novoCenario()
	c.perfis.Chaves = []domain.ChavePesquisa{{IDPerfil: 7, Expressao: "ACME & FILHOS ("}}
	c.indice.Erro = domain.ErrExpressaoInvalida

	c.pipeline(t).Processar(context.Background(), 42, []byte("pdf"))

	if u := ultimoStatus(t, c); u != domain.StatusRecortando {
		t.Errorf("status final = %s; esperava recortando — a importação fica PRESA", u)
	}
	for _, s := range c.importacoes.StatusGravados() {
		if s == domain.StatusErro {
			t.Error("o pipeline gravou erro; o legado não grava status algum ao entrar em pânico")
		}
	}
	if desfechos := c.metricas.desfechos(); len(desfechos) != 1 || desfechos[0] != usecase.DesfechoPreso {
		t.Errorf("desfechos = %v; esperava [%s]", desfechos, usecase.DesfechoPreso)
	}
}

// TestFalhaAoGravarStatusNaoInterrompe cobre docs/ESPECIFICACAO.md §3.6: no
// legado toda gravação de status usa `let _ = ...` e a falha é ignorada.
func TestFalhaAoGravarStatusNaoInterrompe(t *testing.T) {
	c := novoCenario()
	c.importacoes.ErroAtualizarStatus = domain.ErrPersistencia
	c.importacoes.ErroMarcarInicio = domain.ErrPersistencia
	c.perfis.Chaves = []domain.ChavePesquisa{{IDPerfil: 7, Expressao: "A"}}
	c.indice.Acertos = map[string][]uint64{"A": {1}}

	c.pipeline(t).Processar(context.Background(), 42, []byte("pdf"))

	// O fluxo seguiu até o fim, apesar de nenhuma gravação de status funcionar.
	if len(c.recortes.GravacoesObservadas()) != 1 {
		t.Error("o pipeline parou por causa de uma falha de gravação de status")
	}
	if c.importacoes.TotalFinal != 1 {
		t.Errorf("total_recortes = %d; esperava 1", c.importacoes.TotalFinal)
	}
}

// TestPanicoNaoDerrubaOProcesso: um pânico é defeito NOSSO, sem contrapartida
// no legado — o único pânico alcançável em Rust virou erro tipado (D-06).
// Deixar a linha travada em silêncio seria a pior opção, então grava -1.
func TestPanicoNaoDerrubaOProcesso(t *testing.T) {
	c := novoCenario()
	c.perfis.Chaves = []domain.ChavePesquisa{{IDPerfil: 7, Expressao: "A"}}
	c.indice.Acertos = map[string][]uint64{"A": {1}}
	c.recortes.EntrarEmPanico = true

	c.pipeline(t).Processar(context.Background(), 42, []byte("pdf"))

	if u := ultimoStatus(t, c); u != domain.StatusErro {
		t.Errorf("status final = %s; esperava erro", u)
	}
}

// TestDependenciasAusentesSaoRecusadas: a raiz de composição deve falhar no
// arranque, não na primeira importação.
func TestDependenciasAusentesSaoRecusadas(t *testing.T) {
	if _, err := usecase.NovoPipeline(usecase.DependenciasDoPipeline{}); err == nil {
		t.Fatal("NovoPipeline aceitou dependências vazias")
	}

	c := novoCenario()
	completo := usecase.DependenciasDoPipeline{
		Importacoes: c.importacoes,
		Perfis:      c.perfis,
		Recortes:    c.recortes,
		Extrator:    c.extrator,
		Indexador:   c.indexador,
		Relogio:     domaintest.RelogioFixo{},
		Logger:      slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	// Métricas nulas são aceitas: viram MetricasNulas.
	if _, err := usecase.NovoPipeline(completo); err != nil {
		t.Fatalf("NovoPipeline com métricas nulas: %v", err)
	}

	semExtrator := completo
	semExtrator.Extrator = nil
	_, err := usecase.NovoPipeline(semExtrator)
	if err == nil || !strings.Contains(err.Error(), "Extrator") {
		t.Errorf("erro = %v; esperava mencionar Extrator", err)
	}
}

// TestINVP18TotalAcimaDeInt32NaoGravaDataFim é o caso nomeado de INV-P18.
//
// `registrar_termino_importacao` converte com `i32::try_from`
// (reference/main.rs:712): acima de 2.147.483.647 a conversão FALHA, a função
// devolve Err e o resultado é descartado por `let _ =` (main.rs:326). O status
// permanece 5 e NEM data_fim NEM total_recortes são gravados.
//
// O cenário exige mais de dois bilhões de recortes, o que não se monta com
// dublês de índice; o teste ataca a conversão pelo caminho que ela protege.
func TestINVP18TotalAcimaDeInt32NaoGravaDataFim(t *testing.T) {
	// A guarda que o pipeline usa é a mesma do domínio; aqui se confirma que
	// ela recusa em vez de truncar, que é o que Go faria em silêncio.
	if _, err := domain.TamanhoParaInt32(1 << 31); !errors.Is(err, domain.ErrEstouroNumerico) {
		t.Fatalf("TamanhoParaInt32(2^31) = %v; esperava ErrEstouroNumerico", err)
	}

	// E que o caminho feliz continua gravando.
	c := novoCenario()
	c.perfis.Chaves = []domain.ChavePesquisa{{IDPerfil: 7, Expressao: "A"}}
	c.indice.Acertos = map[string][]uint64{"A": {1, 2}}

	c.pipeline(t).Processar(context.Background(), 42, []byte("pdf"))

	if c.importacoes.TotalFinal != 2 {
		t.Errorf("total_recortes = %d; esperava 2", c.importacoes.TotalFinal)
	}
	if u := ultimoStatus(t, c); u != domain.StatusFinalizado {
		t.Errorf("status = %s; esperava finalizado", u)
	}
}

// TestFalhaAoFecharIndiceNaoAlteraODesfecho: Fechar não tem equivalente no
// legado, então a falha dele não pode mudar o estado da importação.
func TestFalhaAoFecharIndiceNaoAlteraODesfecho(t *testing.T) {
	c := novoCenario()
	c.perfis.Chaves = []domain.ChavePesquisa{{IDPerfil: 7, Expressao: "A"}}
	c.indice.Acertos = map[string][]uint64{"A": {1}}
	c.indice.ErroFechar = errors.New("falha ao liberar")

	c.pipeline(t).Processar(context.Background(), 42, []byte("pdf"))

	if u := ultimoStatus(t, c); u != domain.StatusFinalizado {
		t.Errorf("status = %s; esperava finalizado", u)
	}
	if c.importacoes.TotalFinal != 1 {
		t.Errorf("total_recortes = %d; esperava 1", c.importacoes.TotalFinal)
	}
}

// TestPipelineSemMetricasFunciona exercita MetricasNulas, que é o padrão quando
// a raiz de composição não injeta instrumentação.
func TestPipelineSemMetricasFunciona(t *testing.T) {
	c := novoCenario()
	c.perfis.Chaves = []domain.ChavePesquisa{{IDPerfil: 7, Expressao: "A"}}
	c.indice.Acertos = map[string][]uint64{"A": {1}}

	p, err := usecase.NovoPipeline(usecase.DependenciasDoPipeline{
		Importacoes: c.importacoes,
		Perfis:      c.perfis,
		Recortes:    c.recortes,
		Extrator:    c.extrator,
		Indexador:   c.indexador,
		Relogio:     domaintest.RelogioFixo{Instante: time.Unix(1700000000, 0).UTC()},
		Logger:      slog.New(slog.NewTextHandler(io.Discard, nil)),
		// Metricas deliberadamente nula.
	})
	if err != nil {
		t.Fatalf("NovoPipeline: %v", err)
	}

	p.Processar(context.Background(), 42, []byte("pdf"))

	if u := ultimoStatus(t, c); u != domain.StatusFinalizado {
		t.Errorf("status = %s; esperava finalizado", u)
	}
}
