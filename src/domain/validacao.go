package domain

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
