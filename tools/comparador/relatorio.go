package comparador

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
)

// EscreverJSON emite o relatório em JSON indentado.
//
// É a forma consumida por máquina — painel, integração contínua, comparação
// entre execuções. O formato de texto é o consumido por gente.
func EscreverJSON(w io.Writer, rel Relatorio) error {
	cod := json.NewEncoder(w)
	cod.SetIndent("", "  ")
	if err := cod.Encode(rel); err != nil {
		return fmt.Errorf("escrevendo o relatório em JSON: %w", err)
	}
	return nil
}

// EscreverTexto emite o relatório legível.
//
// A ordem é deliberada: primeiro o VEREDITO, porque é o que decide se corta;
// depois a tabela por camada, que localiza; e por último os casos reprodutores,
// que é o que se leva para depurar.
func EscreverTexto(w io.Writer, rel Relatorio) error {
	var b strings.Builder

	b.WriteString("RELATÓRIO DE PARIDADE — corpus dourado\n")
	b.WriteString(strings.Repeat("=", 72) + "\n\n")
	fmt.Fprintf(&b, "gerado em   %s\n", rel.GeradoEm.Format("2006-01-02 15:04:05 MST"))
	fmt.Fprintf(&b, "corpus      %s (%d documentos)\n\n", rel.DirCorpus, rel.Documentos)

	// --- veredito -------------------------------------------------------
	if rel.Aprovado() {
		b.WriteString("VEREDITO: APROVADO — zero divergência de recortes.\n")
	} else {
		b.WriteString("VEREDITO: REPROVADO — há divergência de recortes.\n")
	}
	b.WriteString("\nO portão de corte é ZERO DIVERGÊNCIA nas camadas 4 e 5. As camadas 1 a 3\n" +
		"são diagnósticas: elas localizam a causa, e uma diferença que não chegue a\n" +
		"mudar recorte não altera o conteúdo do banco.\n\n")

	// --- tabela por camada ----------------------------------------------
	b.WriteString("CAMADAS\n")
	b.WriteString(strings.Repeat("-", 72) + "\n")
	fmt.Fprintf(&b, "%-16s %10s %10s %12s %10s\n", "camada", "unidade", "iguais", "divergentes", "taxa")
	for _, c := range rel.Camadas {
		fmt.Fprintf(&b, "%-16s %10s %10d %12d %9.4f%%\n",
			c.Camada, c.Unidade, c.Iguais, c.Divergentes, c.Taxa()*100)
	}
	b.WriteString("\n")

	// --- classes ---------------------------------------------------------
	temClasses := false
	for _, c := range rel.Camadas {
		if len(c.PorClasse) > 0 {
			temClasses = true
			break
		}
	}
	if temClasses {
		b.WriteString("DIVERGÊNCIAS POR CLASSE\n")
		b.WriteString(strings.Repeat("-", 72) + "\n")
		for _, c := range rel.Camadas {
			if len(c.PorClasse) == 0 {
				continue
			}
			classes := make([]string, 0, len(c.PorClasse))
			for classe := range c.PorClasse {
				classes = append(classes, string(classe))
			}
			sort.Strings(classes)
			for _, classe := range classes {
				fmt.Fprintf(&b, "%-16s %-24s %6d\n", c.Camada, classe, c.PorClasse[Classe(classe)])
			}
		}
		b.WriteString("\n")
	}

	// --- casos reprodutores ----------------------------------------------
	if len(rel.Divergencias) == 0 {
		b.WriteString("Nenhuma divergência.\n")
		_, err := io.WriteString(w, b.String())
		return envolver(err)
	}

	b.WriteString("MENOR CASO REPRODUTOR POR CLASSE\n")
	b.WriteString(strings.Repeat("-", 72) + "\n")
	b.WriteString("Um representante por (camada, classe). O total está na tabela acima.\n\n")

	for i, d := range rel.Divergencias {
		fmt.Fprintf(&b, "[%d] %s · %s\n", i+1, d.Camada, d.Classe)
		fmt.Fprintf(&b, "    documento: %s", d.Documento)
		if d.Pagina > 0 {
			fmt.Fprintf(&b, ", página %d", d.Pagina)
		}
		b.WriteString("\n")
		fmt.Fprintf(&b, "    %s\n", d.Detalhe)

		if d.MenorCaso != nil {
			m := d.MenorCaso
			if m.Linha > 0 {
				fmt.Fprintf(&b, "    linha %d:\n", m.Linha)
			}
			fmt.Fprintf(&b, "      esperado: %q\n", recortarPara(m.Esperado, 120))
			fmt.Fprintf(&b, "      obtido:   %q\n", recortarPara(m.Obtido, 120))
			if m.Runa >= 0 {
				fmt.Fprintf(&b, "      primeira diferença na runa %d: %s vs %s\n",
					m.Runa, m.PontoEsperado, m.PontoObtido)
			}
		}
		b.WriteString("\n")
	}

	_, err := io.WriteString(w, b.String())
	return envolver(err)
}

// recortarPara limita o trecho impresso, marcando o corte.
//
// Uma linha de diário pode ter centenas de caracteres; imprimir tudo esconde a
// diferença em vez de mostrá-la. A posição exata sai na runa, logo abaixo.
func recortarPara(s string, maxRunas int) string {
	r := []rune(s)
	if len(r) <= maxRunas {
		return s
	}
	return string(r[:maxRunas]) + "…"
}

func envolver(err error) error {
	if err != nil {
		return fmt.Errorf("escrevendo o relatório: %w", err)
	}
	return nil
}
