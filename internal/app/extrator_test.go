package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gluizcortez/projetorust/internal/adapter/pdftext"
)

// TestExtratorDeProducaoNormaliza é o guarda da regressão mais cara já
// encontrada no projeto.
//
// # O que aconteceu
//
// Da fase F5 até a F11, a raiz de composição injetava `pdftext.NovoExtrator()`,
// que devolve texto BRUTO. O serviço indexava sem junção de hífens (INV-P02) e
// sem remoção de diacríticos (INV-P07), e `pdftext.Normalizar` nunca era
// chamado em produção. A suíte inteira ficava verde: cada teste de camada se
// alimentava do ORÁCULO, não da saída da camada anterior em Go, e a costura
// entre extração e normalização não era exercitada por ninguém.
//
// # Por que este teste e não outro
//
// `test/parity/pipeline_test.go` monta o pipeline por conta própria e passa o
// extrator certo — ele prova que o PIPELINE está correto, não que a MONTAGEM
// está. Trocar `extratorDeProducao` de volta pelo extrator cru deixaria aquele
// teste verde. Este aqui olha exatamente para o que a raiz de composição
// entrega.
func TestExtratorDeProducaoNormaliza(t *testing.T) {
	// O documento tem uma palavra partida por hífen no fim da linha e
	// acentuação — as duas transformações que a normalização aplica.
	//
	// É fixture DESTE pacote, e não do corpus dourado: o corpus saiu do
	// repositório junto com o ferramental de paridade, e sem um PDF aqui este
	// teste passaria a PULAR em silêncio — o que é o mesmo que não existir,
	// justamente para o defeito mais caro já encontrado no projeto.
	caminho := filepath.Join("testdata", "diario-com-hifen.pdf")

	conteudo, err := os.ReadFile(caminho) //nolint:gosec // caminho fixo, dentro do pacote
	if err != nil {
		t.Fatalf("fixture ausente: %v", err)
	}

	ctx := context.Background()

	brutas, err := pdftext.NovoExtrator().ExtrairPaginas(ctx, conteudo)
	if err != nil {
		t.Fatalf("extração bruta: %v", err)
	}
	obtidas, err := extratorDeProducao().ExtrairPaginas(ctx, conteudo)
	if err != nil {
		t.Fatalf("extração de produção: %v", err)
	}

	if len(brutas) != len(obtidas) {
		t.Fatalf("páginas: bruta %d, produção %d", len(brutas), len(obtidas))
	}

	// 1 — o que a raiz entrega tem de ser IGUAL a normalizar o bruto.
	for i := range brutas {
		if esperado := pdftext.Normalizar(brutas[i]); obtidas[i] != esperado {
			t.Errorf("página %d: a raiz de composição não entrega texto normalizado", i+1)
		}
	}

	// 2 — e tem de ser DIFERENTE do bruto, senão a asserção acima passaria com
	// uma normalização que não faz nada.
	iguais := 0
	for i := range brutas {
		if brutas[i] == obtidas[i] {
			iguais++
		}
	}
	if iguais == len(brutas) {
		t.Fatal("o texto de produção é idêntico ao bruto: a normalização não está sendo aplicada")
	}

	// 3 — a evidência concreta: o hífen do fim da linha some, e a palavra é
	// rejuntada. É o caso que o documento do corpus existe para exercitar.
	if strings.Contains(obtidas[0], "conti-") {
		t.Error("a junção de hífens (INV-P02) não foi aplicada: o texto ainda tem \"conti-\"")
	}
	if !strings.Contains(obtidas[0], "continuacao") {
		t.Error("a palavra partida não foi rejuntada em \"continuacao\"")
	}
}
