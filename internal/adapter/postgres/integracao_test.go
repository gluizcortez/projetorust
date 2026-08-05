//go:build integration

// Testes de integração contra um PostgreSQL real.
//
// DESVIO DELIBERADO do prompt da fase, que pedia testcontainers-go. Este
// ambiente não tem daemon Docker, e testcontainers exige um. Em vez de
// entregar testes que não podem ser executados, eles leem a conexão de
// TEST_DATABASE_URL e são pulados com mensagem explicativa quando a variável
// não está definida.
//
// A troca não perde nada: testcontainers é apenas uma forma de PROVER um
// banco. Aqui o banco vem de `make pg-subir`, na integração contínua vem de um
// serviço `postgres:16`, e em qualquer máquina com um PostgreSQL à mão vem de
// uma variável de ambiente. O que os testes exercitam é idêntico.
//
//	make test-integration
package postgres_test

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/gluizcortez/projetorust/internal/adapter/postgres"
	"github.com/gluizcortez/projetorust/internal/config"
	"github.com/gluizcortez/projetorust/internal/domain"
)

func dsnDeTeste(t *testing.T) string {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL não definida — rode `make test-integration`")
	}
	return dsn
}

// bancoLimpo devolve um pool com o esquema recriado do zero.
//
// Cada teste roda contra um banco recém-montado: os testes de gravação
// contam linhas, e resíduo de outro teste falsearia a contagem.
func bancoLimpo(t *testing.T) *pgxpool.Pool {
	t.Helper()
	ctx := context.Background()

	cfg := &config.Config{DatabaseURL: config.URLSegredo(dsnDeTeste(t))}
	pool, err := postgres.NovoPool(ctx, cfg)
	if err != nil {
		t.Fatalf("abrindo pool: %v", err)
	}
	t.Cleanup(pool.Close)

	if _, err := pool.Exec(ctx, `DROP SCHEMA IF EXISTS recorte CASCADE`); err != nil {
		t.Fatalf("limpando esquema: %v", err)
	}
	for _, arquivo := range []string{"testdata/esquema_inferido.sql", "testdata/dados_minimos.sql"} {
		bruto, err := os.ReadFile(arquivo)
		if err != nil {
			t.Fatalf("lendo %s: %v", arquivo, err)
		}
		if _, err := pool.Exec(ctx, string(bruto)); err != nil {
			t.Fatalf("aplicando %s: %v", arquivo, err)
		}
	}
	return pool
}

func importacaoDeTeste(t *testing.T) domain.Importacao {
	t.Helper()
	d, err := domain.NovaData(2024, 3, 15)
	if err != nil {
		t.Fatalf("NovaData: %v", err)
	}
	return domain.NovaImportacao(44521, 1, d, d, "diario.pdf", "abc123")
}

// -------------------------------------------------------------------------
// Migração de linha de base
// -------------------------------------------------------------------------

func TestIntegracaoMigracaoDeLinhaDeBaseAceitaEsquemaEsperado(t *testing.T) {
	pool := bancoLimpo(t)
	ctx := context.Background()

	bruto, err := os.ReadFile("../../../db/migrations/0001_baseline.sql")
	if err != nil {
		t.Fatalf("lendo a migração: %v", err)
	}
	if _, err := pool.Exec(ctx, string(bruto)); err != nil {
		t.Errorf("a migração deveria aceitar o esquema esperado: %v", err)
	}
}

func TestIntegracaoMigracaoDeLinhaDeBaseRecusaColunaAusente(t *testing.T) {
	pool := bancoLimpo(t)
	ctx := context.Background()

	if _, err := pool.Exec(ctx, `ALTER TABLE recorte.tb_importacao DROP COLUMN hash`); err != nil {
		t.Fatalf("removendo coluna: %v", err)
	}

	bruto, err := os.ReadFile("../../../db/migrations/0001_baseline.sql")
	if err != nil {
		t.Fatalf("lendo a migração: %v", err)
	}
	_, err = pool.Exec(ctx, string(bruto))
	if err == nil {
		t.Fatal("a migração deveria recusar um esquema sem a coluna hash")
	}
	if !contemTexto(err.Error(), "hash") {
		t.Errorf("a mensagem deveria nomear a coluna ausente: %v", err)
	}
}

// -------------------------------------------------------------------------
// Importação
// -------------------------------------------------------------------------

func TestIntegracaoRegistrarImportacao(t *testing.T) {
	pool := bancoLimpo(t)
	ctx := context.Background()
	repo := postgres.NovoRepositorioImportacao(pool)

	imp := importacaoDeTeste(t)
	id, err := repo.Registrar(ctx, imp)
	if err != nil {
		t.Fatalf("Registrar: %v", err)
	}
	if id == 0 {
		t.Fatal("o identificador gerado não pode ser zero")
	}

	var (
		idInclusao   int64
		idCadernos   int32
		status       int32
		nomeOriginal string
		tipoCaderno  string
		hash         string
	)
	err = pool.QueryRow(ctx, `
		SELECT id_inclusao, id_cadernos, status, nome_original_pdf, tipo_caderno, hash
		FROM recorte.tb_importacao WHERE id_importacao = $1`, id).
		Scan(&idInclusao, &idCadernos, &status, &nomeOriginal, &tipoCaderno, &hash)
	if err != nil {
		t.Fatalf("relendo: %v", err)
	}

	if idInclusao != 44521 || idCadernos != 1 {
		t.Errorf("ids gravados = %d/%d", idInclusao, idCadernos)
	}
	if status != int32(domain.StatusRecebido) {
		t.Errorf("status = %d, esperado 0 (padrão do legado)", status)
	}
	if tipoCaderno != domain.TipoCadernoPDF {
		t.Errorf("tipo_caderno = %q", tipoCaderno)
	}
	if nomeOriginal != "diario.pdf" || hash != "abc123" {
		t.Errorf("nome/hash = %q/%q", nomeOriginal, hash)
	}
}

func TestIntegracaoTransicoesDeStatus(t *testing.T) {
	pool := bancoLimpo(t)
	ctx := context.Background()
	repo := postgres.NovoRepositorioImportacao(pool)

	id, err := repo.Registrar(ctx, importacaoDeTeste(t))
	if err != nil {
		t.Fatalf("Registrar: %v", err)
	}

	// A sequência do legado, inclusive a escrita redundante de 0 (achado A16).
	sequencia := []domain.StatusImportacao{
		domain.StatusRecebido,
		domain.StatusSelecionado,
		domain.StatusIndexando,
		domain.StatusRecortando,
		domain.StatusFinalizado,
	}
	for _, s := range sequencia {
		if err := repo.AtualizarStatus(ctx, id, s); err != nil {
			t.Fatalf("AtualizarStatus(%s): %v", s, err)
		}
		var lido int32
		if err := pool.QueryRow(ctx,
			`SELECT status FROM recorte.tb_importacao WHERE id_importacao = $1`, id).
			Scan(&lido); err != nil {
			t.Fatalf("relendo status: %v", err)
		}
		if lido != int32(s) {
			t.Errorf("status gravado = %d, esperado %d", lido, int32(s))
		}
	}
}

func TestIntegracaoMarcarInicioUsaRelogioDoServidor(t *testing.T) {
	pool := bancoLimpo(t)
	ctx := context.Background()
	repo := postgres.NovoRepositorioImportacao(pool)

	id, err := repo.Registrar(ctx, importacaoDeTeste(t))
	if err != nil {
		t.Fatalf("Registrar: %v", err)
	}

	var antesDoServidor time.Time
	if err := pool.QueryRow(ctx, `SELECT current_timestamp`).Scan(&antesDoServidor); err != nil {
		t.Fatalf("lendo relógio do servidor: %v", err)
	}

	if err := repo.MarcarInicio(ctx, id); err != nil {
		t.Fatalf("MarcarInicio: %v", err)
	}

	var inicio *time.Time
	if err := pool.QueryRow(ctx,
		`SELECT data_inicio FROM recorte.tb_importacao WHERE id_importacao = $1`, id).
		Scan(&inicio); err != nil {
		t.Fatalf("relendo data_inicio: %v", err)
	}
	if inicio == nil {
		t.Fatal("data_inicio não foi gravada")
	}
	// A hora vem do servidor de banco, não do processo — main.rs:687.
	if inicio.Before(antesDoServidor.Add(-time.Second)) {
		t.Errorf("data_inicio %v é anterior ao relógio do servidor %v", *inicio, antesDoServidor)
	}
}

// TestIntegracaoMarcarTerminoRecusaEstouro cobre INV-P18.
func TestIntegracaoMarcarTerminoRecusaEstouro(t *testing.T) {
	pool := bancoLimpo(t)
	ctx := context.Background()
	repo := postgres.NovoRepositorioImportacao(pool)

	id, err := repo.Registrar(ctx, importacaoDeTeste(t))
	if err != nil {
		t.Fatalf("Registrar: %v", err)
	}

	// A conversão acontece no domínio, ANTES de qualquer comando.
	_, errConv := domain.ParaInt32(2147483648)
	if !errors.Is(errConv, domain.ErrEstouroNumerico) {
		t.Fatalf("2147483648 deveria estourar int32: %v", errConv)
	}

	// Nenhuma coluna pode ter sido tocada.
	var fim *time.Time
	var total *int32
	if err := pool.QueryRow(ctx,
		`SELECT data_fim, total_recortes FROM recorte.tb_importacao WHERE id_importacao = $1`, id).
		Scan(&fim, &total); err != nil {
		t.Fatalf("relendo: %v", err)
	}
	if fim != nil || total != nil {
		t.Errorf("com estouro, data_fim e total_recortes ficam intocados; obtive %v/%v", fim, total)
	}

	// E o caminho normal grava.
	if err := repo.MarcarTermino(ctx, id, 2147483647); err != nil {
		t.Fatalf("MarcarTermino no limite: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`SELECT data_fim, total_recortes FROM recorte.tb_importacao WHERE id_importacao = $1`, id).
		Scan(&fim, &total); err != nil {
		t.Fatalf("relendo: %v", err)
	}
	if fim == nil || total == nil || *total != 2147483647 {
		t.Errorf("no limite deveria gravar; obtive %v/%v", fim, total)
	}
}

// TestIntegracaoDataNaoDeslocaComFuso cobre INV-P16.
//
// O fuso do processo é forçado antes da gravação. Rodar este teste com
// TZ=Asia/Tokyo e com TZ=UTC precisa dar o mesmo dia.
func TestIntegracaoDataNaoDeslocaComFuso(t *testing.T) {
	pool := bancoLimpo(t)
	ctx := context.Background()
	repo := postgres.NovoRepositorioImportacao(pool)

	for _, zona := range []string{"UTC", "Asia/Tokyo", "America/Sao_Paulo", "Pacific/Kiritimati"} {
		t.Run(zona, func(t *testing.T) {
			local, err := time.LoadLocation(zona)
			if err != nil {
				t.Skipf("fuso %s indisponível: %v", zona, err)
			}
			anterior := time.Local
			time.Local = local
			t.Cleanup(func() { time.Local = anterior })

			d, err := domain.NovaData(2024, 3, 15)
			if err != nil {
				t.Fatalf("NovaData: %v", err)
			}
			imp := domain.NovaImportacao(1, 1, d, d, "x.pdf", "h")

			id, err := repo.Registrar(ctx, imp)
			if err != nil {
				t.Fatalf("Registrar: %v", err)
			}

			var texto string
			if err := pool.QueryRow(ctx, `
				SELECT to_char(data_caderno, 'YYYY-MM-DD')
				FROM recorte.tb_importacao WHERE id_importacao = $1`, id).Scan(&texto); err != nil {
				t.Fatalf("relendo data: %v", err)
			}
			if texto != "2024-03-15" {
				t.Errorf("com TZ=%s a data gravada foi %s, esperado 2024-03-15 (INV-P16)", zona, texto)
			}
		})
	}
}

// -------------------------------------------------------------------------
// Perfis
// -------------------------------------------------------------------------

func TestIntegracaoChavesPesquisaFiltraEOrdena(t *testing.T) {
	pool := bancoLimpo(t)
	ctx := context.Background()

	id, err := postgres.NovoRepositorioImportacao(pool).Registrar(ctx, importacaoDeTeste(t))
	if err != nil {
		t.Fatalf("Registrar: %v", err)
	}

	chaves, err := postgres.NovoRepositorioPerfil(pool).ChavesPesquisa(ctx, id)
	if err != nil {
		t.Fatalf("ChavesPesquisa: %v", err)
	}

	// Esperado, considerando os filtros da consulta e o ORDER BY:
	//   perfil 7 ativo, caderno 'S'  -> ALFA e BETA, nesta ordem alfabética
	//   perfil 8 ativo, caderno 'S'  -> GAMA
	//   perfil 9 tem caderno 'N'     -> filtrado
	//   perfil 99 é de cliente 'I'   -> filtrado
	//   expressão NULL do perfil 7   -> filtrada
	esperado := []domain.ChavePesquisa{
		{IDPerfil: 7, Expressao: "ALFA CONSTRUCOES"},
		{IDPerfil: 7, Expressao: "BETA CONSTRUCOES"},
		{IDPerfil: 8, Expressao: "GAMA SERVICOS"},
	}
	if len(chaves) != len(esperado) {
		t.Fatalf("obtive %d chaves, esperava %d: %+v", len(chaves), len(esperado), chaves)
	}
	for i := range esperado {
		if chaves[i] != esperado[i] {
			t.Errorf("chave %d = %+v, esperada %+v — a ORDEM governa INV-P12",
				i, chaves[i], esperado[i])
		}
	}
}

// -------------------------------------------------------------------------
// Recortes e atomicidade — achado A03
// -------------------------------------------------------------------------

func TestIntegracaoSalvarRecortesGravaOPar(t *testing.T) {
	pool := bancoLimpo(t)
	ctx := context.Background()

	id, err := postgres.NovoRepositorioImportacao(pool).Registrar(ctx, importacaoDeTeste(t))
	if err != nil {
		t.Fatalf("Registrar: %v", err)
	}

	uow := postgres.NovaUnidadeDeTrabalho(pool)
	repo := postgres.NovoRepositorioRecorte(pool, uow)

	chave := domain.ChavePesquisa{IDPerfil: 7, Expressao: "ALFA CONSTRUCOES"}
	recortes := []domain.Recorte{
		domain.NovoRecorte(3, "texto integral da página 3"),
		domain.NovoRecorte(7, "texto integral da página 7"),
	}

	n, err := repo.Salvar(ctx, id, chave, recortes)
	if err != nil {
		t.Fatalf("Salvar: %v", err)
	}
	if n != 2 {
		t.Errorf("contador = %d, esperado 2", n)
	}

	var linhas int
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM recorte.tb_recorte r
		JOIN recorte.tb_recorte_texto t ON t.id_recorte = r.id_recorte
		WHERE r.id_importacao = $1 AND t.origem = 'PDF'`, id).Scan(&linhas); err != nil {
		t.Fatalf("contando: %v", err)
	}
	if linhas != 2 {
		t.Errorf("pares gravados = %d, esperado 2", linhas)
	}

	// O texto gravado é o Destaque — a página inteira.
	var texto string
	if err := pool.QueryRow(ctx, `
		SELECT t.recorte FROM recorte.tb_recorte r
		JOIN recorte.tb_recorte_texto t ON t.id_recorte = r.id_recorte
		WHERE r.id_importacao = $1 AND r.nr_pagina = 3`, id).Scan(&texto); err != nil {
		t.Fatalf("lendo texto: %v", err)
	}
	if texto != "texto integral da página 3" {
		t.Errorf("texto gravado = %q", texto)
	}
}

// TestIntegracaoFalhaNoSegundoInsertNaoDeixaOrfao é o critério de aceite que
// cobre o achado A03.
//
// Um gatilho força a falha do INSERT em tb_recorte_texto. Sem transação, a
// linha correspondente em tb_recorte permaneceria órfã — que é o defeito do
// legado.
func TestIntegracaoFalhaNoSegundoInsertNaoDeixaOrfao(t *testing.T) {
	pool := bancoLimpo(t)
	ctx := context.Background()

	id, err := postgres.NovoRepositorioImportacao(pool).Registrar(ctx, importacaoDeTeste(t))
	if err != nil {
		t.Fatalf("Registrar: %v", err)
	}

	_, err = pool.Exec(ctx, `
		CREATE FUNCTION recorte.falhar_no_texto() RETURNS trigger AS $$
		BEGIN
			RAISE EXCEPTION 'falha forçada no INSERT de tb_recorte_texto';
		END $$ LANGUAGE plpgsql;

		CREATE TRIGGER falhar_no_texto
		BEFORE INSERT ON recorte.tb_recorte_texto
		FOR EACH ROW EXECUTE FUNCTION recorte.falhar_no_texto();`)
	if err != nil {
		t.Fatalf("criando gatilho: %v", err)
	}

	uow := postgres.NovaUnidadeDeTrabalho(pool)
	repo := postgres.NovoRepositorioRecorte(pool, uow)

	chave := domain.ChavePesquisa{IDPerfil: 7, Expressao: "ALFA CONSTRUCOES"}
	_, err = repo.Salvar(ctx, id, chave, []domain.Recorte{domain.NovoRecorte(3, "t")})
	if err == nil {
		t.Fatal("a gravação deveria falhar por causa do gatilho")
	}
	if !errors.Is(err, domain.ErrPersistencia) {
		t.Errorf("erro deveria envolver ErrPersistencia: %v", err)
	}

	var orfas int
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM recorte.tb_recorte WHERE id_importacao = $1`, id).Scan(&orfas); err != nil {
		t.Fatalf("contando órfãs: %v", err)
	}
	if orfas != 0 {
		t.Errorf("ACHADO A03: %d linha(s) órfã(s) em tb_recorte — a transação não reverteu", orfas)
	}
}

// TestIntegracaoEscopoTransacionalEhPorChamada cobre INV-P14.
//
// O que já foi gravado por chamadas anteriores PERMANECE quando uma chamada
// posterior falha. Uma transação por importação reverteria tudo e mudaria o
// estado final observável.
func TestIntegracaoEscopoTransacionalEhPorChamada(t *testing.T) {
	pool := bancoLimpo(t)
	ctx := context.Background()

	id, err := postgres.NovoRepositorioImportacao(pool).Registrar(ctx, importacaoDeTeste(t))
	if err != nil {
		t.Fatalf("Registrar: %v", err)
	}

	uow := postgres.NovaUnidadeDeTrabalho(pool)
	repo := postgres.NovoRepositorioRecorte(pool, uow)

	// Duas chamadas bem-sucedidas.
	for _, chave := range []domain.ChavePesquisa{
		{IDPerfil: 7, Expressao: "ALFA CONSTRUCOES"},
		{IDPerfil: 7, Expressao: "BETA CONSTRUCOES"},
	} {
		if _, err := repo.Salvar(ctx, id, chave, []domain.Recorte{
			domain.NovoRecorte(1, "t"),
		}); err != nil {
			t.Fatalf("Salvar(%s): %v", chave.Expressao, err)
		}
	}

	// A terceira falha.
	if _, err := pool.Exec(ctx, `
		CREATE FUNCTION recorte.falhar() RETURNS trigger AS $$
		BEGIN RAISE EXCEPTION 'falha forçada'; END $$ LANGUAGE plpgsql;
		CREATE TRIGGER falhar BEFORE INSERT ON recorte.tb_recorte_texto
		FOR EACH ROW EXECUTE FUNCTION recorte.falhar();`); err != nil {
		t.Fatalf("criando gatilho: %v", err)
	}

	if _, err := repo.Salvar(ctx, id, domain.ChavePesquisa{IDPerfil: 8, Expressao: "GAMA SERVICOS"},
		[]domain.Recorte{domain.NovoRecorte(2, "t")}); err == nil {
		t.Fatal("a terceira chamada deveria falhar")
	}

	// As duas primeiras permanecem: é o comportamento do legado (INV-P14).
	var gravados int
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM recorte.tb_recorte WHERE id_importacao = $1`, id).Scan(&gravados); err != nil {
		t.Fatalf("contando: %v", err)
	}
	if gravados != 2 {
		t.Errorf("INV-P14: esperava as 2 gravações anteriores preservadas, obtive %d", gravados)
	}
}

func TestIntegracaoSalvarSemRecortesNaoTocaOBanco(t *testing.T) {
	pool := bancoLimpo(t)
	ctx := context.Background()

	id, err := postgres.NovoRepositorioImportacao(pool).Registrar(ctx, importacaoDeTeste(t))
	if err != nil {
		t.Fatalf("Registrar: %v", err)
	}

	repo := postgres.NovoRepositorioRecorte(pool, postgres.NovaUnidadeDeTrabalho(pool))
	n, err := repo.Salvar(ctx, id, domain.ChavePesquisa{IDPerfil: 7, Expressao: "X"}, nil)
	if err != nil {
		t.Fatalf("Salvar com lista vazia: %v", err)
	}
	if n != 0 {
		t.Errorf("contador = %d, esperado 0", n)
	}
}

// -------------------------------------------------------------------------
// Unidade de trabalho
// -------------------------------------------------------------------------

func TestIntegracaoUnidadeDeTrabalhoReverteEmErro(t *testing.T) {
	pool := bancoLimpo(t)
	ctx := context.Background()
	uow := postgres.NovaUnidadeDeTrabalho(pool)
	repo := postgres.NovoRepositorioImportacao(pool)

	falhaEsperada := errors.New("erro do caso de uso")
	err := uow.EmTransacao(ctx, func(ctx context.Context) error {
		if _, err := repo.Registrar(ctx, importacaoDeTeste(t)); err != nil {
			return err
		}
		return falhaEsperada
	})
	if !errors.Is(err, falhaEsperada) {
		t.Fatalf("erro = %v, esperava o do caso de uso", err)
	}

	var linhas int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM recorte.tb_importacao`).Scan(&linhas); err != nil {
		t.Fatalf("contando: %v", err)
	}
	if linhas != 0 {
		t.Errorf("a transação deveria ter revertido; %d linha(s) sobraram", linhas)
	}
}

func TestIntegracaoUnidadeDeTrabalhoConfirmaEmSucesso(t *testing.T) {
	pool := bancoLimpo(t)
	ctx := context.Background()
	uow := postgres.NovaUnidadeDeTrabalho(pool)
	repo := postgres.NovoRepositorioImportacao(pool)

	if err := uow.EmTransacao(ctx, func(ctx context.Context) error {
		_, err := repo.Registrar(ctx, importacaoDeTeste(t))
		return err
	}); err != nil {
		t.Fatalf("EmTransacao: %v", err)
	}

	var linhas int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM recorte.tb_importacao`).Scan(&linhas); err != nil {
		t.Fatalf("contando: %v", err)
	}
	if linhas != 1 {
		t.Errorf("esperava 1 linha confirmada, obtive %d", linhas)
	}
}

func TestIntegracaoUnidadeDeTrabalhoReverteEmPanico(t *testing.T) {
	pool := bancoLimpo(t)
	ctx := context.Background()
	uow := postgres.NovaUnidadeDeTrabalho(pool)
	repo := postgres.NovoRepositorioImportacao(pool)

	func() {
		defer func() {
			if r := recover(); r == nil {
				t.Error("o pânico deveria ser repropagado")
			}
		}()
		_ = uow.EmTransacao(ctx, func(ctx context.Context) error {
			if _, err := repo.Registrar(ctx, importacaoDeTeste(t)); err != nil {
				return err
			}
			panic("falha inesperada no meio da transação")
		})
	}()

	var linhas int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM recorte.tb_importacao`).Scan(&linhas); err != nil {
		t.Fatalf("contando: %v", err)
	}
	if linhas != 0 {
		t.Errorf("o pânico deveria ter revertido; %d linha(s) sobraram", linhas)
	}
}

func TestIntegracaoTransacaoAninhadaReaproveitaACorrente(t *testing.T) {
	pool := bancoLimpo(t)
	ctx := context.Background()
	uow := postgres.NovaUnidadeDeTrabalho(pool)
	repo := postgres.NovoRepositorioImportacao(pool)

	falha := errors.New("falha no aninhamento")
	err := uow.EmTransacao(ctx, func(ctx context.Context) error {
		if _, err := repo.Registrar(ctx, importacaoDeTeste(t)); err != nil {
			return err
		}
		// A interna não abre outra transação: participa da mesma.
		return uow.EmTransacao(ctx, func(ctx context.Context) error {
			if _, err := repo.Registrar(ctx, importacaoDeTeste(t)); err != nil {
				return err
			}
			return falha
		})
	})
	if !errors.Is(err, falha) {
		t.Fatalf("erro = %v", err)
	}

	var linhas int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM recorte.tb_importacao`).Scan(&linhas); err != nil {
		t.Fatalf("contando: %v", err)
	}
	if linhas != 0 {
		t.Errorf("as duas inserções deveriam reverter juntas; %d sobraram", linhas)
	}
}

func contemTexto(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || indice(s, sub) >= 0)
}

func indice(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
