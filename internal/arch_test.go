package internal

import (
	"errors"
	"go/build"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// caminhoDoModulo é o prefixo de importação do repositório. Conferido contra o
// go.mod para que uma renomeação do módulo não desative silenciosamente esta
// verificação.
const caminhoDoModulo = "github.com/gluizcortez/projetorust"

// pacotesDoNucleo são os que a regra de dependência protege. Eles formam o
// núcleo da arquitetura hexagonal: não podem conhecer infraestrutura.
//
// Os caminhos são relativos a este diretório (internal/), que é o de trabalho
// quando `go test ./internal` executa.
var pacotesDoNucleo = []string{"domain", "usecase"}

// prefixosProibidos são subárvores do próprio módulo que o núcleo não pode
// importar em hipótese alguma — nem em arquivos de teste.
var prefixosProibidos = []string{
	caminhoDoModulo + "/internal/adapter",
	caminhoDoModulo + "/internal/platform",
	caminhoDoModulo + "/cmd",
}

// TestModuloConfere garante que a constante acima não ficou obsoleta.
func TestModuloConfere(t *testing.T) {
	bruto, err := os.ReadFile(filepath.Join("..", "go.mod"))
	if err != nil {
		t.Fatalf("lendo go.mod: %v", err)
	}
	esperado := "module " + caminhoDoModulo
	if !strings.Contains(string(bruto), esperado) {
		t.Fatalf("go.mod não declara %q — a verificação de arquitetura está apontando para o módulo errado", esperado)
	}
}

// TestRegraDeDependencia falha quando internal/domain ou internal/usecase
// importam infraestrutura.
//
// A regra, em uma frase: o núcleo importa a biblioteca padrão, golang.org/x/ e
// ele mesmo. Nada mais.
//
// Arquivos de teste têm uma exceção deliberada e estreita: podem importar
// bibliotecas de terceiros de apoio a teste, porque exigir cobertura alta no
// núcleo (90% em domain, 85% em usecase) sem auxiliares seria uma restrição
// sem contrapartida. A proibição de importar adapter, platform e cmd continua
// valendo para eles — é ela que garante que o núcleo seja testável isolado.
func TestRegraDeDependencia(t *testing.T) {
	for _, raiz := range pacotesDoNucleo {
		err := filepath.WalkDir(raiz, func(caminho string, entrada os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if !entrada.IsDir() {
				return nil
			}

			pacote, err := build.ImportDir(caminho, 0)
			if err != nil {
				var semGo *build.NoGoError
				if errors.As(err, &semGo) {
					return nil // diretório ainda sem código: normal nas fases iniciais
				}
				return err
			}

			verificar(t, caminho, pacote.Imports, false)
			verificar(t, caminho, pacote.TestImports, true)
			verificar(t, caminho, pacote.XTestImports, true)
			return nil
		})
		if err != nil {
			t.Fatalf("percorrendo %s: %v", raiz, err)
		}
	}
}

func verificar(t *testing.T, pacote string, importacoes []string, ehTeste bool) {
	t.Helper()
	for _, imp := range importacoes {
		if motivo := classificar(imp, ehTeste); motivo != "" {
			rotulo := "internal/" + filepath.ToSlash(pacote)
			origem := "código de produção"
			if ehTeste {
				origem = "arquivo de teste"
			}
			t.Errorf(
				"REGRA DE DEPENDÊNCIA VIOLADA\n"+
					"  pacote infrator: %s (%s)\n"+
					"  importação proibida: %q\n"+
					"  motivo: %s\n"+
					"  o núcleo só pode importar a biblioteca padrão, golang.org/x/ e ele mesmo",
				rotulo, origem, imp, motivo,
			)
		}
	}
}

// classificar devolve o motivo da proibição, ou string vazia se a importação
// for permitida.
func classificar(imp string, ehTeste bool) string {
	for _, proibido := range prefixosProibidos {
		if imp == proibido || strings.HasPrefix(imp, proibido+"/") {
			return "o núcleo não pode depender de adaptadores, de plataforma nem de cmd"
		}
	}

	// Pacotes do próprio núcleo são permitidos.
	if strings.HasPrefix(imp, caminhoDoModulo+"/") {
		return ""
	}

	if ehBibliotecaPadrao(imp) {
		return ""
	}

	if imp == "golang.org/x" || strings.HasPrefix(imp, "golang.org/x/") {
		return ""
	}

	if ehTeste {
		return "" // exceção deliberada: auxiliares de teste de terceiros
	}

	return "dependência de terceiros no núcleo"
}

// ehBibliotecaPadrao usa a convenção do Go: um caminho de importação da
// biblioteca padrão não tem ponto no primeiro segmento.
func ehBibliotecaPadrao(imp string) bool {
	primeiro, _, _ := strings.Cut(imp, "/")
	return !strings.Contains(primeiro, ".")
}
