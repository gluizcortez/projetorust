// Os repositórios: a implementação das portas do domínio sobre PostgreSQL.
//
// Um arquivo por agregado dava quatro arquivos de meia centena de linhas cada,
// todos com o mesmo formato — pool, consulta embutida, mapeamento. Reunidos,
// a diferença entre eles fica visível de uma vez só.
//
// O SQL NÃO está aqui: vive em queries/, embutido por go:embed. Ver queries.go.
package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/gluizcortez/projetorust/src/domain"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
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

// RepositorioRecorte implementa domain.RepositorioRecorte sobre pgx.
type RepositorioRecorte struct {
	base
	uow domain.UnidadeDeTrabalho

	// emLote troca N pares de INSERT por DOIS comandos. EVOLUÇÃO da fase F11,
	// atrás de GRAVACAO_EM_LOTE; padrão false, que é o caminho do legado.
	//
	// O estado final no banco é o MESMO nas duas modalidades — mesmas linhas,
	// mesma associação, mesma contagem, mesmo dt_recorte. A única diferença
	// observável são os identificadores gerados, que já variam entre execuções.
	emLote bool
}

var _ domain.RepositorioRecorte = (*RepositorioRecorte)(nil)

// NovoRepositorioRecorte cria o repositório sobre o pool.
//
// As opções são variádicas para que a chamada existente, sem opção alguma,
// continue válida e continue significando "modo do legado".
func NovoRepositorioRecorte(
	pool *pgxpool.Pool, uow domain.UnidadeDeTrabalho, opcoes ...OpcaoDeRecorte,
) *RepositorioRecorte {
	r := &RepositorioRecorte{base: base{pool: pool}, uow: uow}
	for _, opcao := range opcoes {
		opcao(r)
	}
	return r
}

// OpcaoDeRecorte configura o repositório de recortes.
type OpcaoDeRecorte func(*RepositorioRecorte)

// ComGravacaoEmLote liga a gravação em lote. Ver RepositorioRecorte.emLote.
func ComGravacaoEmLote(ligado bool) OpcaoDeRecorte {
	return func(r *RepositorioRecorte) { r.emLote = ligado }
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
		var err error
		if r.emLote {
			contador, err = r.salvarEmLote(ctx, idImportacao, chave, recortes)
		} else {
			contador, err = r.salvarLinhaALinha(ctx, idImportacao, chave, recortes)
		}
		return err
	})
	if err != nil {
		return 0, fmt.Errorf("gravando %d recorte(s) do perfil %d na importação %d: %w",
			len(recortes), chave.IDPerfil, idImportacao, err)
	}

	return contador, nil
}

// salvarLinhaALinha é o caminho do LEGADO: um par de INSERT por recorte.
//
// Roda dentro da transação aberta por Salvar.
func (r *RepositorioRecorte) salvarLinhaALinha(
	ctx context.Context,
	idImportacao int64,
	chave domain.ChavePesquisa,
	recortes []domain.Recorte,
) (int32, error) {
	q := r.consultador(ctx)

	var contador int32
	for _, recorte := range recortes {
		// main.rs:603 — a conversão FALHA em vez de truncar (INV-P15).
		nrPagina, err := domain.ParaInt64(recorte.Pagina)
		if err != nil {
			return 0, fmt.Errorf("página %d da importação %d: %w", recorte.Pagina, idImportacao, err)
		}

		var idRecorte int64
		if err := q.QueryRow(ctx, sqlRecorteInserir,
			idImportacao,
			nrPagina,
			chave.IDPerfil,
			chave.Expressao,
		).Scan(&idRecorte); err != nil {
			return 0, fmt.Errorf("%w: inserindo recorte: %w", domain.ErrPersistencia, err)
		}

		// main.rs:619 — grava `highlight`, não `text`.
		if _, err := q.Exec(ctx, sqlRecorteTextoInserir, idRecorte, recorte.Destaque); err != nil {
			return 0, fmt.Errorf("%w: inserindo texto do recorte %d: %w",
				domain.ErrPersistencia, idRecorte, err)
		}

		contador++
	}
	return contador, nil
}

// salvarEmLote é a EVOLUÇÃO: dois comandos, qualquer que seja a quantidade.
//
// # Por que não pgx.CopyFrom
//
// Porque `COPY` não devolve os identificadores gerados, e tb_recorte_texto
// precisa deles. Seriam necessárias duas passagens — copiar e depois reler o
// que foi copiado —, e a releitura não teria como distinguir estas linhas de
// outras da mesma importação inseridas por uma chave anterior. O INSERT com
// `unnest ... RETURNING` faz em um comando o que o COPY faria em três.
//
// # A ordem é conferida, não presumida
//
// `WITH ORDINALITY` mais `ORDER BY` faz o PostgreSQL inserir na ordem da
// entrada, e o `RETURNING` sai na ordem de inserção. Ainda assim a página volta
// junto e é CONFERIDA contra a esperada: se alguma versão futura do servidor
// reordenar, o erro aparece aqui, na hora, em vez de virar texto de recorte
// associado à página errada — que é corrupção silenciosa de dados.
//
// # INV-P15 continua valendo
//
// A conversão de página acontece ANTES de qualquer comando. Falhando, nada é
// executado — e como o modo linha a linha também reverte a transação inteira, o
// estado final do banco é idêntico: nenhuma linha.
func (r *RepositorioRecorte) salvarEmLote(
	ctx context.Context,
	idImportacao int64,
	chave domain.ChavePesquisa,
	recortes []domain.Recorte,
) (int32, error) {
	paginas := make([]int64, len(recortes))
	for i, recorte := range recortes {
		nrPagina, err := domain.ParaInt64(recorte.Pagina)
		if err != nil {
			return 0, fmt.Errorf("página %d da importação %d: %w", recorte.Pagina, idImportacao, err)
		}
		paginas[i] = nrPagina
	}

	q := r.consultador(ctx)

	linhas, err := q.Query(ctx, sqlRecorteInserirLote,
		idImportacao, paginas, chave.IDPerfil, chave.Expressao)
	if err != nil {
		return 0, fmt.Errorf("%w: inserindo lote de recortes: %w", domain.ErrPersistencia, err)
	}

	identificadores := make([]int64, 0, len(recortes))
	textos := make([]string, 0, len(recortes))
	for linhas.Next() {
		var idRecorte, nrPagina int64
		if err := linhas.Scan(&idRecorte, &nrPagina); err != nil {
			linhas.Close()
			return 0, fmt.Errorf("%w: lendo identificador de recorte: %w",
				domain.ErrPersistencia, err)
		}

		i := len(identificadores)
		if i >= len(paginas) || nrPagina != paginas[i] {
			linhas.Close()
			return 0, fmt.Errorf(
				"%w: o lote voltou fora de ordem na posição %d (página %d, esperada %d)",
				domain.ErrPersistencia, i, nrPagina, paginaEsperada(paginas, i))
		}

		identificadores = append(identificadores, idRecorte)
		textos = append(textos, recortes[i].Destaque)
	}
	linhas.Close()
	if err := linhas.Err(); err != nil {
		return 0, fmt.Errorf("%w: percorrendo o lote de recortes: %w", domain.ErrPersistencia, err)
	}

	if len(identificadores) != len(recortes) {
		return 0, fmt.Errorf("%w: o lote inseriu %d de %d recorte(s)",
			domain.ErrPersistencia, len(identificadores), len(recortes))
	}

	// main.rs:619 — grava `highlight`, não `text`. O mesmo campo do modo linha a
	// linha, na mesma ordem.
	if _, err := q.Exec(ctx, sqlRecorteTextoInserirLote, identificadores, textos); err != nil {
		return 0, fmt.Errorf("%w: inserindo lote de textos: %w", domain.ErrPersistencia, err)
	}

	// A conversão não pode estourar: len(recortes) veio de uma fatia que a
	// própria página do documento limita, e o modo linha a linha incrementa um
	// int32 sobre a mesma fatia.
	return int32(len(identificadores)), nil //nolint:gosec // limitado por len(recortes), idêntico ao modo linha a linha
}

// paginaEsperada devolve a página esperada na posição, ou -1 fora da faixa.
// Existe só para que a mensagem de erro acima não precise de um `if`.
func paginaEsperada(paginas []int64, i int) int64 {
	if i < len(paginas) {
		return paginas[i]
	}
	return -1
}

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
