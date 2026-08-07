package domain

// TipoCadernoPDF é o valor gravado em recorte.tb_importacao.tipo_caderno.
//
// Vem do Default de DiarioPDF (reference/main.rs:441). É constante aqui para
// que o literal não apareça em nenhum outro lugar do código.
const TipoCadernoPDF = "PDF"

// Importacao é o registro de um documento submetido para recorte.
//
// Corresponde a uma linha de recorte.tb_importacao. Substitui a struct
// DiarioPDF do legado (reference/main.rs:412-430), separando o modelo do
// mapeamento de banco: as tags de coluna são responsabilidade do adaptador.
type Importacao struct {
	// ID é recorte.tb_importacao.id_importacao. Vale zero antes de persistir.
	ID int64

	// IDUsuario é gravado na coluna id_inclusao.
	IDUsuario int64
	// IDCaderno é gravado na coluna id_cadernos.
	IDCaderno int32

	// DataCaderno e DataDisponibilizacao são datas puras, sem fuso (INV-P16).
	DataCaderno          Data
	DataDisponibilizacao Data

	Status StatusImportacao

	// ArquivoPDF é o nome original do arquivo, coluna nome_original_pdf.
	ArquivoPDF string
	// TipoCaderno é sempre TipoCadernoPDF no fluxo atual.
	TipoCaderno string
	// HashSHA256 é o resumo hexadecimal do conteúdo submetido.
	//
	// O legado o grava e não o usa para nada. A idempotência por hash é
	// evolução da fase F11, atrás de chave (achado A11).
	HashSHA256 string
}

// NovaImportacao monta uma importação pronta para ser registrada.
//
// Aplica os padrões que no legado vêm do Default de DiarioPDF
// (reference/main.rs:432-445): status recebido e tipo de caderno "PDF".
// Esses dois padrões não podem aparecer como literal em nenhum outro lugar.
func NovaImportacao(
	idUsuario int64,
	idCaderno int32,
	dataCaderno Data,
	dataDisponibilizacao Data,
	arquivoPDF string,
	hashSHA256 string,
) Importacao {
	return Importacao{
		IDUsuario:            idUsuario,
		IDCaderno:            idCaderno,
		DataCaderno:          dataCaderno,
		DataDisponibilizacao: dataDisponibilizacao,
		Status:               StatusRecebido,
		ArquivoPDF:           arquivoPDF,
		TipoCaderno:          TipoCadernoPDF,
		HashSHA256:           hashSHA256,
	}
}

// Persistida informa se a importação já recebeu identificador do banco.
func (i Importacao) Persistida() bool { return i.ID != 0 }
