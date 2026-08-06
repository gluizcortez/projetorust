package domain

import "errors"

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
