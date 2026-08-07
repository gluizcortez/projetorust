package postgres_test

import (
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"strings"
	"testing"

	"github.com/gluizcortez/projetorust/internal/adapter/postgres"
)

// somaDaConsulta congela o resumo SHA-256 de cada consulta literal.
//
// # Por que resumo, e não comparação com o Rust
//
// Até aqui este teste lia `reference/main.rs` e comparava BYTE A BYTE com o
// literal de lá. O arquivo de referência saiu do repositório junto com o
// ferramental de paridade — e a propriedade que ele protegia continua sendo a
// mais importante da camada de persistência:
//
//	NENHUMA destas consultas pode ser reescrita, reformatada ou "otimizada".
//
// O `ORDER BY` de `obter_chaves_pesquisa` governa a deduplicação por perfil
// (INV-P12) e portanto QUAL EXPRESSÃO fica gravada numa página disputada.
// `salvar_recorte/texto` grava o literal `'PDF'` dentro do próprio SQL. Um
// espaço a mais em qualquer uma delas muda o conteúdo do banco.
//
// Sem o Rust, o oráculo passa a ser este resumo: ele não prova que a consulta
// é igual à do legado — isso foi provado quando a referência existia e está
// registrado em docs/ESPECIFICACAO.md §2.5 —, mas prova que ela não MUDOU
// desde então, que é o que um teste de regressão precisa fazer.
//
// # Se algum destes valores mudar
//
// A pergunta certa NÃO é "qual o novo resumo". É: por que a consulta mudou?
// Alterá-la exige reabrir a comparação com o serviço original, hoje disponível
// apenas no histórico do git — `git show 2febb7a:reference/main.rs`.
var somaDaConsulta = map[string]string{
	"registrar_pdf":                "bf04a141970f2829555bd89536a5fd0f3b2d91ddcd7c3a761e4d6d16f0c3f8a8",
	"obter_chaves_pesquisa":        "b9a5ffd8412a1dfba894186b155cc2f859d9d0736a0ed87f98fa2bc48b4f407d",
	"salvar_recorte/recorte":       "cbb7cc3ae3d5e80ebb9b73191c08dc921be843f93b9fbb1d9acf73f4d6ac42a0",
	"salvar_recorte/texto":         "3c130d593f7cd925200250742e760d1257232f417fa725a7cb20ec7a7688dcbb",
	"atualizar_status_importacao":  "bb433acb9635fb759f78abdba479f3e9047c0c31f38dfe7b73876a2bf729c316",
	"registrar_inicio_importacao":  "2f35a8b7d4b08ce374047e06400ed449b8b61017c13d9d2afe302352d8ac8f01",
	"registrar_termino_importacao": "a29ba75cc7b667b4c1a29194781c81256e928919ca97750f656eca6f886c2685",
}

// TestConsultasNaoMudaram é o critério de aceite herdado da fase F4.
func TestConsultasNaoMudaram(t *testing.T) {
	embutidas := postgres.ConsultasLiterais()
	if len(embutidas) != len(somaDaConsulta) {
		t.Fatalf("o pacote expõe %d consultas e o teste conhece %d",
			len(embutidas), len(somaDaConsulta))
	}

	for nome, esperada := range somaDaConsulta {
		t.Run(nome, func(t *testing.T) {
			sql, presente := embutidas[nome]
			if !presente {
				t.Fatalf("consulta %q não está embutida no pacote", nome)
			}
			soma := sha256.Sum256([]byte(sql))
			obtida := hex.EncodeToString(soma[:])
			if obtida == esperada {
				return
			}
			t.Errorf("a consulta MUDOU\n  resumo esperado: %s\n  resumo obtido:   %s\n"+
				"  Alterar uma consulta literal muda o conteúdo do banco. Se a mudança\n"+
				"  for intencional, compare de novo com o serviço original:\n"+
				"      git show 2febb7a:reference/main.rs\n"+
				"  e registre a decisão em docs/ESPECIFICACAO.md §2.5.\n\n%s",
				esperada, obtida, sql)
		})
	}
}

// TestConsultasDeEvolucaoSaoDisjuntas: as consultas da fase F11 NÃO existem no
// legado e não podem se misturar às literais.
func TestConsultasDeEvolucaoSaoDisjuntas(t *testing.T) {
	literais := postgres.ConsultasLiterais()
	for nome := range postgres.ConsultasDeEvolucao() {
		if _, colide := literais[nome]; colide {
			t.Errorf("a consulta de evolução %q colide com uma literal", nome)
		}
	}
}

func TestOrderByDasChavesEhIntocavel(t *testing.T) {
	consulta := postgres.ConsultasLiterais()["obter_chaves_pesquisa"]

	// A ordenação precisa ser exatamente esta, nesta ordem de colunas.
	padrao := regexp.MustCompile(`(?s)ORDER\s+BY\s+tp\.id_perfil\s*,\s*tpv\.expressao_nm`)
	if !padrao.MatchString(consulta) {
		t.Fatalf(
			"a cláusula ORDER BY de obter_chaves_pesquisa foi alterada.\n"+
				"Ela governa a deduplicação por perfil (INV-P12) e portanto QUAL\n"+
				"expressão é gravada em tb_recorte.expressao_busca para uma página\n"+
				"disputada. Consulta atual:\n%s", consulta)
	}
}

// TestConsultasNaoSaoMontadasPorConcatenacao confere que os parâmetros são
// posicionais, nunca interpolados.
func TestConsultasUsamParametrosPosicionais(t *testing.T) {
	esperados := map[string]int{
		"registrar_pdf":                8,
		"obter_chaves_pesquisa":        1,
		"salvar_recorte/recorte":       4,
		"salvar_recorte/texto":         2,
		"atualizar_status_importacao":  2,
		"registrar_inicio_importacao":  1,
		"registrar_termino_importacao": 2,
	}
	padrao := regexp.MustCompile(`\$\d+`)

	for nome, consulta := range postgres.ConsultasLiterais() {
		distintos := map[string]bool{}
		for _, m := range padrao.FindAllString(consulta, -1) {
			distintos[m] = true
		}
		if len(distintos) != esperados[nome] {
			t.Errorf("%s: %d parâmetros distintos, esperado %d",
				nome, len(distintos), esperados[nome])
		}
	}
}

// TestCabecalhoNaoVazaParaOBanco confere que o comentário explicativo dos
// arquivos .sql fica fora do que é enviado ao PostgreSQL.
func TestCabecalhoNaoVazaParaOBanco(t *testing.T) {
	for nome, consulta := range postgres.ConsultasLiterais() {
		if strings.Contains(consulta, ">>>>> INICIO DA CONSULTA LITERAL") {
			t.Errorf("%s: o marcador vazou para a consulta", nome)
		}
		if strings.HasPrefix(strings.TrimSpace(consulta), "--") {
			t.Errorf("%s: o cabeçalho de comentário vazou para a consulta", nome)
		}
	}
}
