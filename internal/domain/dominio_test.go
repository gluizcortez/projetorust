package domain_test

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/gluizcortez/projetorust/internal/domain"
)

// -------------------------------------------------------------------------
// Status
// -------------------------------------------------------------------------

func TestTodoStatusDeclaradoTemEntradaNoMapaDeTransicoes(t *testing.T) {
	// Exaustividade: acrescentar uma constante sem acrescentá-la a
	// TodosOsStatus e a TransicoesValidas faz este teste falhar.
	if len(domain.TodosOsStatus) != len(domain.TransicoesValidas) {
		t.Fatalf("TodosOsStatus tem %d itens e TransicoesValidas tem %d — "+
			"alguma constante foi acrescentada sem atualizar as duas listas",
			len(domain.TodosOsStatus), len(domain.TransicoesValidas))
	}
	for _, s := range domain.TodosOsStatus {
		if _, ok := domain.TransicoesValidas[s]; !ok {
			t.Errorf("status %s (=%d) não consta de TransicoesValidas", s, int32(s))
		}
		if s.String() == "" || strings.HasPrefix(s.String(), "desconhecido") {
			t.Errorf("status %d não tem nome legível", int32(s))
		}
		if !s.Conhecido() {
			t.Errorf("status %s deveria ser Conhecido", s)
		}
	}
}

func TestValoresDeStatusCorrespondemAoLegado(t *testing.T) {
	esperado := map[domain.StatusImportacao]int32{
		domain.StatusErro:        -1,
		domain.StatusRecebido:    0,
		domain.StatusSelecionado: 1,
		domain.StatusIndexando:   2,
		domain.StatusRecortando:  3,
		domain.StatusReservado:   4,
		domain.StatusFinalizado:  5,
	}
	for s, v := range esperado {
		if int32(s) != v {
			t.Errorf("%s = %d, esperado %d (docs/ESPECIFICACAO.md §3.1)", s, int32(s), v)
		}
	}
}

func TestTransicoes(t *testing.T) {
	// A tabela cobre TODOS os pares (origem, destino) declarados, válidos e
	// inválidos. Derivada de docs/ESPECIFICACAO.md §3.3.
	validas := map[domain.StatusImportacao][]domain.StatusImportacao{
		domain.StatusRecebido:    {domain.StatusRecebido, domain.StatusSelecionado},
		domain.StatusSelecionado: {domain.StatusIndexando, domain.StatusErro},
		domain.StatusIndexando:   {domain.StatusRecortando, domain.StatusErro},
		domain.StatusRecortando:  {domain.StatusFinalizado, domain.StatusErro},
	}

	for _, origem := range domain.TodosOsStatus {
		for _, destino := range domain.TodosOsStatus {
			esperado := false
			for _, d := range validas[origem] {
				if d == destino {
					esperado = true
				}
			}
			obtido := origem.PodeTransicionarPara(destino)
			if obtido != esperado {
				t.Errorf("%s -> %s: obtive %v, esperado %v", origem, destino, obtido, esperado)
			}
		}
	}
}

func TestEstadosTerminais(t *testing.T) {
	for _, s := range domain.TodosOsStatus {
		terminal := s == domain.StatusErro || s == domain.StatusFinalizado
		if s.Terminal() != terminal {
			t.Errorf("%s.Terminal() = %v, esperado %v", s, s.Terminal(), terminal)
		}
		if terminal && len(domain.TransicoesValidas[s]) != 0 {
			t.Errorf("%s é terminal mas tem transições de saída", s)
		}
	}
}

func TestStatusRecebidoTransicionaParaSiMesmo(t *testing.T) {
	// O legado grava status 0 no INSERT e de novo no UPDATE de main.rs:249.
	// Achado A16 — escrita redundante, preservada.
	if !domain.StatusRecebido.PodeTransicionarPara(domain.StatusRecebido) {
		t.Error("a escrita redundante de status 0 (A16) precisa ser uma transição válida")
	}
}

func TestStatusDesconhecido(t *testing.T) {
	s := domain.StatusImportacao(99)
	if s.Conhecido() {
		t.Error("99 não é um status declarado")
	}
	if !strings.Contains(s.String(), "99") {
		t.Errorf("o nome de um status desconhecido deveria citar o valor: %s", s)
	}
}

// -------------------------------------------------------------------------
// Críticas
// -------------------------------------------------------------------------

func TestTextosDeCriticaSaoOsLiteraisDoLegado(t *testing.T) {
	// Conferidos linha a linha contra reference/main.rs na fase F3.
	esperado := map[string]string{
		"main.rs:155": domain.CriticaDataCadernoInvalida,
		"main.rs:159": domain.CriticaDataCadernoAusente,
		"main.rs:168": domain.CriticaDataDisponibilizacaoInvalida,
		"main.rs:172": domain.CriticaDataDisponibilizacaoAusente,
		"main.rs:181": domain.CriticaIDUsuarioInvalido,
		"main.rs:185": domain.CriticaIDUsuarioAusente,
		"main.rs:194": domain.CriticaIDCadernoInvalido,
		"main.rs:198": domain.CriticaIDCadernoAusente,
		"main.rs:207": domain.CriticaPDFSemNome,
		"main.rs:211": domain.CriticaPDFAusente,
	}
	literais := map[string]string{
		"main.rs:155": "Data do caderno é inválida",
		"main.rs:159": "Data do caderno não informada",
		"main.rs:168": "Data de disponibilização é inválida",
		"main.rs:172": "Data de disponibilização não informada",
		"main.rs:181": "Id do usuário é inválido",
		"main.rs:185": "Id do usuário não informado",
		"main.rs:194": "Id do caderno é inválido",
		"main.rs:198": "Id do caderno não informado",
		"main.rs:207": "PDF não possui nome",
		"main.rs:211": "PDF não enviado",
	}
	for origem, constante := range esperado {
		if constante != literais[origem] {
			t.Errorf("%s: constante = %q, literal do legado = %q", origem, constante, literais[origem])
		}
	}
}

func TestSeparadorEhVirgulaSemEspaco(t *testing.T) {
	if domain.SeparadorDeCriticas != "," {
		t.Errorf("separador = %q, o legado usa join(\",\") em main.rs:220", domain.SeparadorDeCriticas)
	}
}

func TestCriticasPreservamOrdemDeInsercao(t *testing.T) {
	var c domain.Criticas
	if !c.Vazio() {
		t.Error("acumulador novo deveria estar vazio")
	}
	if c.Mensagem() != "" {
		t.Errorf("acumulador vazio deveria produzir mensagem vazia, obtive %q", c.Mensagem())
	}

	c.Adicionar("terceiro")
	c.Adicionar("primeiro")
	c.Adicionar("segundo")

	if c.Total() != 3 {
		t.Errorf("Total = %d", c.Total())
	}
	if got := c.Mensagem(); got != "terceiro,primeiro,segundo" {
		t.Errorf("Mensagem = %q — a ordem de inserção precisa ser preservada", got)
	}
	if !errors.Is(c.Erro(), domain.ErrValidacao) {
		t.Error("com críticas, Erro() deveria devolver ErrValidacao")
	}
}

func TestItensDevolveCopia(t *testing.T) {
	var c domain.Criticas
	c.Adicionar("a")
	itens := c.Itens()
	itens[0] = "modificado"
	if c.Mensagem() != "a" {
		t.Error("Itens() deveria devolver cópia, não a fatia interna")
	}
}

// TestValidacaoExaustiva percorre TODAS as combinações de estado dos cinco
// campos e confere a mensagem byte a byte contra a esperada.
//
// São 3^5 = 243 combinações. É o critério de aceite da fase.
func TestValidacaoExaustiva(t *testing.T) {
	const (
		ok = iota
		ausente
		invalido
	)
	nomes := map[int]string{ok: "ok", ausente: "ausente", invalido: "invalido"}

	// Valores por estado, para cada campo.
	texto := func(s string) *string { return &s }

	dataPor := map[int]*string{ok: texto("2024-03-15"), ausente: nil, invalido: texto("15/03/2024")}
	i64Por := map[int]*string{ok: texto("44521"), ausente: nil, invalido: texto("abc")}
	i32Por := map[int]*string{ok: texto("7"), ausente: nil, invalido: texto("2147483648")}

	// Crítica esperada por campo e estado, NA ORDEM contratual.
	esperadaPor := []map[int]string{
		{ausente: domain.CriticaDataCadernoAusente, invalido: domain.CriticaDataCadernoInvalida},
		{ausente: domain.CriticaDataDisponibilizacaoAusente, invalido: domain.CriticaDataDisponibilizacaoInvalida},
		{ausente: domain.CriticaIDUsuarioAusente, invalido: domain.CriticaIDUsuarioInvalido},
		{ausente: domain.CriticaIDCadernoAusente, invalido: domain.CriticaIDCadernoInvalido},
		{ausente: domain.CriticaPDFAusente, invalido: domain.CriticaPDFSemNome},
	}

	total := 0
	for _, dc := range []int{ok, ausente, invalido} {
		for _, dd := range []int{ok, ausente, invalido} {
			for _, iu := range []int{ok, ausente, invalido} {
				for _, ic := range []int{ok, ausente, invalido} {
					for _, pdf := range []int{ok, ausente, invalido} {
						total++
						estados := []int{dc, dd, iu, ic, pdf}

						s := domain.SubmissaoPDF{
							DataCaderno:          dataPor[dc],
							DataDisponibilizacao: dataPor[dd],
							IDUsuario:            i64Por[iu],
							IDCaderno:            i32Por[ic],
						}
						switch pdf {
						case ok:
							s.ArquivoEnviado, s.NomeDoArquivo = true, texto("diario.pdf")
						case ausente:
							s.ArquivoEnviado = false
						case invalido: // arquivo presente, sem nome
							s.ArquivoEnviado, s.NomeDoArquivo = true, nil
						}

						var esperadas []string
						for i, estado := range estados {
							if estado != ok {
								esperadas = append(esperadas, esperadaPor[i][estado])
							}
						}
						esperada := strings.Join(esperadas, ",")

						nome := fmt.Sprintf("%s/%s/%s/%s/%s",
							nomes[dc], nomes[dd], nomes[iu], nomes[ic], nomes[pdf])

						t.Run(nome, func(t *testing.T) {
							imp, criticas := s.Validar()

							if got := criticas.Mensagem(); got != esperada {
								t.Fatalf("mensagem 400 divergente\n  obtida:   %q\n  esperada: %q", got, esperada)
							}
							if criticas.Vazio() != (len(esperadas) == 0) {
								t.Errorf("Vazio() = %v com %d críticas", criticas.Vazio(), len(esperadas))
							}

							// Com tudo válido, a entidade sai completa e com os
							// padrões implícitos aplicados.
							if len(esperadas) == 0 {
								if imp.Status != domain.StatusRecebido {
									t.Errorf("Status = %s, esperado recebido", imp.Status)
								}
								if imp.TipoCaderno != domain.TipoCadernoPDF {
									t.Errorf("TipoCaderno = %q", imp.TipoCaderno)
								}
								if imp.IDUsuario != 44521 || imp.IDCaderno != 7 {
									t.Errorf("ids = %d/%d", imp.IDUsuario, imp.IDCaderno)
								}
								if imp.DataCaderno.String() != "2024-03-15" {
									t.Errorf("DataCaderno = %s", imp.DataCaderno)
								}
								if imp.ArquivoPDF != "diario.pdf" {
									t.Errorf("ArquivoPDF = %q", imp.ArquivoPDF)
								}
								if imp.HashSHA256 != "" {
									t.Error("o resumo só é calculado depois da validação")
								}
								if imp.Persistida() {
									t.Error("importação recém-validada não tem identificador")
								}
							}
						})
					}
				}
			}
		}
	}

	if total != 243 {
		t.Errorf("esperava 243 combinações, percorri %d", total)
	}
}

// TestMensagemDeTodosOsCamposAusentes fixa o exemplo literal da especificação.
func TestMensagemDeTodosOsCamposAusentes(t *testing.T) {
	_, criticas := domain.SubmissaoPDF{}.Validar()

	const esperada = "Data do caderno não informada," +
		"Data de disponibilização não informada," +
		"Id do usuário não informado," +
		"Id do caderno não informado," +
		"PDF não enviado"

	if got := criticas.Mensagem(); got != esperada {
		t.Errorf("docs/ESPECIFICACAO.md §1.4.2\n  obtida:   %q\n  esperada: %q", got, esperada)
	}
}

// -------------------------------------------------------------------------
// Entidades
// -------------------------------------------------------------------------

func TestNovaImportacaoAplicaPadroesImplicitos(t *testing.T) {
	d, _ := domain.NovaData(2024, 3, 15)
	imp := domain.NovaImportacao(1, 2, d, d, "diario.pdf", "abc123")

	if imp.Status != domain.StatusRecebido {
		t.Errorf("Status = %s, o Default do legado é 0 (main.rs:439)", imp.Status)
	}
	if imp.TipoCaderno != "PDF" {
		t.Errorf("TipoCaderno = %q, o Default do legado é \"PDF\" (main.rs:441)", imp.TipoCaderno)
	}
	if imp.ID != 0 || imp.Persistida() {
		t.Error("importação nova não tem identificador")
	}
}

func TestNovoRecorteDuplicaOTexto(t *testing.T) {
	const pagina = "texto integral da página, normalizado"
	r := domain.NovoRecorte(47, pagina)

	if r.Pagina != 47 {
		t.Errorf("Pagina = %d", r.Pagina)
	}
	if r.Texto != pagina || r.Destaque != pagina {
		t.Error("Texto e Destaque recebem o mesmo valor (main.rs:400-406)")
	}
	if r.Texto != r.Destaque {
		t.Error("são idênticos por construção — ver ESPECIFICACAO §5.4")
	}
}

// -------------------------------------------------------------------------
// Erros
// -------------------------------------------------------------------------

func TestErrosSentinelaSaoDistinguiveis(t *testing.T) {
	sentinelas := []error{
		domain.ErrValidacao, domain.ErrPDFInvalido, domain.ErrExtracaoTexto,
		domain.ErrIndiceIndisponivel, domain.ErrPersistencia,
		domain.ErrImportacaoNaoEncontrada, domain.ErrEstouroNumerico,
		domain.ErrDataInvalida,
	}
	for i, a := range sentinelas {
		envolvido := fmt.Errorf("contexto da falha: %w", a)
		if !errors.Is(envolvido, a) {
			t.Errorf("sentinela %d não sobrevive a %%w", i)
		}
		for j, b := range sentinelas {
			if i != j && errors.Is(envolvido, b) {
				t.Errorf("sentinelas %d e %d se confundem", i, j)
			}
		}
	}
}

// -------------------------------------------------------------------------
// Conversões estreitantes — INV-P15 e INV-P18
// -------------------------------------------------------------------------

func TestConversoesEstreitantesFalhamEmVezDeTruncar(t *testing.T) {
	casos := []struct {
		nome   string
		fn     func() error
		aceita bool
	}{
		{"int32 no limite", func() error { _, e := domain.ParaInt32(2147483647); return e }, true},
		{"int32 acima", func() error { _, e := domain.ParaInt32(2147483648); return e }, false},
		{"int32 limite inferior", func() error { _, e := domain.ParaInt32(-2147483648); return e }, true},
		{"int32 abaixo", func() error { _, e := domain.ParaInt32(-2147483649); return e }, false},
		{"int64 de uint64 comum", func() error { _, e := domain.ParaInt64(47); return e }, true},
		{"int64 no limite", func() error { _, e := domain.ParaInt64(1 << 62); return e }, true},
		{"int64 acima", func() error { _, e := domain.ParaInt64(1 << 63); return e }, false},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			err := c.fn()
			if c.aceita && err != nil {
				t.Errorf("deveria caber: %v", err)
			}
			if !c.aceita {
				if err == nil {
					t.Error("deveria estourar — Go truncaria em silêncio (INV-P15)")
				} else if !errors.Is(err, domain.ErrEstouroNumerico) {
					t.Errorf("erro deveria envolver ErrEstouroNumerico: %v", err)
				}
			}
		})
	}

	// O valor truncado que Go produziria, para deixar o risco explícito.
	if v, err := domain.ParaInt32(2147483648); err == nil {
		t.Errorf("2147483648 foi aceito como %d — é exatamente o truncamento que INV-P15 proíbe", v)
	}
}
