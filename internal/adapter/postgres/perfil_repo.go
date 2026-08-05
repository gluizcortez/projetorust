package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/gluizcortez/projetorust/internal/domain"
)

// RepositorioPerfil implementa domain.RepositorioPerfil sobre pgx.
type RepositorioPerfil struct {
	base
}

var _ domain.RepositorioPerfil = (*RepositorioPerfil)(nil)

// NovoRepositorioPerfil cria o repositório sobre o pool.
func NovoRepositorioPerfil(pool *pgxpool.Pool) *RepositorioPerfil {
	return &RepositorioPerfil{base{pool: pool}}
}

// ChavesPesquisa devolve os pares perfil/expressão do caderno da importação.
//
// Reproduz main.rs:544-571. A ORDEM vem do ORDER BY da consulta e é
// normativa: governa a deduplicação por perfil (INV-P12). Este método não
// reordena nada.
func (r *RepositorioPerfil) ChavesPesquisa(
	ctx context.Context, idImportacao int64,
) ([]domain.ChavePesquisa, error) {
	linhas, err := r.consultador(ctx).Query(ctx, sqlChavesPesquisa, idImportacao)
	if err != nil {
		return nil, fmt.Errorf("%w: consultando chaves de pesquisa: %w", domain.ErrPersistencia, err)
	}
	defer linhas.Close()

	var chaves []domain.ChavePesquisa
	for linhas.Next() {
		var c domain.ChavePesquisa
		if err := linhas.Scan(&c.IDPerfil, &c.Expressao); err != nil {
			return nil, fmt.Errorf("%w: lendo chave de pesquisa: %w", domain.ErrPersistencia, err)
		}
		chaves = append(chaves, c)
	}
	if err := linhas.Err(); err != nil {
		return nil, fmt.Errorf("%w: percorrendo chaves de pesquisa: %w", domain.ErrPersistencia, err)
	}
	return chaves, nil
}
