package domain

import "time"

// Este arquivo declara os tipos de LEITURA da fase F11.
//
// Nenhum deles existe no legado, que só escreve em tb_importacao e nunca a
// consulta de volta. Todos servem a evoluções atrás de chave:
//
//	ResumoDaImportacao   STATUS_ENDPOINT       GET /importacao/{id}
//	ChaveDeIdempotencia  IDEMPOTENCIA_POR_HASH reenvio do mesmo documento
//	ImportacaoPresa      VARREDURA_ORFAS       importação parada em 1, 2 ou 3
//
// Com as três chaves no padrão, nenhuma consulta destas é executada — o serviço
// não emite um SELECT a mais que o legado.

// ResumoDaImportacao é o que GET /importacao/{id} devolve.
//
// É deliberadamente MENOR que Importacao: não traz o hash nem o nome do
// arquivo. O hash identifica o conteúdo submetido e o nome pode conter dado do
// remetente; nenhum dos dois é necessário para saber em que pé está o
// processamento, e endpoint de status é o tipo de coisa que acaba exposta mais
// amplamente que o de submissão.
type ResumoDaImportacao struct {
	ID     int64
	Status StatusImportacao

	DataCaderno          Data
	DataDisponibilizacao Data

	// DataInicio e DataFim são carimbos do SERVIDOR de banco, gravados por
	// current_timestamp (INV-P16). Nulas enquanto a etapa correspondente não
	// aconteceu.
	DataInicio *time.Time
	DataFim    *time.Time

	// TotalRecortes é nulo até MarcarTermino gravá-lo. Nulo NÃO é zero: zero é
	// "processou e não achou nada", nulo é "ainda não terminou" — ou terminou em
	// erro, que não grava a coluna.
	TotalRecortes *int32
}

// Concluida informa se a importação chegou a um estado terminal.
func (r ResumoDaImportacao) Concluida() bool {
	return r.Status == StatusFinalizado || r.Status == StatusErro
}

// ChaveDeIdempotencia identifica um reenvio do MESMO documento.
//
// Os três campos vêm da decisão de F11: mesmo hash, mesmo id_cadernos e mesma
// data_caderno. O hash sozinho não basta — o mesmo arquivo pode ser submetido
// legitimamente para cadernos ou datas diferentes, e tratá-los como repetição
// perderia importações.
//
// A data de DISPONIBILIZAÇÃO fica de fora de propósito: ela é a data em que o
// diário foi publicado, e reenviar o mesmo caderno com a disponibilização
// corrigida é correção de metadado, não documento novo.
type ChaveDeIdempotencia struct {
	HashSHA256  string
	IDCaderno   int32
	DataCaderno Data
}

// DeImportacao extrai a chave de idempotência de uma importação.
func DeImportacao(imp Importacao) ChaveDeIdempotencia {
	return ChaveDeIdempotencia{
		HashSHA256:  imp.HashSHA256,
		IDCaderno:   imp.IDCaderno,
		DataCaderno: imp.DataCaderno,
	}
}

// ImportacaoPresa é uma importação parada em estado NÃO terminal.
//
// A varredura de órfãs a encontra pelos status 1, 2 e 3 com data_inicio antiga.
// Status 0 fica de fora: uma importação em 0 foi aceita e ainda não começou, e
// pode estar legitimamente na fila do executor — reprocessá-la seria duplicar
// trabalho em andamento.
type ImportacaoPresa struct {
	ID     int64
	Status StatusImportacao

	// DataInicio é quando a tarefa marcou o começo. Nula quando a importação
	// travou antes de MarcarInicio.
	DataInicio *time.Time
}

// StatusPresos são os estados de onde a varredura resgata.
//
// A ordem é a numérica, e é a mesma do IN da consulta — mantê-las iguais é o
// que permite ler as duas lado a lado.
var StatusPresos = []StatusImportacao{
	StatusSelecionado, StatusIndexando, StatusRecortando,
}
