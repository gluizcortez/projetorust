//go:build integration

package e2e_test

import (
	"bytes"
	"context"
	"fmt"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Testes de CAOS — fase F12.
//
// # A pergunta que eles respondem
//
// "O que fica no banco quando o processo morre no meio de uma importação?" A
// resposta precisa ser a MESMA que o legado deixaria, porque é ela que o
// operador vai encontrar na segunda-feira.
//
// # O que o legado deixa
//
// Um `SIGKILL` não deixa o legado fazer nada: não há tratamento de sinal, não
// há gravação de status final, não há transação a reverter no nível da
// importação. A linha fica **no último status gravado** e permanece assim para
// sempre — nada a reexamina (docs/ESPECIFICACAO.md §3.5, achado A15).
//
// Portanto o critério é: depois do `SIGKILL`, a importação está em um dos
// status NÃO terminais 0, 1, 2 ou 3, e o que já foi gravado PERMANECE
// (INV-P14). O que NÃO pode acontecer:
//
//   - a importação aparecer em 5 sem ter terminado;
//   - a importação aparecer em -1, que é o desfecho de erro TRATADO e diria ao
//     operador que o serviço processou e falhou, quando na verdade ele morreu;
//   - recortes parciais desaparecerem.
//
// # Por que SIGKILL e não SIGTERM
//
// `SIGTERM` tem encerramento ordenado e já é medido em `ciclo_de_vida_test.go`.
// `SIGKILL` é o caso que nenhum código trata — queda de nó, esgotamento de
// memória, contêiner reciclado — e é o único que mede o que sobra sem
// cooperação do processo.

// -------------------------------------------------------------------------
// Instrumental
// -------------------------------------------------------------------------

// esquemaDeCaos prepara um esquema mínimo com um perfil que casa o documento.
//
// Não reaproveita o `testdata` do pacote de persistência porque este teste roda
// o BINÁRIO, e o binário precisa de um banco já semeado antes de subir.
func esquemaDeCaos(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	ctx := context.Background()

	for _, arquivo := range []string{
		"../../internal/adapter/postgres/testdata/esquema_inferido.sql",
		"../../internal/adapter/postgres/testdata/dados_minimos.sql",
	} {
		bruto, err := os.ReadFile(arquivo)
		if err != nil {
			t.Fatalf("lendo %s: %v", arquivo, err)
		}
		if _, err := pool.Exec(ctx, string(bruto)); err != nil {
			t.Fatalf("aplicando %s: %v", arquivo, err)
		}
	}
}

func poolDeCaos(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := exigirBanco(t)

	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("abrindo pool: %v", err)
	}
	t.Cleanup(pool.Close)

	if _, err := pool.Exec(context.Background(), `DROP SCHEMA IF EXISTS recorte CASCADE`); err != nil {
		t.Fatalf("limpando esquema: %v", err)
	}
	esquemaDeCaos(t, pool)
	return pool
}

// submeterDocumento envia um PDF do corpus e devolve o código de resposta.
func submeterDocumento(t *testing.T, p *processo, documento string) int {
	t.Helper()

	conteudo, err := os.ReadFile(filepath.Join("../testdata/corpus", documento))
	if err != nil {
		t.Skipf("corpus indisponível (%v) — gere com `make corpus`", err)
	}

	var corpo bytes.Buffer
	escritor := multipart.NewWriter(&corpo)
	campos := map[string]string{
		"data-caderno":          "2024-03-15",
		"data-disponibilizacao": "2024-03-16",
		"id-usuario":            "44521",
		"id-caderno":            "1",
	}
	for nome, valor := range campos {
		if err := escritor.WriteField(nome, valor); err != nil {
			t.Fatalf("montando o campo %s: %v", nome, err)
		}
	}
	parte, err := escritor.CreateFormFile("pdf", documento)
	if err != nil {
		t.Fatalf("montando a parte do arquivo: %v", err)
	}
	if _, err := parte.Write(conteudo); err != nil {
		t.Fatalf("escrevendo o arquivo: %v", err)
	}
	if err := escritor.Close(); err != nil {
		t.Fatalf("fechando o multipart: %v", err)
	}

	req, err := http.NewRequest(http.MethodPost, "http://"+p.endereco+"/pdf", &corpo)
	if err != nil {
		t.Fatalf("montando a requisição: %v", err)
	}
	req.Header.Set("Content-Type", escritor.FormDataContentType())
	req.Header.Set("X-API-KEY", chaveDeTeste)

	resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		t.Fatalf("POST /pdf: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	return resp.StatusCode
}

// estadoDaImportacao lê o que sobrou no banco.
type estadoDaImportacao struct {
	ID            int64
	Status        int32
	TemInicio     bool
	TemFim        bool
	TotalRecortes *int32
	Recortes      int
	Textos        int
}

func lerEstado(t *testing.T, pool *pgxpool.Pool) (estadoDaImportacao, bool) {
	t.Helper()
	ctx := context.Background()

	var e estadoDaImportacao
	err := pool.QueryRow(ctx, `
		SELECT id_importacao, status,
		       data_inicio IS NOT NULL, data_fim IS NOT NULL, total_recortes
		FROM recorte.tb_importacao
		ORDER BY id_importacao DESC LIMIT 1`).
		Scan(&e.ID, &e.Status, &e.TemInicio, &e.TemFim, &e.TotalRecortes)
	if err != nil {
		return estadoDaImportacao{}, false
	}

	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM recorte.tb_recorte WHERE id_importacao = $1`, e.ID).
		Scan(&e.Recortes); err != nil {
		t.Fatalf("contando recortes: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM recorte.tb_recorte_texto t
		JOIN recorte.tb_recorte r ON r.id_recorte = t.id_recorte
		WHERE r.id_importacao = $1`, e.ID).Scan(&e.Textos); err != nil {
		t.Fatalf("contando textos: %v", err)
	}
	return e, true
}

// esperarStatus aguarda a importação chegar a um dos status pedidos.
//
// Devolve o estado observado e se chegou. Sondar é a única forma: o estágio é
// interno ao processo e o teste só o enxerga pelo banco.
func esperarStatus(t *testing.T, pool *pgxpool.Pool, alvos ...int32) (estadoDaImportacao, bool) {
	t.Helper()
	limite := time.After(30 * time.Second)

	for {
		select {
		case <-limite:
			e, _ := lerEstado(t, pool)
			return e, false
		default:
		}
		if e, achou := lerEstado(t, pool); achou {
			for _, alvo := range alvos {
				if e.Status == alvo {
					return e, true
				}
			}
		}
		time.Sleep(2 * time.Millisecond)
	}
}

// -------------------------------------------------------------------------
// SIGKILL por estágio
// -------------------------------------------------------------------------

// TestCaosSIGKILLPorEstagio mata o processo em cada estágio da importação e
// confere que o banco fica como o legado deixaria.
//
// O documento é o de 120 páginas: ele demora o bastante para que a sonda pegue
// os estágios intermediários antes de a importação terminar.
func TestCaosSIGKILLPorEstagio(t *testing.T) {
	casos := []struct {
		nome  string
		alvos []int32
	}{
		{"durante a indexação (status 2)", []int32{2}},
		{"durante o recorte (status 3)", []int32{3}},
		{"logo após a seleção (status 1 ou 2)", []int32{1, 2}},
	}

	for _, caso := range casos {
		t.Run(caso.nome, func(t *testing.T) {
			pool := poolDeCaos(t)
			p := iniciar(t)

			if codigo := submeterDocumento(t, p, "18-volume-120-paginas.pdf"); codigo != http.StatusOK {
				t.Fatalf("POST /pdf devolveu %d", codigo)
			}

			estado, chegou := esperarStatus(t, pool, caso.alvos...)
			if !chegou {
				t.Skipf("a importação não passou por %v antes de terminar (status %d) — "+
					"o documento processa rápido demais neste ambiente", caso.alvos, estado.Status)
			}

			// SIGKILL: o processo não tem chance de gravar nada.
			if err := p.cmd.Process.Signal(syscall.SIGKILL); err != nil {
				t.Fatalf("enviando SIGKILL: %v", err)
			}
			_ = p.cmd.Wait()

			depois, achou := lerEstado(t, pool)
			if !achou {
				t.Fatal("a importação sumiu do banco")
			}
			conferirEstadoAposMorteSubita(t, depois)
		})
	}
}

// conferirEstadoAposMorteSubita aplica o critério de equivalência com o legado.
func conferirEstadoAposMorteSubita(t *testing.T, e estadoDaImportacao) {
	t.Helper()

	switch e.Status {
	case 0, 1, 2, 3:
		// É o desfecho do legado: a importação fica PRESA no último status
		// gravado. Nada a reexamina — é o achado A15, e é por isso que a chave
		// VARREDURA_ORFAS existe.
		t.Logf("importação %d ficou presa em status %d, com %d recorte(s) — "+
			"é o que o legado deixaria", e.ID, e.Status, e.Recortes)
	case 5:
		t.Errorf("importação %d está em 5 (finalizada) depois de SIGKILL: "+
			"o processo não teve como concluir", e.ID)
	case -1:
		t.Errorf("importação %d está em -1 depois de SIGKILL: o legado NÃO grava "+
			"desfecho de erro numa morte súbita, e -1 diria ao operador que o "+
			"serviço processou e falhou", e.ID)
	default:
		t.Errorf("importação %d está em status inesperado %d", e.ID, e.Status)
	}

	// INV-P14: o trabalho parcial PERMANECE. E o par recorte/texto é ATÔMICO
	// (achado A03): uma linha em tb_recorte sem o texto correspondente é
	// exatamente o defeito que a migração corrigiu.
	if e.Recortes != e.Textos {
		t.Errorf("%d recorte(s) e %d texto(s): o par tem de ser atômico (achado A03)",
			e.Recortes, e.Textos)
	}

	// `data_fim` só é gravada junto com o status 5.
	if e.TemFim && e.Status != 5 {
		t.Errorf("data_fim gravada com status %d", e.Status)
	}
}

// -------------------------------------------------------------------------
// Queda do banco durante o processamento
// -------------------------------------------------------------------------

// TestCaosBancoIndisponivelDuranteOProcessamento verifica que o processo
// SOBREVIVE a uma falha de banco no meio de uma importação.
//
// # O que se mede, e o que NÃO se mede
//
// Mede-se que o processo continua vivo e servindo `/ping`. É o critério que
// importa: no legado, todas as gravações de status usam `let _ = ...` e a falha
// é ignorada (ESPECIFICACAO §3.6), então uma indisponibilidade momentânea do
// banco não derruba o serviço nem interrompe o laço.
//
// NÃO se mede que a importação termine em -1. O enunciado da fase pede isso,
// mas ele descreve um caso diferente: a falha do banco que o legado converte em
// -1 é a que acontece na LEITURA das chaves de pesquisa (main.rs:327-331), não
// nas gravações de status — essas são descartadas em silêncio. Derrubar as
// conexões no meio do processamento atinge as duas, e qual delas falha primeiro
// depende do instante exato. Afirmar "termina em -1" seria afirmar mais do que
// o teste consegue observar.
func TestCaosBancoIndisponivelDuranteOProcessamento(t *testing.T) {
	pool := poolDeCaos(t)
	p := iniciar(t)

	if codigo := submeterDocumento(t, p, "18-volume-120-paginas.pdf"); codigo != http.StatusOK {
		t.Fatalf("POST /pdf devolveu %d", codigo)
	}

	if _, chegou := esperarStatus(t, pool, 1, 2, 3); !chegou {
		t.Skip("a importação terminou antes de o teste poder derrubar o banco")
	}

	// Derruba TODAS as conexões do serviço, sem derrubar o servidor de banco:
	// é o efeito de um reinício do PostgreSQL ou de uma queda de rede.
	derrubarConexoes(t, pool)

	// O processo tem de continuar vivo e respondendo.
	prazo := time.After(20 * time.Second)
	for {
		select {
		case <-prazo:
			t.Fatalf("o serviço parou de responder depois da queda do banco\n%s", p.saida.String())
		default:
		}
		resp, err := (&http.Client{Timeout: time.Second}).Get("http://" + p.endereco + "/ping")
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				break
			}
		}
		time.Sleep(50 * time.Millisecond)
	}

	if p.cmd.ProcessState != nil {
		t.Fatalf("o processo morreu com a queda do banco\n%s", p.saida.String())
	}

	// E o estado que sobrou continua sendo um estado válido.
	if e, achou := lerEstado(t, pool); achou {
		t.Logf("depois da queda: importação %d em status %d, %d recorte(s)",
			e.ID, e.Status, e.Recortes)
		if e.Recortes != e.Textos {
			t.Errorf("%d recorte(s) e %d texto(s): o par tem de ser atômico mesmo "+
				"com o banco caindo no meio (achado A03)", e.Recortes, e.Textos)
		}
	}
}

// derrubarConexoes encerra as sessões do serviço no PostgreSQL.
func derrubarConexoes(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	_, err := pool.Exec(context.Background(), `
		SELECT pg_terminate_backend(pid)
		FROM pg_stat_activity
		WHERE datname = current_database()
		  AND pid <> pg_backend_pid()
		  AND application_name IS DISTINCT FROM 'teste-de-caos'`)
	if err != nil {
		t.Fatalf("derrubando conexões: %v", err)
	}
}

// -------------------------------------------------------------------------
// Morte durante o encerramento
// -------------------------------------------------------------------------

// TestCaosSIGKILLDuranteADrenagem: o segundo sinal já é medido em
// `ciclo_de_vida_test.go`, mas o SIGKILL durante a drenagem é diferente — não
// há código nenhum rodando depois dele.
func TestCaosSIGKILLDuranteADrenagem(t *testing.T) {
	pool := poolDeCaos(t)
	p := iniciar(t)

	if codigo := submeterDocumento(t, p, "18-volume-120-paginas.pdf"); codigo != http.StatusOK {
		t.Fatalf("POST /pdf devolveu %d", codigo)
	}
	if _, chegou := esperarStatus(t, pool, 1, 2, 3); !chegou {
		t.Skip("a importação terminou antes de o encerramento começar")
	}

	// SIGTERM inicia a drenagem; SIGKILL a interrompe no meio.
	p.sinalizar(syscall.SIGTERM)
	time.Sleep(50 * time.Millisecond)

	// A leitura ANTES do SIGKILL é o que torna o teste honesto: se a drenagem
	// já concluiu a importação nesses milissegundos, o status 5 é o desfecho
	// CORRETO e não há morte súbita a medir. Sem esta guarda, o teste acusaria
	// um defeito onde houve apenas uma corrida ganha pelo processo.
	antes, achouAntes := lerEstado(t, pool)
	if achouAntes && (antes.Status == 5 || antes.Status == -1) {
		t.Skipf("a drenagem concluiu a importação (status %d) antes do SIGKILL", antes.Status)
	}

	if err := p.cmd.Process.Signal(syscall.SIGKILL); err != nil {
		t.Fatalf("enviando SIGKILL: %v", err)
	}
	_ = p.cmd.Wait()

	estado, achou := lerEstado(t, pool)
	if !achou {
		t.Fatal("a importação sumiu do banco")
	}
	conferirEstadoAposMorteSubita(t, estado)
}

// -------------------------------------------------------------------------
// Relato
// -------------------------------------------------------------------------

// TestCaosRelatarAmbiente imprime o que o operador precisa saber para
// interpretar os resultados acima.
func TestCaosRelatarAmbiente(t *testing.T) {
	pool := poolDeCaos(t)

	var versao string
	if err := pool.QueryRow(context.Background(), `SELECT version()`).Scan(&versao); err != nil {
		t.Fatalf("consultando a versão: %v", err)
	}
	fmt.Printf("caos: %s\n", versao) //nolint:forbidigo // relato do teste
}
