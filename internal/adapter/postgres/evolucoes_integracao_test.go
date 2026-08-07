//go:build integration

package postgres_test

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/gluizcortez/projetorust/internal/adapter/postgres"
	"github.com/gluizcortez/projetorust/internal/domain"
)

// Testes de integração das evoluções da fase F11.
//
// Três delas — gravação em lote, idempotência e varredura de órfãs — só podem
// ser verificadas contra um PostgreSQL de verdade: a primeira depende de
// `unnest ... WITH ORDINALITY`, a segunda de um índice, e a terceira de
// `FOR UPDATE SKIP LOCKED`, que nenhum dublê reproduz.

// -------------------------------------------------------------------------
// GRAVACAO_EM_LOTE
// -------------------------------------------------------------------------

// linhaDeRecorte é o que se compara entre as duas modalidades.
//
// O identificador gerado NÃO entra: ele vem de uma sequência e difere entre
// execuções. Tudo o mais entra, inclusive o texto, que é o que a associação
// entre as duas tabelas determina.
type linhaDeRecorte struct {
	NrPagina       int64
	IDPerfil       int64
	ExpressaoBusca string
	Origem         string
	Texto          string
}

// lerRecortesDe devolve o estado observável dos recortes de uma importação.
func lerRecortesDe(t *testing.T, pool *pgxpool.Pool, idImportacao int64) []linhaDeRecorte {
	t.Helper()
	linhas, err := pool.Query(context.Background(), `
		SELECT r.nr_pagina, r.id_perfil, r.expressao_busca, t.origem, t.recorte
		FROM recorte.tb_recorte r
		JOIN recorte.tb_recorte_texto t ON t.id_recorte = r.id_recorte
		WHERE r.id_importacao = $1
		ORDER BY r.id_recorte`, idImportacao)
	if err != nil {
		t.Fatalf("lendo recortes: %v", err)
	}
	defer linhas.Close()

	var saida []linhaDeRecorte
	for linhas.Next() {
		var l linhaDeRecorte
		if err := linhas.Scan(&l.NrPagina, &l.IDPerfil, &l.ExpressaoBusca, &l.Origem, &l.Texto); err != nil {
			t.Fatalf("lendo linha: %v", err)
		}
		saida = append(saida, l)
	}
	if err := linhas.Err(); err != nil {
		t.Fatalf("percorrendo: %v", err)
	}
	return saida
}

func recortesDeTeste(n int) []domain.Recorte {
	saida := make([]domain.Recorte, 0, n)
	for i := range n {
		saida = append(saida, domain.Recorte{
			Pagina:   uint64(i + 1),
			Destaque: "texto integral da página " + strings.Repeat("x", i%7) + " " + string(rune('a'+i%26)),
		})
	}
	return saida
}

// TestIntegracaoLoteProduzOMesmoEstadoQueLinhaALinha é o critério de aceite
// literal: o estado final do banco é igual, desconsiderando apenas os
// identificadores gerados.
func TestIntegracaoLoteProduzOMesmoEstadoQueLinhaALinha(t *testing.T) {
	pool := bancoLimpo(t)
	ctx := context.Background()

	uow := postgres.NovaUnidadeDeTrabalho(pool)
	importacoes := postgres.NovoRepositorioImportacao(pool)

	chave := domain.ChavePesquisa{IDPerfil: 1, Expressao: "fulano de tal"}
	recortes := recortesDeTeste(37)

	// Duas importações distintas, a MESMA entrada, modalidades diferentes.
	idLinha, err := importacoes.Registrar(ctx, importacaoDeTeste(t))
	if err != nil {
		t.Fatalf("registrando importação linha a linha: %v", err)
	}
	idLote, err := importacoes.Registrar(ctx, importacaoDeTeste(t))
	if err != nil {
		t.Fatalf("registrando importação em lote: %v", err)
	}

	repoLinha := postgres.NovoRepositorioRecorte(pool, uow)
	repoLote := postgres.NovoRepositorioRecorte(pool, uow, postgres.ComGravacaoEmLote(true))

	nLinha, err := repoLinha.Salvar(ctx, idLinha, chave, recortes)
	if err != nil {
		t.Fatalf("Salvar linha a linha: %v", err)
	}
	nLote, err := repoLote.Salvar(ctx, idLote, chave, recortes)
	if err != nil {
		t.Fatalf("Salvar em lote: %v", err)
	}

	if nLinha != nLote {
		t.Errorf("contagem devolvida: linha a linha %d, lote %d", nLinha, nLote)
	}
	if int(nLote) != len(recortes) {
		t.Errorf("contagem = %d; esperava %d", nLote, len(recortes))
	}

	esperado := lerRecortesDe(t, pool, idLinha)
	obtido := lerRecortesDe(t, pool, idLote)

	if len(esperado) != len(obtido) {
		t.Fatalf("linhas: linha a linha %d, lote %d", len(esperado), len(obtido))
	}
	for i := range esperado {
		if esperado[i] != obtido[i] {
			t.Errorf("linha %d divergiu:\n  linha a linha: %+v\n  lote:          %+v",
				i, esperado[i], obtido[i])
		}
	}
}

// TestIntegracaoLotePreservaAOrdemComPaginasEmbaralhadas.
//
// O pareamento entre identificador gerado e texto é POSICIONAL. Uma reordenação
// do RETURNING associaria o texto de uma página ao recorte de outra — corrupção
// silenciosa, que nenhuma contagem detecta. As páginas fora de ordem crescente
// são o que torna a asserção capaz de enxergar isso.
func TestIntegracaoLotePreservaAOrdemComPaginasEmbaralhadas(t *testing.T) {
	pool := bancoLimpo(t)
	ctx := context.Background()

	uow := postgres.NovaUnidadeDeTrabalho(pool)
	importacoes := postgres.NovoRepositorioImportacao(pool)
	id, err := importacoes.Registrar(ctx, importacaoDeTeste(t))
	if err != nil {
		t.Fatalf("registrando: %v", err)
	}

	// Páginas deliberadamente fora de ordem, e cada texto nomeia sua página.
	paginas := []uint64{9, 3, 41, 1, 27, 15, 2}
	recortes := make([]domain.Recorte, 0, len(paginas))
	for _, p := range paginas {
		recortes = append(recortes, domain.Recorte{
			Pagina:   p,
			Destaque: "conteudo-da-pagina-" + itoa(int64(p)),
		})
	}

	repo := postgres.NovoRepositorioRecorte(pool, uow, postgres.ComGravacaoEmLote(true))
	if _, err := repo.Salvar(ctx, id, domain.ChavePesquisa{IDPerfil: 1, Expressao: "x"}, recortes); err != nil {
		t.Fatalf("Salvar em lote: %v", err)
	}

	for _, linha := range lerRecortesDe(t, pool, id) {
		esperado := "conteudo-da-pagina-" + itoa(linha.NrPagina)
		if linha.Texto != esperado {
			t.Errorf("página %d recebeu o texto %q; esperava %q",
				linha.NrPagina, linha.Texto, esperado)
		}
	}
}

func itoa(v int64) string {
	if v == 0 {
		return "0"
	}
	var b []byte
	for v > 0 {
		b = append([]byte{byte('0' + v%10)}, b...)
		v /= 10
	}
	return string(b)
}

// TestIntegracaoLoteVazioNaoTocaNoBanco: a fatia vazia devolve 0 nas duas
// modalidades, sem abrir transação.
func TestIntegracaoLoteVazioNaoTocaNoBanco(t *testing.T) {
	pool := bancoLimpo(t)
	ctx := context.Background()
	uow := postgres.NovaUnidadeDeTrabalho(pool)

	for _, emLote := range []bool{false, true} {
		repo := postgres.NovoRepositorioRecorte(pool, uow, postgres.ComGravacaoEmLote(emLote))
		n, err := repo.Salvar(ctx, 1, domain.ChavePesquisa{IDPerfil: 1, Expressao: "x"}, nil)
		if err != nil {
			t.Errorf("emLote=%t: %v", emLote, err)
		}
		if n != 0 {
			t.Errorf("emLote=%t devolveu %d", emLote, n)
		}
	}
}

// -------------------------------------------------------------------------
// IDEMPOTENCIA_POR_HASH
// -------------------------------------------------------------------------

func importacaoCom(t *testing.T, hash string, idCaderno int32, ano, mes, dia int) domain.Importacao {
	t.Helper()
	d, err := domain.NovaData(ano, mes, dia)
	if err != nil {
		t.Fatalf("NovaData: %v", err)
	}
	return domain.NovaImportacao(44521, idCaderno, d, d, "diario.pdf", hash)
}

// TestIntegracaoImportacaoEquivalenteExigeOsTresCampos.
//
// Só o hash não basta: o mesmo arquivo pode ser submetido legitimamente para
// outro caderno ou outra data, e tratá-los como repetição PERDERIA importações.
func TestIntegracaoImportacaoEquivalenteExigeOsTresCampos(t *testing.T) {
	pool := bancoLimpo(t)
	ctx := context.Background()

	escrita := postgres.NovoRepositorioImportacao(pool)
	leitura := postgres.NovoRepositorioConsulta(pool)

	const hash = "deadbeef"
	id, err := escrita.Registrar(ctx, importacaoCom(t, hash, 1, 2024, 3, 15))
	if err != nil {
		t.Fatalf("registrando: %v", err)
	}

	data, _ := domain.NovaData(2024, 3, 15)
	outraData, _ := domain.NovaData(2024, 3, 16)

	casos := []struct {
		nome  string
		chave domain.ChaveDeIdempotencia
		acha  bool
	}{
		{"tudo igual", domain.ChaveDeIdempotencia{HashSHA256: hash, IDCaderno: 1, DataCaderno: data}, true},
		{"outro hash", domain.ChaveDeIdempotencia{HashSHA256: "outro", IDCaderno: 1, DataCaderno: data}, false},
		{"outro caderno", domain.ChaveDeIdempotencia{HashSHA256: hash, IDCaderno: 2, DataCaderno: data}, false},
		{"outra data", domain.ChaveDeIdempotencia{HashSHA256: hash, IDCaderno: 1, DataCaderno: outraData}, false},
	}

	for _, caso := range casos {
		t.Run(caso.nome, func(t *testing.T) {
			resumo, err := leitura.ImportacaoEquivalente(ctx, caso.chave)
			if !caso.acha {
				if !errors.Is(err, domain.ErrImportacaoNaoEncontrada) {
					t.Fatalf("erro = %v; esperava ErrImportacaoNaoEncontrada", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("ImportacaoEquivalente: %v", err)
			}
			if resumo.ID != id {
				t.Errorf("id = %d; esperava %d", resumo.ID, id)
			}
		})
	}
}

// TestIntegracaoImportacaoEquivalenteDevolveAMaisRecente.
//
// Duplicatas existem legitimamente no histórico: o legado nunca deduplicou.
func TestIntegracaoImportacaoEquivalenteDevolveAMaisRecente(t *testing.T) {
	pool := bancoLimpo(t)
	ctx := context.Background()

	escrita := postgres.NovoRepositorioImportacao(pool)
	leitura := postgres.NovoRepositorioConsulta(pool)

	const hash = "cafe"
	var ultimo int64
	for range 4 {
		id, err := escrita.Registrar(ctx, importacaoCom(t, hash, 1, 2024, 3, 15))
		if err != nil {
			t.Fatalf("registrando: %v", err)
		}
		ultimo = id
	}

	data, _ := domain.NovaData(2024, 3, 15)
	resumo, err := leitura.ImportacaoEquivalente(ctx,
		domain.ChaveDeIdempotencia{HashSHA256: hash, IDCaderno: 1, DataCaderno: data})
	if err != nil {
		t.Fatalf("ImportacaoEquivalente: %v", err)
	}
	if resumo.ID != ultimo {
		t.Errorf("id = %d; esperava a mais recente, %d", resumo.ID, ultimo)
	}
}

// TestIntegracaoMigracaoDoIndiceEhReversivel aplica e reverte a 0002.
//
// As duas instruções usam CONCURRENTLY e por isso NÃO podem rodar em
// transação — é justamente o que este teste verifica ao executá-las pelo pool,
// em autocommit.
func TestIntegracaoMigracaoDoIndiceEhReversivel(t *testing.T) {
	pool := bancoLimpo(t)
	ctx := context.Background()

	existe := func() bool {
		var n int
		err := pool.QueryRow(ctx, `
			SELECT count(*) FROM pg_indexes
			WHERE schemaname = 'recorte' AND indexname = 'ix_importacao_idempotencia'`).Scan(&n)
		if err != nil {
			t.Fatalf("consultando índices: %v", err)
		}
		return n > 0
	}

	aplicar := func(arquivo string) {
		t.Helper()
		bruto, err := os.ReadFile("../../../db/migrations/" + arquivo)
		if err != nil {
			t.Fatalf("lendo %s: %v", arquivo, err)
		}
		if _, err := pool.Exec(ctx, string(bruto)); err != nil {
			t.Fatalf("aplicando %s: %v", arquivo, err)
		}
	}

	if existe() {
		t.Fatal("o índice já existia antes da migração")
	}

	aplicar("0002_idempotencia_indice.up.sql")
	if !existe() {
		t.Fatal("a migração não criou o índice")
	}

	// Aplicar de novo tem de ser inofensivo (IF NOT EXISTS).
	aplicar("0002_idempotencia_indice.up.sql")

	aplicar("0002_idempotencia_indice.down.sql")
	if existe() {
		t.Fatal("a reversão não removeu o índice")
	}
	// Reverter de novo também.
	aplicar("0002_idempotencia_indice.down.sql")
}

// TestIntegracaoIndiceNaoImpedeDuplicatas.
//
// O índice NÃO é único de propósito: um banco de produção já tem duplicatas, e
// um índice único passaria a recusar reenvios que hoje são aceitos — mudança de
// comportamento com a chave DESLIGADA.
func TestIntegracaoIndiceNaoImpedeDuplicatas(t *testing.T) {
	pool := bancoLimpo(t)
	ctx := context.Background()

	bruto, err := os.ReadFile("../../../db/migrations/0002_idempotencia_indice.up.sql")
	if err != nil {
		t.Fatalf("lendo a migração: %v", err)
	}
	if _, err := pool.Exec(ctx, string(bruto)); err != nil {
		t.Fatalf("aplicando: %v", err)
	}

	escrita := postgres.NovoRepositorioImportacao(pool)
	for i := range 3 {
		if _, err := escrita.Registrar(ctx, importacaoCom(t, "mesmo-hash", 1, 2024, 3, 15)); err != nil {
			t.Fatalf("registro %d recusado pelo índice: %v", i, err)
		}
	}
}

// -------------------------------------------------------------------------
// STATUS_ENDPOINT — consulta de resumo
// -------------------------------------------------------------------------

// TestIntegracaoConsultarResumo percorre o ciclo de vida e confere o que a
// consulta enxerga em cada ponto.
func TestIntegracaoConsultarResumo(t *testing.T) {
	pool := bancoLimpo(t)
	ctx := context.Background()

	escrita := postgres.NovoRepositorioImportacao(pool)
	leitura := postgres.NovoRepositorioConsulta(pool)

	id, err := escrita.Registrar(ctx, importacaoCom(t, "abc", 1, 2024, 3, 15))
	if err != nil {
		t.Fatalf("registrando: %v", err)
	}

	// Recém-registrada: sem data_inicio, sem data_fim, sem total.
	resumo, err := leitura.Consultar(ctx, id)
	if err != nil {
		t.Fatalf("Consultar: %v", err)
	}
	if resumo.Status != domain.StatusRecebido {
		t.Errorf("status = %s; esperava %s", resumo.Status, domain.StatusRecebido)
	}
	if resumo.DataCaderno.String() != "2024-03-15" {
		t.Errorf("data_caderno = %q", resumo.DataCaderno)
	}
	if resumo.DataInicio != nil || resumo.DataFim != nil {
		t.Errorf("carimbos = %v e %v; esperava nulos", resumo.DataInicio, resumo.DataFim)
	}
	if resumo.TotalRecortes != nil {
		t.Errorf("total = %v; esperava nulo — ainda não terminou", *resumo.TotalRecortes)
	}
	if resumo.Concluida() {
		t.Error("recém-registrada não pode estar concluída")
	}

	// Terminada com ZERO recortes: o total passa a ser 0, que é DIFERENTE de
	// nulo. É a distinção que INV-P20 torna importante.
	if err := escrita.MarcarInicio(ctx, id); err != nil {
		t.Fatalf("MarcarInicio: %v", err)
	}
	if err := escrita.AtualizarStatus(ctx, id, domain.StatusFinalizado); err != nil {
		t.Fatalf("AtualizarStatus: %v", err)
	}
	if err := escrita.MarcarTermino(ctx, id, 0); err != nil {
		t.Fatalf("MarcarTermino: %v", err)
	}

	resumo, err = leitura.Consultar(ctx, id)
	if err != nil {
		t.Fatalf("Consultar: %v", err)
	}
	if resumo.TotalRecortes == nil {
		t.Fatal("total = nulo; depois de MarcarTermino tem de ser 0")
	}
	if *resumo.TotalRecortes != 0 {
		t.Errorf("total = %d; esperava 0", *resumo.TotalRecortes)
	}
	if resumo.DataInicio == nil || resumo.DataFim == nil {
		t.Error("os carimbos deveriam estar preenchidos")
	}
	if !resumo.Concluida() {
		t.Error("status 5 é concluída")
	}
}

// TestIntegracaoConsultarInexistente.
func TestIntegracaoConsultarInexistente(t *testing.T) {
	pool := bancoLimpo(t)
	leitura := postgres.NovoRepositorioConsulta(pool)

	_, err := leitura.Consultar(context.Background(), 999999)
	if !errors.Is(err, domain.ErrImportacaoNaoEncontrada) {
		t.Errorf("erro = %v; esperava ErrImportacaoNaoEncontrada", err)
	}
}

// -------------------------------------------------------------------------
// VARREDURA_ORFAS
// -------------------------------------------------------------------------

// prepararPresas registra importações e as deixa no status pedido, com
// data_inicio recuada no tempo.
func prepararPresas(
	t *testing.T, pool *pgxpool.Pool, quantidade int,
	status domain.StatusImportacao, idade time.Duration,
) []int64 {
	t.Helper()
	ctx := context.Background()
	escrita := postgres.NovoRepositorioImportacao(pool)

	ids := make([]int64, 0, quantidade)
	for i := range quantidade {
		id, err := escrita.Registrar(ctx, importacaoCom(t, "h"+itoa(int64(i)), 1, 2024, 3, 15))
		if err != nil {
			t.Fatalf("registrando: %v", err)
		}
		if err := escrita.AtualizarStatus(ctx, id, status); err != nil {
			t.Fatalf("AtualizarStatus: %v", err)
		}
		// data_inicio é gravada pelo servidor; recuá-la é o que simula a idade.
		if _, err := pool.Exec(ctx, `
			UPDATE recorte.tb_importacao
			SET data_inicio = current_timestamp - $2::interval
			WHERE id_importacao = $1`, id, idade.String()); err != nil {
			t.Fatalf("recuando data_inicio: %v", err)
		}
		ids = append(ids, id)
	}
	return ids
}

// TestIntegracaoPresasExigeTransacao.
//
// `FOR UPDATE SKIP LOCKED` fora de transação tranca e destranca na mesma
// instrução, e duas instâncias concorrentes voltariam a ver as mesmas linhas —
// exatamente o defeito que a consulta existe para evitar. A guarda transforma
// isso em erro imediato, em vez de trabalho duplicado em produção.
func TestIntegracaoPresasExigeTransacao(t *testing.T) {
	pool := bancoLimpo(t)
	leitura := postgres.NovoRepositorioConsulta(pool)

	_, err := leitura.ImportacoesPresas(context.Background(), time.Now(), 10)
	if err == nil {
		t.Fatal("ImportacoesPresas aceitou ser chamada fora de transação")
	}
	if !strings.Contains(err.Error(), "transação") {
		t.Errorf("erro = %v; deveria explicar a exigência", err)
	}
}

// TestIntegracaoPresasRespeitaOLimiar.
func TestIntegracaoPresasRespeitaOLimiar(t *testing.T) {
	pool := bancoLimpo(t)
	ctx := context.Background()

	antigas := prepararPresas(t, pool, 3, domain.StatusIndexando, 3*time.Hour)
	prepararPresas(t, pool, 4, domain.StatusIndexando, time.Minute) // recentes

	uow := postgres.NovaUnidadeDeTrabalho(pool)
	leitura := postgres.NovoRepositorioConsulta(pool)

	var encontradas []domain.ImportacaoPresa
	err := uow.EmTransacao(ctx, func(ctx context.Context) error {
		var err error
		encontradas, err = leitura.ImportacoesPresas(ctx, time.Now().Add(-time.Hour), 100)
		return err
	})
	if err != nil {
		t.Fatalf("EmTransacao: %v", err)
	}

	if len(encontradas) != len(antigas) {
		t.Fatalf("encontradas = %d; esperava %d — as recentes não podem entrar",
			len(encontradas), len(antigas))
	}
	vistos := map[int64]bool{}
	for _, p := range encontradas {
		vistos[p.ID] = true
		if p.Status != domain.StatusIndexando {
			t.Errorf("importação %d veio com status %s", p.ID, p.Status)
		}
		if p.DataInicio == nil {
			t.Errorf("importação %d veio sem data_inicio", p.ID)
		}
	}
	for _, id := range antigas {
		if !vistos[id] {
			t.Errorf("a importação antiga %d não foi encontrada", id)
		}
	}
}

// TestIntegracaoPresasIgnoraStatusTerminaisEZero.
//
// Status 0 fica de fora porque pode estar legitimamente na fila do executor; -1
// e 5 porque já terminaram.
func TestIntegracaoPresasIgnoraStatusTerminaisEZero(t *testing.T) {
	pool := bancoLimpo(t)
	ctx := context.Background()

	for _, status := range []domain.StatusImportacao{
		domain.StatusRecebido, domain.StatusErro, domain.StatusFinalizado,
	} {
		prepararPresas(t, pool, 2, status, 5*time.Hour)
	}
	esperadas := prepararPresas(t, pool, 3, domain.StatusRecortando, 5*time.Hour)

	uow := postgres.NovaUnidadeDeTrabalho(pool)
	leitura := postgres.NovoRepositorioConsulta(pool)

	var encontradas []domain.ImportacaoPresa
	if err := uow.EmTransacao(ctx, func(ctx context.Context) error {
		var err error
		encontradas, err = leitura.ImportacoesPresas(ctx, time.Now().Add(-time.Hour), 100)
		return err
	}); err != nil {
		t.Fatalf("EmTransacao: %v", err)
	}

	if len(encontradas) != len(esperadas) {
		t.Errorf("encontradas = %d; esperava %d (só 1, 2 e 3 são varridos)",
			len(encontradas), len(esperadas))
	}
}

// TestIntegracaoPresasIgnoraDataInicioNula.
//
// Uma importação em status 1..3 sem data_inicio travou ANTES do UPDATE, e não
// há nela carimbo com que medir idade. Varrê-la seria arriscar derrubar uma
// importação que acabou de começar.
func TestIntegracaoPresasIgnoraDataInicioNula(t *testing.T) {
	pool := bancoLimpo(t)
	ctx := context.Background()

	ids := prepararPresas(t, pool, 2, domain.StatusIndexando, 5*time.Hour)
	if _, err := pool.Exec(ctx, `
		UPDATE recorte.tb_importacao SET data_inicio = NULL WHERE id_importacao = $1`,
		ids[0]); err != nil {
		t.Fatalf("anulando data_inicio: %v", err)
	}

	uow := postgres.NovaUnidadeDeTrabalho(pool)
	leitura := postgres.NovoRepositorioConsulta(pool)

	var encontradas []domain.ImportacaoPresa
	if err := uow.EmTransacao(ctx, func(ctx context.Context) error {
		var err error
		encontradas, err = leitura.ImportacoesPresas(ctx, time.Now().Add(-time.Hour), 100)
		return err
	}); err != nil {
		t.Fatalf("EmTransacao: %v", err)
	}

	if len(encontradas) != 1 || encontradas[0].ID != ids[1] {
		t.Errorf("encontradas = %+v; esperava apenas a importação %d", encontradas, ids[1])
	}
}

// TestIntegracaoDuasInstanciasNaoPegamAMesmaImportacao é o critério de aceite
// literal da varredura: com duas instâncias concorrentes, NENHUMA importação é
// processada duas vezes.
//
// As duas transações são abertas ao MESMO TEMPO e cada uma segura suas linhas
// até a outra ter terminado a busca — é assim que o teste força a disputa real
// em vez de torcer pelo escalonador.
//
// O LOTE é metade do acervo, e isso é essencial: com lote igual ao total, a
// primeira instância levaria tudo e a segunda voltaria vazia — comportamento
// CORRETO, mas que não exercita a disputa. Em produção o lote também é menor
// que o acúmulo, senão não haveria por que limitá-lo.
func TestIntegracaoDuasInstanciasNaoPegamAMesmaImportacao(t *testing.T) {
	pool := bancoLimpo(t)
	ctx := context.Background()

	const total = 20
	const lote = total / 2
	prepararPresas(t, pool, total, domain.StatusIndexando, 5*time.Hour)

	uow := postgres.NovaUnidadeDeTrabalho(pool)
	leitura := postgres.NovoRepositorioConsulta(pool)

	// A barreira faz as duas transações buscarem antes de qualquer uma soltar
	// as travas. Sem ela, a primeira poderia terminar e comitar antes de a
	// segunda começar, e o teste passaria sem nunca ter havido concorrência.
	var barreira sync.WaitGroup
	barreira.Add(2)

	tomadas := make([][]int64, 2)
	erros := make([]error, 2)
	var grupo sync.WaitGroup

	for i := range 2 {
		grupo.Add(1)
		go func(i int) {
			defer grupo.Done()
			// O PRAZO é o que transforma um `SKIP LOCKED` ausente em FALHA em
			// vez de travamento: sem ele, um `FOR UPDATE` simples faria a
			// segunda instância esperar pela primeira, que por sua vez espera na
			// barreira, e o teste penduraria até o tempo limite do `go test`.
			ctx, cancelar := context.WithTimeout(ctx, 15*time.Second)
			defer cancelar()

			erros[i] = uow.EmTransacao(ctx, func(ctx context.Context) error {
				presas, err := leitura.ImportacoesPresas(ctx, time.Now().Add(-time.Hour), lote)
				if err != nil {
					barreira.Done()
					return err
				}
				for _, p := range presas {
					tomadas[i] = append(tomadas[i], p.ID)
				}
				// Só solta as travas depois que a outra já buscou.
				barreira.Done()
				barreira.Wait()
				return nil
			})
		}(i)
	}
	grupo.Wait()

	for i, err := range erros {
		if err != nil {
			t.Fatalf("instância %d: %v", i, err)
		}
	}

	// A asserção central: nenhuma importação em duas instâncias.
	dono := map[int64]int{}
	for i, ids := range tomadas {
		for _, id := range ids {
			if outra, jaTem := dono[id]; jaTem {
				t.Errorf("a importação %d foi tomada pelas instâncias %d e %d", id, outra, i)
			}
			dono[id] = i
		}
	}

	// E juntas elas cobrem tudo — SKIP LOCKED PULA a linha trancada, não a
	// perde: quem chegou depois seguiu para as seguintes em vez de esperar.
	if len(dono) != total {
		t.Errorf("as duas instâncias cobriram %d de %d importações", len(dono), total)
	}
	for i, ids := range tomadas {
		if len(ids) != lote {
			t.Errorf("instância %d tomou %d importações; esperava %d", i, len(ids), lote)
		}
	}
}

// TestIntegracaoPresasRespeitaOLote: o lote limita por quanto tempo as linhas
// ficam trancadas.
func TestIntegracaoPresasRespeitaOLote(t *testing.T) {
	pool := bancoLimpo(t)
	ctx := context.Background()

	prepararPresas(t, pool, 10, domain.StatusIndexando, 5*time.Hour)

	uow := postgres.NovaUnidadeDeTrabalho(pool)
	leitura := postgres.NovoRepositorioConsulta(pool)

	var encontradas []domain.ImportacaoPresa
	if err := uow.EmTransacao(ctx, func(ctx context.Context) error {
		var err error
		encontradas, err = leitura.ImportacoesPresas(ctx, time.Now().Add(-time.Hour), 4)
		return err
	}); err != nil {
		t.Fatalf("EmTransacao: %v", err)
	}

	if len(encontradas) != 4 {
		t.Errorf("encontradas = %d; o lote era 4", len(encontradas))
	}
}
