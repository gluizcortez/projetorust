package pdftext

import (
	"context"
	"fmt"

	"github.com/gluizcortez/projetorust/internal/domain"
)

// ExtratorNormalizado é o Extrator com a normalização da fase F6 acoplada.
//
// # Por que este tipo existe
//
// `domain.ExtratorTexto` promete texto **já normalizado** — é o que o legado
// entrega, porque `criar_indice` (reference/main.rs:489-505) junta hífens e
// remove diacríticos ANTES de indexar, na mesma função que extrai. O `Extrator`
// cru devolve o texto bruto de propósito, para que uma falha de extração seja
// distinguível de uma de normalização no diagnóstico e no corpus
// (`*.paginas-brutas.json` contra `*.paginas.json`).
//
// Este decorador é o que liga os dois. A fase F5 já o previa, com estas
// palavras: "Essas duas transformações são a fase F6 e entram como um DECORADOR
// sobre este tipo, não como um passo escondido aqui."
//
// # O defeito que a ausência dele causava
//
// Ele NÃO foi construído na F6, e a raiz de composição injetava o extrator cru.
// O serviço indexava texto **não normalizado**, e `pdftext.Normalizar` era
// código morto em produção. Consequências medidas sobre o corpus dourado:
//
//   - INV-P02: palavra partida por hífen no fim da linha não era rejuntada, e a
//     busca de frase que a atravessa deixava de encontrar;
//   - INV-P07: os diacríticos permaneciam, então expressão sem acento — a forma
//     em que os perfis estão cadastrados — não casava com o texto;
//   - INV-P19 **INVERTIDA**: expressão ACENTUADA, que no legado é inerte,
//     passava a casar; e a sem acento, que funciona, deixava de casar.
//
// Nenhum teste anterior pegava isso porque cada camada se alimentava do
// ORÁCULO, não da saída da camada anterior em Go: F5 comparava extração contra
// `paginas-brutas`, F6 normalizava o oráculo bruto, F7 buscava sobre o oráculo
// normalizado. A costura entre elas nunca era exercitada. Ver
// `test/parity/pipeline_test.go`, que é o teste que a expôs, e o relatório da
// fase F12.
type ExtratorNormalizado struct {
	interno *Extrator
}

// É este tipo — e não o Extrator cru — que satisfaz a porta por completo.
var _ domain.ExtratorTexto = (*ExtratorNormalizado)(nil)

// NovoExtratorNormalizado é o extrator de PRODUÇÃO.
//
// A raiz de composição deve usar este, nunca `NovoExtrator`: aquele existe para
// o teste de paridade da extração, que mede o texto antes da normalização.
func NovoExtratorNormalizado() *ExtratorNormalizado {
	return &ExtratorNormalizado{interno: NovoExtrator()}
}

// ExtrairPaginas devolve o texto JÁ NORMALIZADO de cada página.
//
// A ordem das transformações é normativa e está em INV-P08: junção de hífens
// ANTES da remoção de diacríticos, porque a classe de caracteres de palavra da
// junção precisa casar letras acentuadas. Ver Normalizar.
//
// Todo o comportamento com entrada inválida vem do extrator interno, incluindo
// INV-P20: PDF truncado devolve fatia vazia e erro nil.
func (e *ExtratorNormalizado) ExtrairPaginas(
	ctx context.Context, conteudo []byte,
) ([]string, error) {
	brutas, err := e.interno.ExtrairPaginas(ctx, conteudo)
	if err != nil {
		return nil, fmt.Errorf("extraindo para normalizar: %w", err)
	}

	// A normalização é feita no lugar: a fatia acabou de ser criada pelo
	// extrator interno e ninguém mais a referencia.
	for i, bruta := range brutas {
		brutas[i] = Normalizar(bruta)
	}
	return brutas, nil
}
