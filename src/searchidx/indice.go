package searchidx

import (
	"context"
	"fmt"
	"math"
	"strings"

	"github.com/gluizcortez/projetorust/src/domain"
)

// ocorrencia é a posição de um termo: em que página, e em que posição ordinal
// DENTRO daquela página.
//
// A posição é o índice do termo no fluxo produzido por Tokenizar, contando a
// partir de zero e contando apenas os termos que sobreviveram ao descarte por
// comprimento — é a mesma numeração que o Tantivy usa, e é o que faz a busca
// de frase casar "JOAO   SILVA" e "JOAO, SILVA" mas não "JOAOSILVA".
type ocorrencia struct {
	pagina  uint64
	posicao uint32
}

// menorQue ordena por (página, posição). As listas de ocorrência são mantidas
// nesta ordem, que é a que a interseção posicional consome.
func (o ocorrencia) menorQue(outra ocorrencia) bool {
	if o.pagina != outra.pagina {
		return o.pagina < outra.pagina
	}
	return o.posicao < outra.posicao
}

// Indexador constrói índices posicionais em memória.
//
// Substitui o Tantivy. O uso real do legado é estreito — dois campos, um
// documento por página, uma única forma de consulta (frase exata), pontuação
// ignorada, nada persistido —, e reproduzir essa fatia é menos superfície de
// divergência do que domar um mecanismo completo.
//
// O cache de filtros vive AQUI, e não no índice, porque as expressões de perfil
// se repetem entre documentos: compilar uma vez por processo, não uma vez por
// importação. Seguro para uso concorrente.
type Indexador struct {
	filtros *cacheDeFiltros
}

// NovoIndexador devolve um indexador pronto para uso.
func NovoIndexador() *Indexador {
	return &Indexador{filtros: novoCacheDeFiltros()}
}

var _ domain.Indexador = (*Indexador)(nil)

// Construir indexa as páginas, NUMERANDO A PARTIR DE 1
// (reference/main.rs:526: `doc.add_u64(page_field, (i + 1) as u64)`).
//
// Fatia vazia devolve um índice vazio, não erro — é o caso do PDF truncado,
// que o legado aceita e conclui com zero recortes (INV-P20).
func (ix *Indexador) Construir(ctx context.Context, paginas []string) (domain.Indice, error) {
	if len(paginas) > math.MaxUint32 {
		return nil, fmt.Errorf("%w: %d páginas excedem o limite de numeração",
			domain.ErrIndiceIndisponivel, len(paginas))
	}

	novo := &indice{
		postings: make(map[string][]ocorrencia, capacidadeInicialDoVocabulario),
		textos:   paginas,
		filtros:  ix.filtros,
	}

	// O contador de página é incrementado, nunca convertido de `i`: a conversão
	// de int para uint64 seria correta — o limite acima garante — mas exigiria
	// suprimir o aviso do gosec, e contar é mais simples que justificar.
	var pagina uint64

	for i, texto := range paginas {
		// Cancelamento entre páginas: um documento de mil páginas não deve
		// sobreviver ao desligamento do processo.
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("%w: indexação interrompida na página %d: %w",
				domain.ErrIndiceIndisponivel, i+1, err)
		}

		pagina++
		// COM POSIÇÃO: o termo descartado por comprimento deixa um BURACO na
		// numeração, como no Tantivy. Numerar pelo índice da fatia filtrada
		// faria a frase casar por cima do termo longo. Ver TokenizarComPosicao.
		termos := TokenizarComPosicao(texto)
		if len(termos) > 0 && termos[len(termos)-1].Posicao > math.MaxUint32 {
			return nil, fmt.Errorf("%w: página %d tem posição %d, acima do limite",
				domain.ErrIndiceIndisponivel, pagina, termos[len(termos)-1].Posicao)
		}

		for _, t := range termos {
			novo.postings[t.Termo] = append(novo.postings[t.Termo], ocorrencia{
				pagina: pagina,
				//nolint:gosec // o limite acima garante que a posição cabe em uint32
				posicao: uint32(t.Posicao),
			})
		}
	}

	return novo, nil
}

// capacidadeInicialDoVocabulario é a capacidade inicial do mapa de ocorrências.
//
// É um valor FIXO e modesto, não uma estimativa a partir do tamanho do texto.
// A primeira versão desta fase estimava linearmente nos bytes e superestimava o
// vocabulário em 420 vezes num documento de 500 páginas — 220.522 posições para
// 524 termos distintos —, o que sozinho respondia por dezenas de megabytes de
// mapa ocioso.
//
// O erro é estrutural, não de calibração: o vocabulário cresce de forma
// SUBLINEAR no volume de texto (lei de Heaps), então nenhum divisor constante
// serve. As duas medições disponíveis mostram a distância entre os regimes:
//
//	corpus real da fase F0   24 KB de texto → 330 termos distintos
//	500 páginas sintéticas   2,6 MB         → 524 termos distintos
//
// O crescimento do mapa é amortizado, e o custo de recópia para alguns milhares
// de termos é irrelevante perto do custo de tokenizar. Dimensionar de verdade
// depende de medir vocabulário em diário oficial REAL — a decisão aberta D-11.
const capacidadeInicialDoVocabulario = 4096

// indice é o índice posicional de um documento, descartado ao fim da
// importação.
//
// Entre `Construir` e `Fechar` o índice é IMUTÁVEL, e é isso que torna `Frase`
// segura para uso concorrente sem sincronização alguma. `Fechar` solta as
// referências e portanto ESCREVE: é a única operação com regra de posse, e vale
// para ela o mesmo contrato de qualquer `io.Closer` — quem fecha é o dono, e
// depois de todas as buscas terem retornado.
//
// Fechar duas vezes é inofensivo, e buscar depois de fechar devolve fatia vazia
// sem erro, não pânico.
type indice struct {
	// postings mapeia termo para suas ocorrências, ordenadas por
	// (página, posição) — a ordem em que a construção as insere.
	postings map[string][]ocorrencia

	// textos é o texto integral de cada página. O índice 0 é a PÁGINA 1.
	textos []string

	// filtros é compartilhado com o Indexador que criou este índice.
	filtros *cacheDeFiltros
}

var _ domain.Indice = (*indice)(nil)

// Frase executa a busca de frase do legado: a sequência de termos em posições
// CONSECUTIVAS, distância zero, sem tolerância.
//
// Reproduz reference/main.rs:361-410, onde a consulta é montada como
// `format!(r#""{key}""#)` — as aspas fazem o QueryParser do Tantivy produzir
// uma PhraseQuery sobre posições de termos.
//
// NÃO é busca de subcadeia (INV-P05). "JOAO SILVA" casa "JOAO   SILVA",
// "JOAO\nSILVA" e "JOAO, SILVA", porque o que separa dois termos é irrelevante
// depois da tokenização; e NÃO casa "JOAOSILVA", que é um único termo.
//
// Uma página gera NO MÁXIMO UM recorte, mesmo com várias ocorrências: no
// Tantivy cada página é um documento e um documento aparece uma vez no
// resultado.
//
// Expressão que tokeniza para vazio devolve fatia vazia e erro nulo (INV-P06).
//
// A ordem do resultado é por página crescente. O legado devolve na ordem do
// TopDocs e o chamador ordena por página logo em seguida (main.rs:285); como
// não há páginas repetidas, as duas ordens convergem para a mesma sequência.
func (ix *indice) Frase(ctx context.Context, expressao string) ([]domain.Recorte, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("%w: busca interrompida: %w", domain.ErrIndiceIndisponivel, err)
	}

	// O MESMO tokenizador aplicado ao texto indexado, que é o que o
	// QueryParser do legado faz com a consulta — inclusive quanto à POSIÇÃO:
	// um termo longo NO MEIO DA EXPRESSÃO também deixa buraco, e a frase passa
	// a exigir a mesma distância no documento.
	termos := TokenizarComPosicao(expressao)
	if len(termos) == 0 {
		return nil, nil
	}

	paginas := ix.paginasComFrase(termos)
	if len(paginas) == 0 {
		return nil, nil
	}

	// PARIDADE — o filtro só existe quando a expressão CRUA contém `&`
	// (main.rs:392), e só é compilado porque há pelo menos um acerto
	// (INV-P23). Ordem invertida transformaria sucesso em falha.
	if strings.ContainsRune(expressao, '&') {
		filtrado, err := ix.aplicarFiltroDoOperador(expressao, paginas)
		if err != nil {
			return nil, err
		}
		paginas = filtrado
	}

	recortes := make([]domain.Recorte, 0, len(paginas))
	for _, pagina := range paginas {
		recortes = append(recortes, domain.NovoRecorte(pagina, ix.textos[pagina-1]))
	}
	return recortes, nil
}

// paginasComFrase devolve, em ordem crescente e sem repetição, as páginas em
// que os termos aparecem em posições consecutivas.
func (ix *indice) paginasComFrase(termos []TermoPosicionado) []uint64 {
	// Um termo só: as páginas distintas em que ele ocorre.
	if len(termos) == 1 {
		return paginasDistintas(ix.postings[termos[0].Termo])
	}

	// Vários termos: `candidatos` guarda a ocorrência do ÚLTIMO termo já
	// casado. A cada termo seguinte sobrevivem apenas as continuações na
	// distância que a EXPRESSÃO pede — normalmente 1, e maior quando a própria
	// expressão tem um termo descartado por comprimento no meio.
	//
	// A cópia é obrigatória: `avancar` escreve no arranjo que recebe, e o
	// primeiro deles seria a lista de ocorrências DO PRÓPRIO ÍNDICE, que é
	// imutável e compartilhada entre buscas concorrentes.
	candidatos := append([]ocorrencia(nil), ix.postings[termos[0].Termo]...)
	for k := 1; k < len(termos) && len(candidatos) > 0; k++ {
		distancia := termos[k].Posicao - termos[k-1].Posicao
		if distancia <= 0 || distancia > math.MaxUint32 {
			// Inalcançável: TokenizarComPosicao emite posições estritamente
			// crescentes. A guarda evita que uma mudança futura produza um
			// deslocamento negativo, que faria `avancar` procurar para trás.
			return nil
		}
		candidatos = avancar(candidatos, ix.postings[termos[k].Termo], uint32(distancia)) //nolint:gosec // limite conferido acima
	}

	return paginasDistintas(candidatos)
}

// avancar conserva as ocorrências de `atuais` que têm continuação a
// `distancia` posições em `seguintes`, devolvendo já a posição avançada.
//
// `distancia` é 1 no caso comum. Ela é maior quando a EXPRESSÃO tem um termo
// descartado por comprimento entre dois termos casados: o buraco na numeração
// da consulta precisa do mesmo buraco no documento.
//
// As duas listas chegam ordenadas por (página, posição) e o alvo cresce de
// forma monótona, então a interseção é uma fusão linear com um único ponteiro
// que nunca retrocede — não uma busca por elemento, muito menos um produto
// cartesiano.
func avancar(atuais, seguintes []ocorrencia, distancia uint32) []ocorrencia {
	// Reaproveita o arranjo de entrada, que é uma cópia local: a escrita nunca
	// ultrapassa a leitura.
	mantidos := atuais[:0]

	j := 0
	for _, atual := range atuais {
		if atual.posicao > math.MaxUint32-distancia {
			continue // a posição alvo transbordaria
		}
		alvo := ocorrencia{pagina: atual.pagina, posicao: atual.posicao + distancia}

		for j < len(seguintes) && seguintes[j].menorQue(alvo) {
			j++
		}
		if j < len(seguintes) && seguintes[j] == alvo {
			mantidos = append(mantidos, alvo)
		}
	}

	return mantidos
}

// paginasDistintas colapsa uma lista de ocorrências ordenada por
// (página, posição) na sequência de páginas distintas.
func paginasDistintas(ocorrencias []ocorrencia) []uint64 {
	if len(ocorrencias) == 0 {
		return nil
	}
	paginas := make([]uint64, 0, 8)
	anterior := uint64(0)
	for _, o := range ocorrencias {
		if o.pagina != anterior {
			paginas = append(paginas, o.pagina)
			anterior = o.pagina
		}
	}
	return paginas
}

// aplicarFiltroDoOperador descarta as páginas cujo texto não casa com o filtro
// do operador `&` (main.rs:392-398).
func (ix *indice) aplicarFiltroDoOperador(expressao string, paginas []uint64) ([]uint64, error) {
	re, err := ix.filtros.Obter(expressao)
	if err != nil {
		return nil, err
	}

	mantidas := paginas[:0]
	for _, pagina := range paginas {
		if re.MatchString(ix.textos[pagina-1]) {
			mantidas = append(mantidas, pagina)
		}
	}
	return mantidas, nil
}

// Fechar libera as estruturas do índice.
//
// O legado não tem equivalente: o `Index` do Tantivy é derrubado com a tarefa,
// junto com o escritor de 500 MB. Aqui só existe o que o coletor de lixo
// recolhe, e soltar as referências apenas antecipa isso.
func (ix *indice) Fechar() error {
	ix.postings = nil
	ix.textos = nil
	return nil
}
