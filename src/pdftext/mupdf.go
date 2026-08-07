package pdftext

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/gen2brain/go-fitz"
	"golang.org/x/net/html"

	"github.com/gluizcortez/projetorust/src/domain"
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
