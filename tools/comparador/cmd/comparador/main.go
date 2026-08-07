// Comando comparador confronta a implementação Go com o corpus dourado nas
// cinco camadas da fase F12 e emite o relatório em texto e em JSON.
//
//	go run ./tools/comparador/cmd/comparador \
//	    -corpus test/testdata/corpus \
//	    -esperado test/testdata/expected \
//	    -json build/paridade.json
//
// O código de saída é o que a integração contínua observa:
//
//	0  aprovado — zero divergência de recortes
//	1  reprovado — há divergência de recortes
//	2  falha de execução (corpus ausente, oráculo ilegível)
//
// A distinção entre 1 e 2 é o ponto: "não consegui medir" não é "medi e está
// errado", e tratá-los igual esconde um corpus que deixou de ser gerado.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/gluizcortez/projetorust/tools/comparador"
)

const (
	saidaAprovado  = 0
	saidaReprovado = 1
	saidaFalha     = 2
)

func main() {
	os.Exit(executar())
}

func executar() int {
	var (
		dirCorpus   = flag.String("corpus", "test/testdata/corpus", "diretório do corpus")
		dirEsperado = flag.String("esperado", "test/testdata/expected", "diretório dos oráculos")
		saidaJSON   = flag.String("json", "", "arquivo para o relatório em JSON (vazio: não escreve)")
		silencioso  = flag.Bool("silencioso", false, "não imprime o relatório em texto")
	)
	flag.Parse()

	if _, err := os.Stat(*dirEsperado); err != nil {
		fmt.Fprintf(os.Stderr, //nolint:forbidigo // saída do comando
			"oráculos ausentes em %s (%v)\ngere o corpus com:  make corpus\n", *dirEsperado, err)
		return saidaFalha
	}

	rel, err := comparador.Executar(context.Background(), comparador.Opcoes{
		DirCorpus:   *dirCorpus,
		DirEsperado: *dirEsperado,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "comparando: %v\n", err) //nolint:forbidigo // saída do comando
		return saidaFalha
	}

	if !*silencioso {
		if err := comparador.EscreverTexto(os.Stdout, rel); err != nil {
			fmt.Fprintf(os.Stderr, "%v\n", err) //nolint:forbidigo // saída do comando
			return saidaFalha
		}
	}

	if *saidaJSON != "" {
		if err := escreverArquivo(*saidaJSON, rel); err != nil {
			fmt.Fprintf(os.Stderr, "%v\n", err) //nolint:forbidigo // saída do comando
			return saidaFalha
		}
	}

	if !rel.Aprovado() {
		return saidaReprovado
	}
	return saidaAprovado
}

func escreverArquivo(caminho string, rel comparador.Relatorio) error {
	if dir := filepath.Dir(caminho); dir != "." {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			return fmt.Errorf("criando %s: %w", dir, err)
		}
	}

	f, err := os.Create(caminho) //nolint:gosec // caminho vindo de bandeira do operador
	if err != nil {
		return fmt.Errorf("criando %s: %w", caminho, err)
	}
	defer func() { _ = f.Close() }()

	if err := comparador.EscreverJSON(f, rel); err != nil {
		return err
	}
	return nil
}
