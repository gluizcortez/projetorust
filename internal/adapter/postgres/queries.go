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

// ConsultasLiterais expõe as consultas para o teste de paridade textual.
//
// A chave é o nome da função de origem em reference/main.rs, para que a
// mensagem de falha aponte direto para o trecho a conferir.
func ConsultasLiterais() map[string]string {
	return map[string]string{
		"registrar_pdf":                sqlImportacaoInserir,
		"obter_chaves_pesquisa":        sqlChavesPesquisa,
		"salvar_recorte/recorte":       sqlRecorteInserir,
		"salvar_recorte/texto":         sqlRecorteTextoInserir,
		"atualizar_status_importacao":  sqlImportacaoStatus,
		"registrar_inicio_importacao":  sqlImportacaoInicio,
		"registrar_termino_importacao": sqlImportacaoTermino,
	}
}
