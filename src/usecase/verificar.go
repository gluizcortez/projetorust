package usecase

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strings"

	"github.com/gluizcortez/projetorust/src/domain"
	"github.com/gluizcortez/projetorust/src/pdftext"
	"github.com/gluizcortez/projetorust/src/searchidx"
)

// -------------------------------------------------------------------------
// Verificação manual — fase F14, EVOLUÇÃO atrás de VERIFICACAO_ENDPOINT
// -------------------------------------------------------------------------
//
// Este caso de uso NÃO existe no legado. Ele responde uma pergunta que o time
// técnico faz o tempo todo e que hoje só se responde submetendo o documento de
// verdade e depois consultando o banco:
//
//	"esta expressão aparece neste PDF? em que página? e se não aparece, por quê?"
//
// # A regra que ele obedece
//
// A busca é EXATAMENTE a mesma de `/pdf`: mesmo extrator, mesmo normalizador,
// mesmo indexador, mesma `Indice.Frase`. Isso não é economia de código — é o
// ponto. Uma ferramenta de diagnóstico que buscasse de outro jeito mentiria
// sobre o serviço, e o time gastaria o dobro do tempo perseguindo uma
// diferença que só existe na ferramenta.
//
// Em particular, ela REPRODUZ os defeitos preservados. Expressão acentuada não
// casa aqui, como não casa lá (INV-P19). O que a ferramenta faz de diferente é
// **dizer que foi por isso** — ver `diagnosticar`.
//
// # O que ele NÃO faz
//
// Não toca no banco. Nenhum repositório entra por dependência: as três portas
// que ele usa — extrator, indexador e índice — são todas em memória. Nada é
// registrado, nenhum status muda, nenhuma linha é escrita. Submeter um
// documento aqui é uma operação de leitura pura sobre bytes que o cliente
// acabou de enviar.

// ComandoVerificar é a entrada da verificação manual.
type ComandoVerificar struct {
	// Submissao passa pela MESMA validação de /pdf, com as mesmas críticas na
	// mesma ordem. Um diagnóstico que aceitasse entrada que o serviço recusa
	// não diagnosticaria o serviço.
	Submissao domain.SubmissaoPDF

	// Conteudo é o PDF já lido em memória.
	Conteudo []byte

	// Expressao é o que se procura — um nome, um número de processo, uma razão
	// social. Vai para a busca EXATAMENTE como veio, porque é assim que a
	// expressão cadastrada de um perfil vai.
	Expressao string
}

// OcorrenciaVerificada é uma página em que a expressão foi encontrada.
type OcorrenciaVerificada struct {
	// Pagina começa em 1, como em tb_recorte.nr_pagina.
	Pagina uint64

	// Trecho é o texto ao redor da ocorrência, para conferência visual.
	//
	// Vazio quando a expressão casou por FRASE mas não aparece como subcadeia
	// contígua no texto — o que acontece quando há pontuação entre os termos.
	// Nesse caso a página casou de verdade; só não há um trecho literal a
	// mostrar. Ver `recortarTrecho`.
	Trecho string
}

// ResultadoVerificacao é o que a verificação devolve.
//
// O campo que mais importa quando algo dá errado é `Diagnostico`, não
// `Ocorrencias`.
type ResultadoVerificacao struct {
	// Expressao é o texto consultado, como veio.
	Expressao string

	// TermosBuscados são os termos que a expressão produziu depois da
	// tokenização — é o que a busca de fato procurou.
	//
	// Ver isto responde metade das dúvidas sozinho: uma expressão que vira
	// zero termos nunca casa, e uma que perde um termo por comprimento explica
	// um resultado inesperado.
	TermosBuscados []string

	// TotalPaginas é quantas páginas o documento tem.
	TotalPaginas int

	// Ocorrencias são as páginas em que a expressão foi encontrada, em ordem.
	// Uma página aparece no máximo uma vez, como no laço de recorte (INV-P12).
	Ocorrencias []OcorrenciaVerificada

	// Diagnostico traz o motivo provável quando nada foi encontrado, e
	// advertências mesmo quando algo foi. Vazio quer dizer "nada a apontar".
	Diagnostico []string
}

// Encontrou informa se houve ao menos uma ocorrência.
func (r ResultadoVerificacao) Encontrou() bool { return len(r.Ocorrencias) > 0 }

// ErrExpressaoVazia indica expressão ausente ou só com espaços.
var ErrExpressaoVazia = errors.New("expressão de verificação vazia")

// DependenciasDaVerificacao reúne o que a verificação precisa.
//
// A ausência de qualquer porta de PERSISTÊNCIA nesta lista é a garantia
// estrutural de que o caso de uso não escreve: ele não tem por onde.
type DependenciasDaVerificacao struct {
	Extrator  domain.ExtratorTexto
	Indexador domain.Indexador
	Logger    *slog.Logger
}

// Verificacao executa a busca de uma expressão num PDF, sem persistir nada.
type Verificacao struct {
	extrator  domain.ExtratorTexto
	indexador domain.Indexador
	logger    *slog.Logger
}

// NovaVerificacao monta o caso de uso.
func NovaVerificacao(d DependenciasDaVerificacao) (*Verificacao, error) {
	var faltando []string
	if d.Extrator == nil {
		faltando = append(faltando, "Extrator")
	}
	if d.Indexador == nil {
		faltando = append(faltando, "Indexador")
	}
	if d.Logger == nil {
		faltando = append(faltando, "Logger")
	}
	if len(faltando) > 0 {
		return nil, fmt.Errorf("usecase: verificação exige %s", strings.Join(faltando, ", "))
	}
	return &Verificacao{extrator: d.Extrator, indexador: d.Indexador, logger: d.Logger}, nil
}

// Executar valida a submissão, indexa o documento e procura a expressão.
//
// A ordem reproduz a de `/pdf` até a busca: validar, extrair, indexar,
// procurar. O que muda é o desfecho — em vez de gravar recortes, devolve o que
// achou.
func (v *Verificacao) Executar(
	ctx context.Context, cmd ComandoVerificar,
) (ResultadoVerificacao, error) {
	expressao := strings.TrimSpace(cmd.Expressao)
	if expressao == "" {
		return ResultadoVerificacao{}, ErrExpressaoVazia
	}

	// A MESMA validação de /pdf, com as mesmas críticas na mesma ordem.
	if _, criticas := cmd.Submissao.Validar(); !criticas.Vazio() {
		return ResultadoVerificacao{}, domain.NovoErroDeValidacao(criticas)
	}

	paginas, err := v.extrator.ExtrairPaginas(ctx, cmd.Conteudo)
	if err != nil {
		return ResultadoVerificacao{}, fmt.Errorf("extraindo o documento: %w", err)
	}

	indice, err := v.indexador.Construir(ctx, paginas)
	if err != nil {
		return ResultadoVerificacao{}, fmt.Errorf("indexando o documento: %w", err)
	}
	defer func() {
		if err := indice.Fechar(); err != nil {
			v.logger.WarnContext(ctx, "falha ao fechar o índice da verificação",
				slog.Any("erro", err))
		}
	}()

	recortes, err := indice.Frase(ctx, expressao)
	if err != nil {
		// Expressão que não compila como filtro chega aqui. Não é erro do
		// serviço — é entrada inválida —, e o manipulador a traduz em 400.
		return ResultadoVerificacao{}, fmt.Errorf("%w: %w", ErrExpressaoInvalida, err)
	}

	resultado := ResultadoVerificacao{
		Expressao:      expressao,
		TermosBuscados: termosDe(expressao),
		TotalPaginas:   len(paginas),
	}
	for _, r := range recortes {
		resultado.Ocorrencias = append(resultado.Ocorrencias, OcorrenciaVerificada{
			Pagina: r.Pagina,
			Trecho: recortarTrecho(r.Destaque, expressao),
		})
	}
	resultado.Diagnostico = diagnosticar(expressao, resultado)

	v.logger.InfoContext(ctx, "verificação manual executada",
		slog.String("expressao", expressao),
		slog.Int("paginas", len(paginas)),
		slog.Int("ocorrencias", len(resultado.Ocorrencias)))

	return resultado, nil
}

// ErrExpressaoInvalida indica expressão que o filtro não aceita.
var ErrExpressaoInvalida = errors.New("expressão de verificação inválida")

// termosDe devolve os termos que a expressão produz na tokenização.
//
// É a MESMA função que o índice usa, e é por isso que serve de diagnóstico: o
// que aparece aqui é literalmente o que a busca procurou.
func termosDe(expressao string) []string {
	posicionados := searchidx.TokenizarComPosicao(expressao)
	termos := make([]string, 0, len(posicionados))
	for _, p := range posicionados {
		termos = append(termos, p.Termo)
	}
	return termos
}

// larguraDoTrecho é quanto texto acompanha a ocorrência, de cada lado.
//
// 120 caracteres dão uma ou duas linhas de contexto — o suficiente para
// reconhecer o parágrafo sem transformar a resposta num despejo da página, que
// tem ~17 mil caracteres num diário real.
const larguraDoTrecho = 120

// separadorEntreTermos casa a pontuação e o espaço que ficam ENTRE dois termos.
//
// Não é a definição de alfanumérico do serviço — essa vive em
// searchidx.ehAlfanumerico, com 1.265 exceções medidas. Aqui basta uma
// aproximação, porque esta função só localiza o trecho para exibição; quem
// decide se a página casou é `Indice.Frase`.
var separadorEntreTermos = regexp.MustCompile(`[^\p{L}\p{N}]+`)

// recortarTrecho devolve o texto ao redor da primeira ocorrência.
//
// # Por que não é uma busca de subcadeia
//
// A busca do serviço é por FRASE — termos em posições consecutivas —, não por
// subcadeia (INV-P05). No documento, `Dra. Deborah da Silva Felix` tem um ponto
// que não gera termo: a frase casa, mas a subcadeia `dra deborah da silva
// felix` não existe no texto. Procurar literalmente devolveria vazio no caso
// MAIS COMUM — nome com abreviação.
//
// Então a localização é feita com os TERMOS, unidos por um separador que aceita
// qualquer pontuação entre eles. É a mesma forma da frase que o índice casou.
//
// # Ainda pode vir vazio
//
// Vem, quando o índice casou por um caminho que a aproximação acima não
// reproduz. A ocorrência continua válida — o manipulador HTTP omite o campo.
// Localizar sempre exigiria o índice devolver deslocamentos de byte, que ele
// não guarda, e mudaria uma estrutura que está sob paridade.
func recortarTrecho(pagina, expressao string) string {
	termos := termosDe(expressao)
	if len(termos) == 0 {
		return ""
	}

	// Os termos já vêm em minúsculas e sem acento; o texto da página vem
	// normalizado mas com a caixa original — daí o `(?i)`.
	partes := make([]string, 0, len(termos))
	for _, termo := range termos {
		partes = append(partes, regexp.QuoteMeta(termo))
	}
	padrao, err := regexp.Compile(`(?i)` + strings.Join(partes, separadorEntreTermos.String()))
	if err != nil {
		// Inalcançável: os termos passaram por QuoteMeta.
		return ""
	}

	faixa := padrao.FindStringIndex(pagina)
	if faixa == nil {
		return ""
	}

	runas := []rune(pagina)
	// Converte deslocamento de BYTES em deslocamento de RUNAS, para não cortar
	// um caractere ao meio.
	inicioRuna := len([]rune(pagina[:faixa[0]]))
	fimRuna := len([]rune(pagina[:faixa[1]]))

	de := max(0, inicioRuna-larguraDoTrecho)
	ate := min(len(runas), fimRuna+larguraDoTrecho)

	trecho := strings.TrimSpace(string(runas[de:ate]))
	if de > 0 {
		trecho = "…" + trecho
	}
	if ate < len(runas) {
		trecho += "…"
	}
	return trecho
}

// diagnosticar explica um resultado inesperado.
//
// É o que separa esta ferramenta de um `grep`: quando a expressão não casa, o
// time precisa saber SE é a expressão, SE é o documento, ou SE é um defeito
// preservado do serviço agindo como sempre agiu.
//
// As advertências valem mesmo quando algo foi encontrado — uma expressão
// acentuada que casou é sinal de que a normalização mudou, e isso é grave.
func diagnosticar(expressao string, r ResultadoVerificacao) []string {
	var notas []string

	if temDiacritico(expressao) {
		if r.Encontrou() {
			notas = append(notas, "A expressão tem acento e MESMO ASSIM casou. "+
				"Isso contraria INV-P19 e sugere que a normalização do texto mudou — vale investigar.")
		} else {
			notas = append(notas, "A expressão tem ACENTO. O texto do documento perde os acentos "+
				"na indexação e a expressão não passa pela mesma normalização, então ela nunca casa. "+
				"É defeito preservado do serviço original (INV-P19). Tente sem acento.")
		}
	}

	if len(r.TermosBuscados) == 0 {
		notas = append(notas, "A expressão não produziu termo algum — só pontuação ou símbolos. "+
			"Uma expressão assim nunca casa (INV-P06).")
	}

	for _, t := range strings.Fields(expressao) {
		if len(t) >= searchidx.LimiteComprimentoTermo {
			notas = append(notas, fmt.Sprintf(
				"O termo %q tem %d bytes e é DESCARTADO na indexação, porque o limite é %d. "+
					"Ele não participa da busca e ainda deixa um buraco na numeração de posições (INV-P03).",
				t, len(t), searchidx.LimiteComprimentoTermo))
		}
	}

	if r.TotalPaginas == 0 {
		notas = append(notas, "O documento não produziu página alguma de texto. "+
			"Pode ser um PDF de imagens sem camada de texto, ou um arquivo truncado.")
	}

	if !r.Encontrou() && len(notas) == 0 {
		notas = append(notas, "A expressão não foi encontrada e nada de anômalo foi detectado nela. "+
			"Confira os termos em `termos_buscados`: a busca é por FRASE — os termos precisam "+
			"aparecer em sequência, na ordem dada.")
	}

	return notas
}

// temDiacritico informa se o texto muda ao perder os acentos.
func temDiacritico(s string) bool {
	return pdftext.RemoverDiacriticos(s) != s
}
