package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/gluizcortez/projetorust/internal/domain"
)

// RepositorioConsulta é o lado de LEITURA de tb_importacao — inteiramente novo
// na fase F11.
//
// O legado só escreve nessa tabela: nenhuma das sete consultas literais a lê de
// volta. Tudo aqui existe para as evoluções STATUS_ENDPOINT e
// IDEMPOTENCIA_POR_HASH, e com as duas no padrão nenhum método deste tipo é
// chamado — o serviço não emite um SELECT a mais que o legado.
//
// Fica em um tipo SEPARADO de RepositorioImportacao de propósito: o repositório
// de escrita reproduz o legado byte a byte e não deve ganhar métodos que o
// legado não tem.
type RepositorioConsulta struct {
	base
}

// NovoRepositorioConsulta cria o repositório de leitura sobre o pool.
func NovoRepositorioConsulta(pool *pgxpool.Pool) *RepositorioConsulta {
	return &RepositorioConsulta{base{pool: pool}}
}

// Consultar devolve o resumo de uma importação pelo identificador.
//
// Identificador sem correspondência devolve domain.ErrImportacaoNaoEncontrada.
func (r *RepositorioConsulta) Consultar(
	ctx context.Context, id int64,
) (domain.ResumoDaImportacao, error) {
	linha := r.consultador(ctx).QueryRow(ctx, sqlImportacaoResumo, id)

	resumo, err := lerResumo(linha)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ResumoDaImportacao{}, fmt.Errorf("importação %d: %w",
			id, domain.ErrImportacaoNaoEncontrada)
	}
	if err != nil {
		return domain.ResumoDaImportacao{}, fmt.Errorf("%w: consultando importação %d: %w",
			domain.ErrPersistencia, id, err)
	}
	return resumo, nil
}

// ImportacaoEquivalente procura a importação mais recente do mesmo documento.
//
// Satisfaz usecase.ConsultorDeIdempotencia. Nenhuma correspondência devolve
// domain.ErrImportacaoNaoEncontrada.
func (r *RepositorioConsulta) ImportacaoEquivalente(
	ctx context.Context, chave domain.ChaveDeIdempotencia,
) (domain.ResumoDaImportacao, error) {
	linha := r.consultador(ctx).QueryRow(ctx, sqlImportacaoEquivalente,
		chave.HashSHA256,
		chave.IDCaderno,
		paraDataPg(chave.DataCaderno),
	)

	resumo, err := lerResumo(linha)
	if errors.Is(err, pgx.ErrNoRows) {
		// O hash NÃO entra na mensagem: ele identifica o conteúdo submetido, e
		// mensagens de erro acabam em registro agregado.
		return domain.ResumoDaImportacao{}, fmt.Errorf(
			"caderno %d em %s: %w", chave.IDCaderno, chave.DataCaderno,
			domain.ErrImportacaoNaoEncontrada)
	}
	if err != nil {
		return domain.ResumoDaImportacao{}, fmt.Errorf(
			"%w: consultando importação equivalente: %w", domain.ErrPersistencia, err)
	}
	return resumo, nil
}

// lerResumo decodifica uma linha do SELECT de resumo.
//
// As três colunas anuláveis passam por tipos do pgtype porque a diferença entre
// NULO e zero é observável: total_recortes NULO é "ainda não terminou" e zero é
// "terminou sem ocorrências". Ver domain.ResumoDaImportacao.
func lerResumo(linha pgx.Row) (domain.ResumoDaImportacao, error) {
	var (
		id                   int64
		status               int32
		dataCaderno          pgtype.Date
		dataDisponibilizacao pgtype.Date
		dataInicio           pgtype.Timestamptz
		dataFim              pgtype.Timestamptz
		totalRecortes        pgtype.Int4
	)

	if err := linha.Scan(&id, &status, &dataCaderno, &dataDisponibilizacao,
		&dataInicio, &dataFim, &totalRecortes); err != nil {
		return domain.ResumoDaImportacao{}, err //nolint:wrapcheck // o chamador classifica ErrNoRows
	}

	resumo := domain.ResumoDaImportacao{
		ID:                   id,
		Status:               domain.StatusImportacao(status),
		DataCaderno:          daDataPg(dataCaderno),
		DataDisponibilizacao: daDataPg(dataDisponibilizacao),
		DataInicio:           doCarimbo(dataInicio),
		DataFim:              doCarimbo(dataFim),
	}
	if totalRecortes.Valid {
		v := totalRecortes.Int32
		resumo.TotalRecortes = &v
	}
	return resumo, nil
}

// -------------------------------------------------------------------------
// Varredura de órfãs — VARREDURA_ORFAS
// -------------------------------------------------------------------------

// ImportacoesPresas devolve, e TRANCA, importações paradas em 1, 2 ou 3.
//
// Só pode ser chamada DENTRO de uma transação: `FOR UPDATE SKIP LOCKED` sem
// transação tranca e destranca na mesma instrução, e duas instâncias
// concorrentes voltariam a ver as mesmas linhas. A verificação abaixo é o que
// impede esse uso — sem ela, o defeito só apareceria como trabalho duplicado em
// produção, que é o mais difícil de diagnosticar.
//
// As linhas devolvidas permanecem trancadas até o fim da transação, então o
// chamador deve tratá-las e encerrar sem demora.
func (r *RepositorioConsulta) ImportacoesPresas(
	ctx context.Context, antesDe time.Time, limite int32,
) ([]domain.ImportacaoPresa, error) {
	if _, emTransacao := daTransacao(ctx); !emTransacao {
		return nil, errors.New(
			"postgres: ImportacoesPresas exige transação — FOR UPDATE SKIP LOCKED " +
				"não tranca nada fora de uma")
	}

	status := make([]int32, 0, len(domain.StatusPresos))
	for _, s := range domain.StatusPresos {
		status = append(status, int32(s))
	}

	linhas, err := r.consultador(ctx).Query(ctx, sqlImportacoesPresas,
		status, antesDe.UTC(), limite)
	if err != nil {
		return nil, fmt.Errorf("%w: buscando importações presas: %w", domain.ErrPersistencia, err)
	}
	defer linhas.Close()

	var presas []domain.ImportacaoPresa
	for linhas.Next() {
		var (
			id         int64
			bruto      int32
			dataInicio pgtype.Timestamptz
		)
		if err := linhas.Scan(&id, &bruto, &dataInicio); err != nil {
			return nil, fmt.Errorf("%w: lendo importação presa: %w", domain.ErrPersistencia, err)
		}
		presas = append(presas, domain.ImportacaoPresa{
			ID:         id,
			Status:     domain.StatusImportacao(bruto),
			DataInicio: doCarimbo(dataInicio),
		})
	}
	if err := linhas.Err(); err != nil {
		return nil, fmt.Errorf("%w: percorrendo importações presas: %w", domain.ErrPersistencia, err)
	}
	return presas, nil
}

// -------------------------------------------------------------------------
// Conversões de tipo do driver
// -------------------------------------------------------------------------

// daDataPg converte a data do driver para a data pura do domínio.
//
// A leitura é o caminho inverso de paraDataPg e tem o mesmo cuidado com fuso: a
// coluna é `date`, o driver a entrega à meia-noite UTC, e os componentes são
// extraídos DAÍ — nunca de uma conversão para o fuso local, que deslocaria o dia
// (INV-P16).
func daDataPg(d pgtype.Date) domain.Data {
	if !d.Valid {
		return domain.Data{}
	}
	t := d.Time.UTC()
	// NovaData valida o intervalo; uma data vinda do PostgreSQL sempre passa,
	// então o erro é descartado com a data zero — que é o mesmo que "não
	// informada", e é o que a coluna nula já produziria.
	data, err := domain.NovaData(t.Year(), int(t.Month()), t.Day())
	if err != nil {
		return domain.Data{}
	}
	return data
}

// doCarimbo converte o carimbo do driver para ponteiro, preservando o NULO.
func doCarimbo(t pgtype.Timestamptz) *time.Time {
	if !t.Valid {
		return nil
	}
	v := t.Time
	return &v
}
