package domain

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
