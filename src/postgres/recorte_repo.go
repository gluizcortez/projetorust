package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/gluizcortez/projetorust/src/domain"
)

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
