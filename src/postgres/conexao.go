// A conexão com o banco: o pool, a abstração de quem executa uma consulta
// (pool ou transação, indistintamente) e a conversão de data do domínio.
package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/gluizcortez/projetorust/src/config"
	"github.com/gluizcortez/projetorust/src/domain"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Parâmetros do pool.
//
// O legado usa Pool::connect (reference/main.rs:85), que aceita os padrões do
// SQLx. Os valores abaixo reproduzem esses padrões como LINHA DE BASE, para
// que a migração não mude a pressão sobre o banco antes da medição da fase
// F12. A origem de cada número está em docs/CONTEXT.md.
const (
	// MaxConexoes reproduz o padrão do SQLx (10).
	MaxConexoes = int32(10)
	// MinConexoes reproduz o padrão do SQLx (0): o pool não mantém conexões
	// ociosas abertas.
	MinConexoes = int32(0)
	// TempoDeVidaMaximo reproduz o max_lifetime padrão do SQLx (30 min).
	TempoDeVidaMaximo = 30 * time.Minute
	// TempoOciosoMaximo reproduz o idle_timeout padrão do SQLx (10 min).
	TempoOciosoMaximo = 10 * time.Minute
	// PeriodoDeVerificacao é a varredura de saúde do pgxpool. O SQLx não tem
	// equivalente direto; o padrão do pgx (1 min) é mantido.
	PeriodoDeVerificacao = 1 * time.Minute
	// TempoLimiteDeConexao reproduz o acquire_timeout padrão do SQLx (30 s).
	TempoLimiteDeConexao = 30 * time.Second
)

// NovoPool abre o pool de conexões.
//
// Diferente do legado, que entra em pânico (main.rs:85), a falha vira erro: a
// decisão de encerrar o processo é de cmd/, não daqui.
func NovoPool(ctx context.Context, cfg *config.Config) (*pgxpool.Pool, error) {
	conf, err := pgxpool.ParseConfig(cfg.DatabaseURL.Revelar())
	if err != nil {
		// A URL não entra na mensagem: pode conter a senha.
		return nil, fmt.Errorf("DATABASE_URL não pôde ser analisada: %w", err)
	}

	conf.MaxConns = MaxConexoes
	conf.MinConns = MinConexoes
	conf.MaxConnLifetime = TempoDeVidaMaximo
	conf.MaxConnIdleTime = TempoOciosoMaximo
	conf.HealthCheckPeriod = PeriodoDeVerificacao
	conf.ConnConfig.ConnectTimeout = TempoLimiteDeConexao

	// Nenhum AfterConnect define search_path: todas as consultas do legado
	// qualificam o esquema `recorte.` explicitamente, e a verificação de
	// docs/DECISOES-ABERTAS.md, D-12, ainda não indicou configuração por papel
	// ou por banco. Se D-12 revelar uma, é aqui que ela entra.

	pool, err := pgxpool.NewWithConfig(ctx, conf)
	if err != nil {
		return nil, fmt.Errorf("abrindo pool de conexões: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("banco de dados inalcançável: %w", err)
	}

	return pool, nil
}

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

// dataEmUTC converte a data pura do domínio para time.Time à meia-noite UTC.
//
// UTC é obrigatório e não é detalhe: com o fuso local do processo, uma data
// como 2024-03-15 pode chegar ao driver como 2024-03-14T21:00-03:00 e ser
// gravada com o dia anterior. Ver docs/INVARIANTES.md, INV-P16.
func dataEmUTC(d domain.Data) time.Time {
	return time.Date(d.Ano(), time.Month(d.Mes()), d.Dia(), 0, 0, 0, 0, time.UTC)
}
