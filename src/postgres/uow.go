package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/gluizcortez/projetorust/src/domain"
)

// UnidadeDeTrabalho executa uma função dentro de uma transação.
//
// A transação viaja no contexto; os repositórios a encontram por conta própria
// e participam dela sem recebê-la por parâmetro. Isso mantém as assinaturas das
// portas livres de qualquer conceito de banco.
type UnidadeDeTrabalho struct {
	pool *pgxpool.Pool
}

var _ domain.UnidadeDeTrabalho = (*UnidadeDeTrabalho)(nil)

// NovaUnidadeDeTrabalho cria a unidade de trabalho sobre o pool.
func NovaUnidadeDeTrabalho(pool *pgxpool.Pool) *UnidadeDeTrabalho {
	return &UnidadeDeTrabalho{pool: pool}
}

// EmTransacao abre a transação, executa a função e confirma.
//
// Erro reverte. Pânico reverte e o repropaga — abandonar uma transação aberta
// prenderia a conexão até o tempo limite do pool.
//
// Transações aninhadas reaproveitam a corrente em vez de abrir outra: o
// escopo transacional do serviço é definido por quem chama EmTransacao, e
// aninhar silenciosamente mudaria o que sobrevive a uma falha (INV-P14).
func (u *UnidadeDeTrabalho) EmTransacao(ctx context.Context, fn func(context.Context) error) (err error) {
	if _, jaEmTransacao := daTransacao(ctx); jaEmTransacao {
		if err := fn(ctx); err != nil {
			return fmt.Errorf("dentro de transação já aberta: %w", err)
		}
		return nil
	}

	tx, err := u.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("%w: abrindo transação: %w", domain.ErrPersistencia, err)
	}

	defer func() {
		if p := recover(); p != nil {
			_ = tx.Rollback(context.WithoutCancel(ctx))
			panic(p)
		}
		if err != nil {
			// O contexto pode já ter sido cancelado pelo erro; a reversão
			// precisa de um que sobreviva.
			if errReverter := tx.Rollback(context.WithoutCancel(ctx)); errReverter != nil &&
				!errors.Is(errReverter, pgx.ErrTxClosed) {
				err = errors.Join(err, fmt.Errorf("revertendo transação: %w", errReverter))
			}
		}
	}()

	if err = fn(comTransacao(ctx, tx)); err != nil {
		return err
	}

	if err = tx.Commit(ctx); err != nil {
		return fmt.Errorf("%w: confirmando transação: %w", domain.ErrPersistencia, err)
	}
	return nil
}
