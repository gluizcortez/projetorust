package postgres

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// consultador é o mínimo que os repositórios precisam de um executor de SQL.
//
// Tanto *pgxpool.Pool quanto pgx.Tx o satisfazem, e é isso que permite ao
// mesmo repositório participar de uma transação quando existe uma, sem
// duplicar código nem receber a transação por parâmetro.
type consultador interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

var (
	_ consultador = (*pgxpool.Pool)(nil)
	_ consultador = (pgx.Tx)(nil)
)

// chaveDeTransacao é o tipo da chave de contexto que carrega a transação.
// Não exportado: ninguém fora deste pacote consegue colidir com ele.
type chaveDeTransacao struct{}

// comTransacao anexa a transação corrente ao contexto.
func comTransacao(ctx context.Context, tx pgx.Tx) context.Context {
	return context.WithValue(ctx, chaveDeTransacao{}, tx)
}

// daTransacao devolve a transação corrente, se houver.
func daTransacao(ctx context.Context) (pgx.Tx, bool) {
	tx, ok := ctx.Value(chaveDeTransacao{}).(pgx.Tx)
	return tx, ok
}

// base resolve o executor: a transação do contexto quando existe, o pool
// quando não existe.
type base struct {
	pool *pgxpool.Pool
}

func (b base) consultador(ctx context.Context) consultador {
	if tx, ok := daTransacao(ctx); ok {
		return tx
	}
	return b.pool
}
