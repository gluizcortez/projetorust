package postgres_test

import (
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/gluizcortez/projetorust/internal/adapter/postgres"
)

// origemDaConsulta localiza cada literal SQL no código Rust de referência.
//
// Os intervalos são [primeira linha com a aspa de abertura, linha com a aspa
// de fechamento], em base 1 — os mesmos citados em docs/ESPECIFICACAO.md §2.5.
var origemDaConsulta = map[string][2]int{
	"registrar_pdf":                {448, 462},
	"obter_chaves_pesquisa":        {545, 563},
	"salvar_recorte/recorte":       {574, 585},
	"salvar_recorte/texto":         {587, 595},
	"atualizar_status_importacao":  {664, 671},
	"registrar_inicio_importacao":  {683, 690},
	"registrar_termino_importacao": {701, 709},
}

// TestConsultasSaoIdenticasAoLegado é o critério de aceite central da fase F4.
//
// Carrega cada consulta das DUAS fontes — o arquivo .sql embutido no binário e
// o literal dentro de reference/main.rs — e compara BYTE A BYTE. Qualquer
// reformatação, reordenação de coluna ou "otimização" de uma consulta reprova.
func TestConsultasSaoIdenticasAoLegado(t *testing.T) {
	bruto, err := os.ReadFile("../../../reference/main.rs")
	if err != nil {
		t.Fatalf("lendo a referência normativa: %v", err)
	}
	linhas := strings.Split(string(bruto), "\n")

	embutidas := postgres.ConsultasLiterais()
	if len(embutidas) != len(origemDaConsulta) {
		t.Fatalf("o pacote expõe %d consultas e o teste conhece %d",
			len(embutidas), len(origemDaConsulta))
	}

	for nome, intervalo := range origemDaConsulta {
		t.Run(nome, func(t *testing.T) {
			esperada := literalDoRust(t, linhas, intervalo[0], intervalo[1])
			obtida, presente := embutidas[nome]
			if !presente {
				t.Fatalf("consulta %q não está embutida no pacote", nome)
			}

			if obtida == esperada {
				return
			}

			// Diferença: aponta o primeiro byte divergente com contexto.
			pos := primeiraDivergencia(obtida, esperada)
			t.Errorf(
				"a consulta embutida DIVERGE do literal de main.rs:%d-%d\n"+
					"  primeira divergência no byte %d\n"+
					"  embutida: %q\n"+
					"  legado:   %q\n"+
					"  A consulta é transcrição literal: nenhum caractere pode ser alterado.",
				intervalo[0], intervalo[1], pos,
				trecho(obtida, pos), trecho(esperada, pos),
			)
		})
	}
}

// literalDoRust extrai o conteúdo de uma string literal Rust delimitada pelas
// linhas informadas, preservando cada byte — inclusive espaços à direita.
func literalDoRust(t *testing.T, linhas []string, primeira, ultima int) string {
	t.Helper()
	if primeira < 1 || ultima > len(linhas) || primeira >= ultima {
		t.Fatalf("intervalo inválido: %d-%d", primeira, ultima)
	}

	abre := linhas[primeira-1]
	idx := strings.Index(abre, `r"`)
	if idx >= 0 {
		idx += 2
	} else {
		idx = strings.Index(abre, `"`)
		if idx < 0 {
			t.Fatalf("linha %d não abre uma string: %q", primeira, abre)
		}
		idx++
	}

	fecha := linhas[ultima-1]
	fim := strings.Index(fecha, `"`)
	if fim < 0 {
		t.Fatalf("linha %d não fecha a string: %q", ultima, fecha)
	}

	partes := []string{abre[idx:]}
	partes = append(partes, linhas[primeira:ultima-1]...)
	partes = append(partes, fecha[:fim])
	return strings.Join(partes, "\n")
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

func trecho(s string, pos int) string {
	inicio := max(0, pos-30)
	fim := min(len(s), pos+30)
	return s[inicio:fim]
}

// TestOrderByDasChavesEhIntocavel protege a cláusula que governa INV-P12.
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
