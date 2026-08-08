// Os tipos de dados do domínio: a importação, o recorte e o perfil de cliente.
//
// Reunidos num arquivo só porque são o mesmo assunto — o QUE o serviço
// manipula. As REGRAS que operam sobre eles estão em validacao.go e status.go.
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

// Recorte é uma ocorrência de expressão de perfil em uma página de documento.
// É o produto do serviço.
type Recorte struct {
	// Pagina é o número da página, começando em 1
	// (reference/main.rs:526: doc.add_u64(page_field, (i+1) as u64)).
	Pagina uint64

	// Texto é o texto integral da página.
	//
	// DADO MORTO NO LEGADO: o campo `text` de Recorte é escrito e nunca lido —
	// salvar_recorte grava `highlight` (reference/main.rs:619). O compilador
	// Rust confirma com "field `text` is never read". Mantido por fidelidade;
	// em Go compartilha o mesmo backing array de Destaque, então custa um
	// cabeçalho de string, não uma cópia.
	// Ver docs/MAPA-DE-CHAMADAS.md §4.1.
	Texto string

	// Destaque é o que vai para recorte.tb_recorte_texto.recorte.
	//
	// Apesar do nome, NÃO é um trecho ao redor da ocorrência: é o texto
	// integral da página, normalizado e sem diacríticos — idêntico a Texto por
	// construção. Recortar uma janela de contexto é evolução da fase F11,
	// atrás de chave. Ver docs/ESPECIFICACAO.md §5.4.
	Destaque string
}

// NovoRecorte monta um recorte a partir do texto de uma página.
//
// Texto e Destaque recebem o mesmo valor, como em reference/main.rs:400-406.
func NovoRecorte(pagina uint64, textoDaPagina string) Recorte {
	return Recorte{
		Pagina:   pagina,
		Texto:    textoDaPagina,
		Destaque: textoDaPagina,
	}
}

// ChavePesquisa é o par perfil/expressão a procurar em um documento.
//
// Corresponde a uma linha do resultado de obter_chaves_pesquisa
// (reference/main.rs:544-571).
type ChavePesquisa struct {
	// IDPerfil identifica o perfil do cliente.
	IDPerfil int64
	// Expressao é o texto cadastrado em tb_perfil_variacao.expressao_nm.
	//
	// NÃO passa por normalização: o texto indexado tem os diacríticos
	// removidos e a expressão não, então expressões acentuadas nunca casam.
	// É defeito de produto existente, preservado. Ver INV-P19 e D-17.
	Expressao string
}
