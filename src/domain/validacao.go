// A validação da submissão: as críticas, seus textos literais e os erros
// tipados que o serviço devolve.
//
// Os TEXTOS e a ORDEM em que se acumulam são contrato observável — ver
// docs/ESPECIFICACAO.md §1.4.2. Alterá-los muda o corpo da resposta 400.
package domain

import (
	"errors"
	"strings"
)

// SubmissaoPDF são os campos crus de uma submissão, exatamente como chegam do
// formulário multipart — antes de qualquer análise.
//
// Ponteiro nil significa CAMPO AUSENTE; ponteiro para cadeia vazia significa
// campo presente e vazio. A distinção importa: o legado emite críticas
// diferentes para os dois casos (reference/main.rs:151-214).
type SubmissaoPDF struct {
	// DataCaderno é o campo "data-caderno".
	DataCaderno *string
	// DataDisponibilizacao é o campo "data-disponibilizacao".
	DataDisponibilizacao *string
	// IDUsuario é o campo "id-usuario".
	IDUsuario *string
	// IDCaderno é o campo "id-caderno".
	IDCaderno *string

	// ArquivoEnviado informa se a parte "pdf" veio na requisição.
	ArquivoEnviado bool
	// NomeDoArquivo é nil quando o arquivo veio sem nome.
	// Só é consultado quando ArquivoEnviado é verdadeiro.
	NomeDoArquivo *string
}

// Validar analisa a submissão e devolve a importação montada mais as críticas
// acumuladas.
//
// A ORDEM das críticas é contrato observável e reproduz a sequência dos blocos
// de validação do legado: data do caderno, data de disponibilização, id do
// usuário, id do caderno, PDF. Ver docs/ESPECIFICACAO.md §1.4.2.
//
// Por posição, no máximo uma crítica é acrescentada — nunca "ausente" e
// "inválida" para o mesmo campo.
//
// A importação devolvida NÃO tem HashSHA256: o resumo só é calculado depois,
// sobre o conteúdo lido do arquivo (reference/main.rs:233-235). Quem chama
// preenche o campo.
//
// Quando há críticas, a importação devolvida é parcial e não deve ser
// persistida — o legado também monta o registro à medida que valida
// (main.rs:147-214) e só o descarta ao responder 400.
func (s SubmissaoPDF) Validar() (Importacao, Criticas) {
	var criticas Criticas

	// 1. data-caderno (main.rs:151-162)
	var dataCaderno Data
	if s.DataCaderno == nil {
		criticas.Adicionar(CriticaDataCadernoAusente)
	} else if d, err := AnalisarData(*s.DataCaderno); err != nil {
		criticas.Adicionar(CriticaDataCadernoInvalida)
	} else {
		dataCaderno = d
	}

	// 2. data-disponibilizacao (main.rs:164-175)
	var dataDisponibilizacao Data
	if s.DataDisponibilizacao == nil {
		criticas.Adicionar(CriticaDataDisponibilizacaoAusente)
	} else if d, err := AnalisarData(*s.DataDisponibilizacao); err != nil {
		criticas.Adicionar(CriticaDataDisponibilizacaoInvalida)
	} else {
		dataDisponibilizacao = d
	}

	// 3. id-usuario, i64 (main.rs:177-188)
	var idUsuario int64
	if s.IDUsuario == nil {
		criticas.Adicionar(CriticaIDUsuarioAusente)
	} else if v, err := AnalisarInteiro(*s.IDUsuario, 64); err != nil {
		criticas.Adicionar(CriticaIDUsuarioInvalido)
	} else {
		idUsuario = v
	}

	// 4. id-caderno, i32 (main.rs:190-201)
	var idCaderno int32
	if s.IDCaderno == nil {
		criticas.Adicionar(CriticaIDCadernoAusente)
	} else if v, err := AnalisarInteiro(*s.IDCaderno, 32); err != nil {
		criticas.Adicionar(CriticaIDCadernoInvalido)
	} else if convertido, err := ParaInt32(v); err != nil {
		// Inalcançável: AnalisarInteiro com 32 bits já limitou o intervalo.
		// A conversão passa pelo verificador mesmo assim, para que nenhuma
		// conversão estreitante do serviço fique fora do caminho auditado.
		criticas.Adicionar(CriticaIDCadernoInvalido)
	} else {
		idCaderno = convertido
	}

	// 5. pdf (main.rs:203-214)
	var arquivoPDF string
	switch {
	case !s.ArquivoEnviado:
		criticas.Adicionar(CriticaPDFAusente)
	case s.NomeDoArquivo == nil:
		criticas.Adicionar(CriticaPDFSemNome)
	default:
		arquivoPDF = *s.NomeDoArquivo
	}

	imp := NovaImportacao(
		idUsuario,
		idCaderno,
		dataCaderno,
		dataDisponibilizacao,
		arquivoPDF,
		"", // o resumo é calculado depois, sobre o conteúdo do arquivo
	)

	return imp, criticas
}

// Textos de crítica de validação.
//
// SÃO CONTRATO OBSERVÁVEL, byte a byte. Conferidos linha a linha contra
// reference/main.rs na fase F3; a coluna indica a linha de origem.
// Alterar qualquer um destes literais quebra clientes que analisam a mensagem
// da resposta 400. Ver docs/ESPECIFICACAO.md §1.4.2.
const (
	CriticaDataCadernoInvalida          = "Data do caderno é inválida"             // main.rs:155
	CriticaDataCadernoAusente           = "Data do caderno não informada"          // main.rs:159
	CriticaDataDisponibilizacaoInvalida = "Data de disponibilização é inválida"    // main.rs:168
	CriticaDataDisponibilizacaoAusente  = "Data de disponibilização não informada" // main.rs:172
	CriticaIDUsuarioInvalido            = "Id do usuário é inválido"               // main.rs:181
	CriticaIDUsuarioAusente             = "Id do usuário não informado"            // main.rs:185
	CriticaIDCadernoInvalido            = "Id do caderno é inválido"               // main.rs:194
	CriticaIDCadernoAusente             = "Id do caderno não informado"            // main.rs:198
	CriticaPDFSemNome                   = "PDF não possui nome"                    // main.rs:207
	CriticaPDFAusente                   = "PDF não enviado"                        // main.rs:211
)

// SeparadorDeCriticas é a vírgula SEM espaço, reproduzindo
// criticas.join(",") de reference/main.rs:220.
const SeparadorDeCriticas = ","

// Criticas acumula os erros de validação de uma requisição.
//
// A ORDEM DE INSERÇÃO É CONTRATO: a mensagem da resposta 400 é a concatenação
// nesta ordem. O legado a fixa pela sequência dos blocos de validação
// (reference/main.rs:151-214): data do caderno, data de disponibilização, id
// do usuário, id do caderno, PDF.
type Criticas struct {
	itens []string
}

// Adicionar acrescenta uma crítica ao final, preservando a ordem.
func (c *Criticas) Adicionar(texto string) {
	c.itens = append(c.itens, texto)
}

// Vazio informa se nenhuma crítica foi acumulada.
func (c *Criticas) Vazio() bool { return len(c.itens) == 0 }

// Total devolve a quantidade acumulada.
func (c *Criticas) Total() int { return len(c.itens) }

// Itens devolve uma cópia das críticas, na ordem de inserção.
//
// A cópia evita que quem consome altere o acumulador por engano — em
// particular a camada HTTP, que precisa da lista para o corpo problem+json da
// fase F11.
func (c *Criticas) Itens() []string {
	saida := make([]string, len(c.itens))
	copy(saida, c.itens)
	return saida
}

// Mensagem devolve o corpo exato da resposta 400.
//
// Reproduz criticas.join(",") — vírgula sem espaço. Com o acumulador vazio
// devolve a cadeia vazia, que é o que join produz sobre um vetor vazio.
func (c *Criticas) Mensagem() string {
	return strings.Join(c.itens, SeparadorDeCriticas)
}

// Erro devolve ErrValidacao quando há críticas, e nil quando não há.
//
// Permite que o caso de uso propague a falha por errors.Is sem perder a lista.
func (c *Criticas) Erro() error {
	if c.Vazio() {
		return nil
	}
	return ErrValidacao
}

// -------------------------------------------------------------------------
// Críticas de EVOLUÇÃO — fase F11
// -------------------------------------------------------------------------
//
// Os textos abaixo NÃO existem no legado. Só aparecem com a chave
// correspondente ligada, e por isso não violam a paridade: com todas as chaves
// no padrão, nenhuma resposta muda.
//
// A posição é sempre AO FINAL da lista, depois das cinco críticas do legado.
// Inseri-las no meio deslocaria as existentes e mudaria a mensagem de uma
// requisição que hoje falha por outro motivo.

// CriticaPDFAcimaDoLimite acompanha MAX_UPLOAD_BYTES.
//
// É a opção B de docs/DECISOES-ABERTAS.md, D-16: reaproveita o formato de
// crítica que os clientes já sabem interpretar, em vez de introduzir um 413 ou
// reaproveitar o texto genérico do 422.
//
// É a ÚNICA crítica nova da fase. VALIDAR_ASSINATURA_PDF, a outra evolução que
// rejeita arquivo, responde com o 422 e o texto que o legado JÁ emite — a
// especificação da fase exige "a MESMA 422 do legado, sem texto novo", e um
// texto a mais ali seria contrato novo sem necessidade.
const CriticaPDFAcimaDoLimite = "PDF excede o tamanho máximo"

// AssinaturaPDF é o prefixo que todo PDF tem, por exigência da própria
// especificação do formato: `%PDF-` seguido da versão.
//
// Não é validação profunda: um arquivo que comece assim e esteja corrompido
// continua passando, e é o extrator que descobre depois. O objetivo é barrar o
// engano óbvio — planilha, imagem, arquivo vazio — antes de gastar uma
// importação inteira nele.
const AssinaturaPDF = "%PDF-"

// PareceComPDF informa se o conteúdo começa com a assinatura do formato.
//
// Conteúdo vazio devolve false: um arquivo de zero byte não é PDF. Isso NÃO
// entra em conflito com INV-P20, que trata de PDF TRUNCADO — truncado tem
// cabeçalho e perde o fim; vazio não tem nada.
func PareceComPDF(conteudo []byte) bool {
	return len(conteudo) >= len(AssinaturaPDF) &&
		string(conteudo[:len(AssinaturaPDF)]) == AssinaturaPDF
}

// Erros sentinela do domínio.
//
// São comparados com errors.Is, nunca por texto. Os adaptadores os envolvem
// com %w ao traduzir falhas de infraestrutura, e a camada HTTP é a única que
// os converte em código de status.
var (
	// ErrValidacao indica entrada rejeitada pelas regras de crítica.
	// Acompanha sempre um Criticas com o detalhe observável.
	ErrValidacao = errors.New("validação rejeitou a entrada")

	// ErrPDFInvalido indica documento que o extrator não conseguiu abrir.
	// ATENÇÃO: um PDF TRUNCADO não cai aqui — ele é aceito e produz zero
	// páginas. Ver docs/INVARIANTES.md, INV-P20.
	ErrPDFInvalido = errors.New("PDF inválido")

	// ErrExtracaoTexto indica falha na leitura do texto de um documento já
	// aberto.
	ErrExtracaoTexto = errors.New("falha ao extrair texto")

	// ErrIndiceIndisponivel indica falha na construção ou no uso do índice.
	ErrIndiceIndisponivel = errors.New("índice indisponível")

	// ErrPersistencia envolve toda falha vinda do banco de dados.
	ErrPersistencia = errors.New("falha de persistência")

	// ErrImportacaoNaoEncontrada indica identificador sem correspondência.
	ErrImportacaoNaoEncontrada = errors.New("importação não encontrada")

	// ErrEstouroNumerico indica conversão estreitante que não cabe no destino.
	//
	// Existe porque o legado usa try_from, que FALHA em vez de truncar
	// (reference/main.rs:603 e 712). Go converteria em silêncio.
	// Ver docs/INVARIANTES.md, INV-P15 e INV-P18.
	ErrEstouroNumerico = errors.New("estouro numérico")

	// ErrExpressaoInvalida indica expressão de perfil que não compila como
	// filtro do operador `&`.
	//
	// ATENÇÃO — não tem equivalente exato no legado. Ali a compilação é
	// `regex::Regex::new(&exp).unwrap()` (reference/main.rs:395) e uma
	// expressão inválida ENTRA EM PÂNICO dentro da tarefa do tokio, que morre
	// SEM atualizar o status: a importação fica presa em 3 para sempre. Um erro
	// tipado leva a importação a -1, que é um desfecho diferente.
	// A escolha entre reproduzir o travamento e corrigi-lo está registrada em
	// docs/DECISOES-ABERTAS.md, D-06. A fase F8 adotou o padrão provisório de
	// lá: reproduzir o travamento — ver usecase.Pipeline.encerrarPreso.
	ErrExpressaoInvalida = errors.New("expressão de busca inválida")
)

// ErroDeValidacao carrega as críticas que rejeitaram a submissão.
//
// Existe para que a camada HTTP obtenha a lista sem que o caso de uso precise
// devolver um terceiro valor de retorno. Satisfaz errors.Is para ErrValidacao e
// é recuperável com errors.As.
type ErroDeValidacao struct {
	Criticas Criticas
}

// NovoErroDeValidacao embrulha as críticas acumuladas.
func NovoErroDeValidacao(criticas Criticas) *ErroDeValidacao {
	return &ErroDeValidacao{Criticas: criticas}
}

// Error devolve a mensagem exata da resposta 400 — a concatenação por vírgula
// sem espaço (reference/main.rs:220).
func (e *ErroDeValidacao) Error() string {
	return e.Criticas.Mensagem()
}

// Unwrap liga o erro ao sentinela do domínio.
func (e *ErroDeValidacao) Unwrap() error { return ErrValidacao }
