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
)
