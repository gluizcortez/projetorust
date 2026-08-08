// A extração de texto do PDF, em dois tipos que se compõem.
//
// `Extrator` devolve o texto BRUTO, como o MuPDF o entrega. `ExtratorNormalizado`
// é o decorador que aplica a normalização da fase F6 por cima — e é ELE que a
// raiz de composição injeta. Manter os dois no mesmo arquivo torna difícil
// repetir o engano da F12, em que o cru foi injetado por seis fases.
package pdftext

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/gen2brain/go-fitz"
	"github.com/gluizcortez/projetorust/src/domain"
	"golang.org/x/net/html"
)

// Extrator obtém o texto de um PDF reproduzindo o laço do legado.
//
// ESTRATÉGIA — ver docs/F5-AVALIACAO-EXTRACAO.md para a medição completa.
//
// O legado percorre blocos → linhas → caracteres do fz_stext_page
// (reference/main.rs:489-505), apara cada LINHA e lhe acrescenta um `\n`. A
// ligação Go para MuPDF não expõe essa estrutura diretamente, mas o escritor
// HTML do próprio MuPDF emite UM `<p>` POR LINHA do stext — exatamente a
// granularidade necessária.
//
// A saída de `doc.Text()` NÃO serve: ela junta as linhas de um bloco com
// espaço e aplica a de-hifenização própria do MuPDF, o que quebra a junção de
// hífens do legado (INV-P02) e a busca de frase (INV-P05). Medido: 20 de 154
// páginas contra 159 de 159.
// O Extrator devolve o texto BRUTO — antes da junção de hífens e da remoção de
// diacríticos. Essas duas transformações são a fase F6 e entram como um
// DECORADOR sobre este tipo, não como um passo escondido aqui: separá-las é o
// que torna uma falha de extração distinguível de uma de normalização, tanto no
// diagnóstico quanto no corpus (`*.paginas-brutas.json` contra `*.paginas.json`).
//
// # ESTE TIPO NÃO É O DE PRODUÇÃO
//
// Quem satisfaz `domain.ExtratorTexto` por completo — que promete texto JÁ
// NORMALIZADO — é `ExtratorNormalizado`, e é ele que a raiz de composição
// injeta. Este aqui é um ESTÁGIO, usado pelo teste de paridade da extração.
//
// A afirmação `var _ domain.ExtratorTexto = (*Extrator)(nil)` foi REMOVIDA de
// propósito: ela dizia que o extrator cru cumpre um contrato que ele não
// cumpre, e foi o que permitiu à raiz de composição injetá-lo por engano
// durante seis fases. Ver o cabeçalho de decorador.go.
type Extrator struct{}

// NovoExtrator cria o extrator de texto BRUTO.
//
// Para produção, use NovoExtratorNormalizado.
func NovoExtrator() *Extrator {
	return &Extrator{}
}

// ExtrairPaginas devolve o texto BRUTO de cada página, na ordem do documento.
//
// Comportamento com entrada inválida, MEDIDO na fase F0 e normativo
// (INV-P20):
//
//	PDF truncado   → fatia VAZIA, erro nil. O MuPDF aceita o arquivo e devolve
//	                 zero páginas. A importação termina em status 5, e no banco
//	                 fica indistinguível de um diário sem ocorrências.
//	arquivo vazio  → ErrPDFInvalido
//	não é PDF      → ErrPDFInvalido
func (e *Extrator) ExtrairPaginas(ctx context.Context, conteudo []byte) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("extraindo páginas: %w", err)
	}

	doc, err := fitz.NewFromMemory(conteudo)
	if err != nil {
		return nil, fmt.Errorf("%w: abrindo documento: %w", domain.ErrPDFInvalido, err)
	}
	defer func() { _ = doc.Close() }()

	total := doc.NumPage()
	paginas := make([]string, 0, total)

	for i := range total {
		// Cancelamento é verificado ENTRE páginas: um documento de centenas de
		// páginas não pode ignorar o encerramento do processo.
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("extraindo página %d de %d: %w", i+1, total, err)
		}

		bruto, err := doc.HTML(i, false)
		if err != nil {
			return nil, fmt.Errorf("%w: página %d: %w", domain.ErrExtracaoTexto, i+1, err)
		}

		pagina, err := montarPagina(bruto)
		if err != nil {
			return nil, fmt.Errorf("%w: página %d: %w", domain.ErrExtracaoTexto, i+1, err)
		}

		paginas = append(paginas, pagina)
	}

	return paginas, nil
}

// montarPagina remonta o texto da página a partir da saída HTML do MuPDF,
// reproduzindo reference/main.rs:492-505.
//
// Cada `<p>` é uma linha do stext: o texto é aparado, recebe exatamente um
// `\n`, e as linhas são concatenadas SEM separador entre blocos — a fronteira
// de bloco não aparece na saída do legado (INV-P09).
//
// A análise usa um tokenizador de HTML de verdade, não expressão regular:
// atributos de estilo do MuPDF contêm `:` e `;`, e o texto da página pode
// conter `<`, `>` e `&` escapados como entidades.
func montarPagina(documentoHTML string) (string, error) {
	tokenizador := html.NewTokenizer(strings.NewReader(documentoHTML))

	var (
		pagina    strings.Builder
		linha     strings.Builder
		emLinha   bool
		aninhados int
	)

	for {
		switch tokenizador.Next() {
		case html.ErrorToken:
			if err := tokenizador.Err(); err != nil && !errors.Is(err, io.EOF) {
				return "", fmt.Errorf("analisando HTML do MuPDF: %w", err)
			}
			return pagina.String(), nil

		case html.StartTagToken:
			nome, _ := tokenizador.TagName()
			if string(nome) != "p" {
				continue
			}
			if emLinha {
				// `<p>` aninhado não ocorre na saída do MuPDF; contabilizado
				// para que uma mudança de formato apareça como texto estranho
				// em vez de silenciosamente truncar a linha.
				aninhados++
				continue
			}
			emLinha = true
			linha.Reset()

		case html.EndTagToken:
			nome, _ := tokenizador.TagName()
			if string(nome) != "p" || !emLinha {
				continue
			}
			if aninhados > 0 {
				aninhados--
				continue
			}
			pagina.WriteString(strings.TrimSpace(linha.String()))
			pagina.WriteString("\n")
			emLinha = false

		case html.TextToken:
			if emLinha {
				// Text() do tokenizador já desescapa as entidades.
				linha.Write(tokenizador.Text())
			}

		case html.SelfClosingTagToken, html.CommentToken, html.DoctypeToken:
			// Irrelevantes: `<br/>` não aparece na saída do MuPDF, e comentários
			// e doctype não carregam texto.
			continue
		}
	}
}

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
