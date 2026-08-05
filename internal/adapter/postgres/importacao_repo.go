package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/gluizcortez/projetorust/internal/domain"
)

// RepositorioImportacao implementa domain.RepositorioImportacao sobre pgx.
type RepositorioImportacao struct {
	base
}

var _ domain.RepositorioImportacao = (*RepositorioImportacao)(nil)

// NovoRepositorioImportacao cria o repositório sobre o pool.
func NovoRepositorioImportacao(pool *pgxpool.Pool) *RepositorioImportacao {
	return &RepositorioImportacao{base{pool: pool}}
}

// paraDataPg converte a data pura do domínio para o tipo de data do pgx.
//
// pgtype.Date NÃO carrega fuso, que é o ponto: um time.Time construído no
// local do servidor pode deslocar um dia na gravação (INV-P16). A conversão
// passa por time.Date em UTC apenas porque é o formato que o driver aceita; a
// hora é sempre meia-noite e o fuso sempre UTC.
func paraDataPg(d domain.Data) pgtype.Date {
	if d.Zero() {
		return pgtype.Date{Valid: false}
	}
	return pgtype.Date{
		Time:  dataEmUTC(d),
		Valid: true,
	}
}

// Registrar insere a importação e devolve o identificador gerado.
// Reproduz reference/main.rs:447-477.
func (r *RepositorioImportacao) Registrar(ctx context.Context, imp domain.Importacao) (int64, error) {
	var id int64
	err := r.consultador(ctx).QueryRow(ctx, sqlImportacaoInserir,
		imp.IDUsuario,
		imp.IDCaderno,
		paraDataPg(imp.DataCaderno),
		paraDataPg(imp.DataDisponibilizacao),
		int32(imp.Status),
		imp.ArquivoPDF,
		imp.TipoCaderno,
		imp.HashSHA256,
	).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("%w: registrando importação: %w", domain.ErrPersistencia, err)
	}
	return id, nil
}

// AtualizarStatus grava o novo estado. Reproduz main.rs:651-680.
func (r *RepositorioImportacao) AtualizarStatus(
	ctx context.Context, id int64, s domain.StatusImportacao,
) error {
	if _, err := r.consultador(ctx).Exec(ctx, sqlImportacaoStatus, int32(s), id); err != nil {
		return fmt.Errorf("%w: atualizando status para %s: %w", domain.ErrPersistencia, s, err)
	}
	return nil
}

// MarcarInicio grava data_inicio com current_timestamp do servidor.
// Reproduz main.rs:682-698.
func (r *RepositorioImportacao) MarcarInicio(ctx context.Context, id int64) error {
	if _, err := r.consultador(ctx).Exec(ctx, sqlImportacaoInicio, id); err != nil {
		return fmt.Errorf("%w: marcando início da importação: %w", domain.ErrPersistencia, err)
	}
	return nil
}

// MarcarTermino grava data_fim e total_recortes.
//
// Reproduz main.rs:700-718 — inclusive INV-P18: a conversão para int32
// acontece ANTES de qualquer comando, e a falha deixa as duas colunas
// intocadas em vez de gravar um valor truncado.
func (r *RepositorioImportacao) MarcarTermino(ctx context.Context, id int64, total int32) error {
	if _, err := r.consultador(ctx).Exec(ctx, sqlImportacaoTermino, total, id); err != nil {
		return fmt.Errorf("%w: marcando término da importação: %w", domain.ErrPersistencia, err)
	}
	return nil
}
