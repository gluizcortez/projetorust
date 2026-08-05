package observability

import "context"

// chave é o tipo das chaves de contexto deste pacote.
//
// Não é exportado de propósito: nenhum outro pacote consegue colidir com ele,
// nem ler os valores sem passar pelos acessadores daqui.
type chave int

const (
	chaveIDRequisicao chave = iota
	chaveIDImportacao
	chaveIDPerfil
)

// ComIDRequisicao anexa o identificador da requisição ao contexto.
//
// A partir daí, todo registro emitido com esse contexto carrega o atributo
// id_requisicao — sem que quem registra precise passá-lo.
func ComIDRequisicao(ctx context.Context, id string) context.Context {
	if id == "" {
		return ctx
	}
	return context.WithValue(ctx, chaveIDRequisicao, id)
}

// IDRequisicao lê o identificador da requisição do contexto.
func IDRequisicao(ctx context.Context) (string, bool) {
	v, ok := ctx.Value(chaveIDRequisicao).(string)
	return v, ok
}

// ComIDImportacao anexa o identificador da importação ao contexto.
//
// Substitui a interpolação do legado, que embutia o identificador no texto da
// mensagem — "[ID Importação: 42] -> ..." — e por isso impedia correlação por
// campo (achado A21).
func ComIDImportacao(ctx context.Context, id int64) context.Context {
	return context.WithValue(ctx, chaveIDImportacao, id)
}

// IDImportacao lê o identificador da importação do contexto.
func IDImportacao(ctx context.Context) (int64, bool) {
	v, ok := ctx.Value(chaveIDImportacao).(int64)
	return v, ok
}

// ComIDPerfil anexa o identificador do perfil ao contexto.
func ComIDPerfil(ctx context.Context, id int64) context.Context {
	return context.WithValue(ctx, chaveIDPerfil, id)
}

// IDPerfil lê o identificador do perfil do contexto.
func IDPerfil(ctx context.Context) (int64, bool) {
	v, ok := ctx.Value(chaveIDPerfil).(int64)
	return v, ok
}
