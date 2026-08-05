package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/gluizcortez/projetorust/internal/domain"
)

// RepositorioRecorte implementa domain.RepositorioRecorte sobre pgx.
type RepositorioRecorte struct {
	base
	uow domain.UnidadeDeTrabalho
}

var _ domain.RepositorioRecorte = (*RepositorioRecorte)(nil)

// NovoRepositorioRecorte cria o repositório sobre o pool.
func NovoRepositorioRecorte(pool *pgxpool.Pool, uow domain.UnidadeDeTrabalho) *RepositorioRecorte {
	return &RepositorioRecorte{base: base{pool: pool}, uow: uow}
}

// Salvar grava os recortes de uma chave de pesquisa.
//
// Reproduz reference/main.rs:573-631, com UMA correção deliberada e UMA
// preservação deliberada.
//
// CORREÇÃO (achado A03). O legado executa os dois INSERT de cada recorte de
// forma independente e tem um TODO explícito sobre isso (main.rs:599). Falha
// no segundo deixa uma linha órfã em tb_recorte, sem o texto correspondente.
// Aqui o par é atômico.
//
// PRESERVAÇÃO (INV-P14). O escopo transacional é UMA CHAMADA deste método —
// não a importação inteira. O legado invoca salvar_recorte uma vez por chave
// de pesquisa, então "uma transação por chamada" mantém exatamente o que
// sobrevive a uma falha no meio do processamento: as chaves já gravadas
// permanecem, a que falhou não deixa nada, e as seguintes não são
// processadas. Uma transação por importação reverteria as chaves anteriores e
// mudaria o estado final observável — por isso é evolução da fase F11, atrás
// da chave GRAVACAO_EM_LOTE.
func (r *RepositorioRecorte) Salvar(
	ctx context.Context,
	idImportacao int64,
	chave domain.ChavePesquisa,
	recortes []domain.Recorte,
) (int32, error) {
	// O caso de uso não chama com lista vazia (main.rs:308). Se chamar, não há
	// o que gravar e o banco não é tocado.
	if len(recortes) == 0 {
		return 0, nil
	}

	var contador int32
	err := r.uow.EmTransacao(ctx, func(ctx context.Context) error {
		q := r.consultador(ctx)

		for _, recorte := range recortes {
			// main.rs:603 — a conversão FALHA em vez de truncar (INV-P15).
			nrPagina, err := domain.ParaInt64(recorte.Pagina)
			if err != nil {
				return fmt.Errorf("página %d da importação %d: %w", recorte.Pagina, idImportacao, err)
			}

			var idRecorte int64
			if err := q.QueryRow(ctx, sqlRecorteInserir,
				idImportacao,
				nrPagina,
				chave.IDPerfil,
				chave.Expressao,
			).Scan(&idRecorte); err != nil {
				return fmt.Errorf("%w: inserindo recorte: %w", domain.ErrPersistencia, err)
			}

			// main.rs:619 — grava `highlight`, não `text`.
			if _, err := q.Exec(ctx, sqlRecorteTextoInserir, idRecorte, recorte.Destaque); err != nil {
				return fmt.Errorf("%w: inserindo texto do recorte %d: %w",
					domain.ErrPersistencia, idRecorte, err)
			}

			contador++
		}
		return nil
	})
	if err != nil {
		return 0, fmt.Errorf("gravando %d recorte(s) do perfil %d na importação %d: %w",
			len(recortes), chave.IDPerfil, idImportacao, err)
	}

	return contador, nil
}
