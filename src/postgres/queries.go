package postgres

import (
	_ "embed"
	"strings"
)

// Consultas SQL do serviço, transcritas caractere a caractere de
// reference/main.rs e embutidas no binário.
//
// Nenhuma pode ser reescrita, reformatada ou "otimizada". O teste
// TestConsultasSaoIdenticasAoLegado carrega as duas fontes e compara byte a
// byte; qualquer divergência reprova a integração contínua.

//go:embed queries/importacao_inserir.sql
var arquivoImportacaoInserir string

//go:embed queries/importacao_status.sql
var arquivoImportacaoStatus string

//go:embed queries/importacao_inicio.sql
var arquivoImportacaoInicio string

//go:embed queries/importacao_termino.sql
var arquivoImportacaoTermino string

//go:embed queries/chaves_pesquisa.sql
var arquivoChavesPesquisa string

//go:embed queries/recorte_inserir.sql
var arquivoRecorteInserir string

//go:embed queries/recorte_texto_inserir.sql
var arquivoRecorteTextoInserir string

// -------------------------------------------------------------------------
// Consultas de EVOLUÇÃO — fase F11
// -------------------------------------------------------------------------
//
// As cinco abaixo NÃO existem no legado e por isso vivem em queries/f11/, sem o
// marcador de consulta literal: não há original com que compará-las, byte a
// byte, como se faz com as sete de cima. Todas só são executadas com a chave
// ligada — com os padrões, o serviço emite exatamente as sete consultas do
// legado, nem uma a mais.

//go:embed queries/f11/importacao_resumo.sql
var sqlImportacaoResumo string

//go:embed queries/f11/importacao_equivalente.sql
var sqlImportacaoEquivalente string

//go:embed queries/f11/importacoes_presas.sql
var sqlImportacoesPresas string

//go:embed queries/f11/recorte_inserir_lote.sql
var sqlRecorteInserirLote string

//go:embed queries/f11/recorte_texto_inserir_lote.sql
var sqlRecorteTextoInserirLote string

// marcadorDeConsulta separa o cabeçalho explicativo do texto literal.
//
// O cabeçalho documenta a origem e as armadilhas de cada consulta; o que vai
// para o banco é apenas o que vem DEPOIS desta linha, byte a byte igual ao
// literal do legado.
const marcadorDeConsulta = "-- >>>>> INICIO DA CONSULTA LITERAL"

// consultaLiteral extrai o texto da consulta, descartando o cabeçalho.
func consultaLiteral(arquivo string) string {
	inicio := strings.Index(arquivo, marcadorDeConsulta)
	if inicio < 0 {
		// Só acontece se alguém remover o marcador de um arquivo .sql. Como o
		// conteúdo é embutido em tempo de compilação, a falha é determinística
		// e aparece no primeiro teste que rodar.
		return arquivo
	}
	restante := arquivo[inicio:]
	if quebra := strings.IndexByte(restante, '\n'); quebra >= 0 {
		return restante[quebra+1:]
	}
	return ""
}

// As consultas prontas para uso. Avaliadas uma vez, na carga do pacote.
var (
	sqlImportacaoInserir   = consultaLiteral(arquivoImportacaoInserir)
	sqlImportacaoStatus    = consultaLiteral(arquivoImportacaoStatus)
	sqlImportacaoInicio    = consultaLiteral(arquivoImportacaoInicio)
	sqlImportacaoTermino   = consultaLiteral(arquivoImportacaoTermino)
	sqlChavesPesquisa      = consultaLiteral(arquivoChavesPesquisa)
	sqlRecorteInserir      = consultaLiteral(arquivoRecorteInserir)
	sqlRecorteTextoInserir = consultaLiteral(arquivoRecorteTextoInserir)
)
