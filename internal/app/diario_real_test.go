//go:build integration

package app_test

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"os"
	"sort"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/gluizcortez/projetorust/internal/app"
	"github.com/gluizcortez/projetorust/internal/platform/observability"
)

// -------------------------------------------------------------------------
// O serviço sobre um diário REAL
// -------------------------------------------------------------------------
//
// Todo o resto da suíte roda sobre documentos que nós mesmos escrevemos —
// inclusive o corpus dourado da F0, que a F12 apontou como a maior ressalva do
// relatório de paridade: 28 documentos SINTÉTICOS não são base para cortar.
//
// Este teste não resolve aquela ressalva, e não é o que ele se propõe a ser.
// Ele responde a uma pergunta menor e ainda assim não respondida até aqui: **o
// serviço processa um Diário Oficial de verdade?** Documento real tem o que
// nenhum documento nosso teria — texto posicionado com espaço entre as letras,
// abreviação, número de processo colado, caixa alta acentuada.
//
// `exemplos/dou-secao1-2026-07-08.pdf` é o DOU Seção 1, nº 126, de 8 de julho
// de 2026, página 177 — deliberações do MPT. Uma página, 17.307 caracteres
// depois da normalização.
//
// `db/init/03-perfis-do-dou-real.sql` semeia oito perfis casados com ele, UM
// POR EXPRESSÃO. Uma por perfil é essencial: dentro de um mesmo perfil, a
// primeira expressão a encontrar uma página consome aquela página (INV-P12), e
// as demais não gerariam recorte — o resultado seria correto e ilegível.
//
// A tabela abaixo é a mesma do README. Aqui ela deixa de ser afirmação e passa
// a ser asserção.

// expressaoDoDOU é uma linha da matriz de validação.
type expressaoDoDOU struct {
	perfil    int64
	expressao string
	casa      bool
	porque    string
}

// matrizDoDOU é o resultado MEDIDO contra o documento real.
//
// As três linhas que NÃO casam valem mais que as cinco que casam: duas delas
// são propriedades do serviço que só um documento real expõe.
var matrizDoDOU = []expressaoDoDOU{
	{301, "BR BPO TECNOLOGIA E SERVICOS", true, "frase de 4 termos"},
	{302, "CASAMAX COMERCIAL E SERVICOS LTDA", true, "frase de 5 termos"},

	// No PDF: "Lei Geral de Proteção de Dados" — com acento e em caixa mista.
	// Casar exige que a normalização e o rebaixamento de caixa estejam no
	// caminho de PRODUÇÃO, não só nos testes de unidade. Foi exatamente esse o
	// defeito que a F12 encontrou (o extrator cru na raiz de composição), e
	// esta linha é a guarda dele sobre entrada real.
	{303, "LEI GERAL DE PROTECAO DE DADOS", true, "acento e caixa mista no documento"},

	{304, "DEBORAH DA SILVA FELIX", true, "no PDF, `Dra. Deborah da Silva Felix`"},
	{305, "HOMOLOGACOES DE ARQUIVAMENTO", true, "caixa alta acentuada no documento"},

	// A extração devolve "EMPRESA BRASILEIRA DE CORREIOS E TELEG R A FO S":
	// o texto foi posicionado com espaço entre as letras no documento
	// original. Vira cinco termos onde a expressão espera um. O serviço está
	// certo — nenhuma busca por frase casaria, e o legado também não casa.
	{306, "EMPRESA BRASILEIRA DE CORREIOS E TELEGRAFOS", false,
		"o PDF quebra a palavra em `TELEG R A FO S`"},

	// A MESMA expressão do perfil 303, com acento. Expressão acentuada é
	// INERTE: o texto indexado perdeu os acentos e a expressão cadastrada não
	// passa pela mesma normalização. DEFEITO PRESERVADO — INV-P19.
	//
	// O par 303/307 é a demonstração viva, e é por isso que as duas linhas
	// existem: sozinha, cada uma seria só um resultado; juntas, são a prova de
	// que a diferença é o acento e nada mais.
	{307, "LEI GERAL DE PROTEÇÃO DE DADOS", false, "INV-P19: expressão acentuada é inerte"},

	{308, "PREFEITURA MUNICIPAL DE SAO PAULO", false, "controle negativo"},
}

// TestIntegracaoDiarioRealProduzOsRecortesMedidos submete o DOU real pelo HTTP
// de produção e confere a matriz linha a linha.
//
// Roda com as chaves NO PADRÃO — é o serviço como o legado, sem evolução
// alguma ligada.
func TestIntegracaoDiarioRealProduzOsRecortesMedidos(t *testing.T) {
	cfg := configComBanco(t)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	ctx, cancelarMontagem := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancelarMontagem()

	servico, err := app.Novo(ctx, cfg, logger, nil, observability.NovasMetricas(),
		"integracao", "abc123", func() string { return "teste" })
	if err != nil {
		t.Fatalf("app.Novo: %v", err)
	}

	execucao, sinal := context.WithCancel(context.Background())
	resultado := make(chan error, 1)
	go func() { resultado <- servico.Executar(execucao, nil) }()
	defer func() {
		sinal()
		select {
		case err := <-resultado:
			if err != nil {
				t.Errorf("Executar: %v", err)
			}
		case <-time.After(30 * time.Second):
			t.Error("Executar não retornou em 30s")
		}
	}()

	endereco := esperarPorta(t, servico)

	corpo, tipo := submissaoDoDOU(t)
	req, err := http.NewRequest(http.MethodPost, "http://"+endereco+"/pdf", bytes.NewReader(corpo))
	if err != nil {
		t.Fatalf("montando a requisição: %v", err)
	}
	req.Header.Set("Content-Type", tipo)
	req.Header.Set("X-API-KEY", cfg.APIKey.Revelar())

	resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		t.Fatalf("POST /pdf: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	lido, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST /pdf devolveu %d: %s", resp.StatusCode, lido)
	}
	if string(lido) != "PDF carregado com sucesso" {
		t.Errorf("corpo = %q; o literal do legado é outro", lido)
	}

	// O 200 confirma o REGISTRO, não o resultado: o processamento é assíncrono.
	pool := abrirPool(t, os.Getenv("TEST_DATABASE_URL"))
	idImportacao := esperarImportacaoFinalizada(t, pool)

	t.Run("a matriz de expressões", func(t *testing.T) {
		conferirMatrizDoDOU(t, pool, idImportacao)
	})

	t.Run("o texto gravado é a página inteira, normalizada", func(t *testing.T) {
		var texto string
		err := pool.QueryRow(context.Background(),
			`SELECT t.recorte FROM recorte.tb_recorte_texto t
			   JOIN recorte.tb_recorte r USING (id_recorte)
			  WHERE r.id_importacao = $1 LIMIT 1`, idImportacao).Scan(&texto)
		if err != nil {
			t.Fatalf("lendo o texto: %v", err)
		}

		// INV: o valor gravado é a página INTEIRA, não o trecho que casou.
		if len(texto) < 17_000 {
			t.Errorf("texto tem %d bytes; a página tem ~17.300 — deveria ser a página inteira", len(texto))
		}
		// A normalização passou: a forma acentuada não sobrevive.
		for _, acentuada := range []string{"Proteção", "HOMOLOGAÇÕES", "endereço"} {
			if bytes.Contains([]byte(texto), []byte(acentuada)) {
				t.Errorf("o texto gravado ainda tem %q — a normalização não foi aplicada", acentuada)
			}
		}
		if !bytes.Contains([]byte(texto), []byte("Lei Geral de Protecao de Dados")) {
			t.Error("o texto gravado não tem a forma normalizada de `Lei Geral de Proteção de Dados`")
		}
	})
}

// conferirMatrizDoDOU compara os recortes gravados com matrizDoDOU.
//
// Confere as duas direções: toda expressão que deve casar tem recorte, e
// NENHUMA das que não devem tem. A segunda metade é a que pega regressão de
// normalização — corrigir INV-P19 por engano faria o perfil 307 aparecer.
func conferirMatrizDoDOU(t *testing.T, pool *pgxpool.Pool, idImportacao int64) {
	t.Helper()

	linhas, err := pool.Query(context.Background(),
		`SELECT id_perfil, expressao_busca, nr_pagina
		   FROM recorte.tb_recorte WHERE id_importacao = $1`, idImportacao)
	if err != nil {
		t.Fatalf("lendo os recortes: %v", err)
	}
	defer linhas.Close()

	type recorte struct {
		expressao string
		pagina    int32
	}
	gravados := map[int64]recorte{}
	for linhas.Next() {
		var perfil int64
		var r recorte
		if err := linhas.Scan(&perfil, &r.expressao, &r.pagina); err != nil {
			t.Fatalf("lendo linha: %v", err)
		}
		if anterior, repetido := gravados[perfil]; repetido {
			t.Errorf("perfil %d gerou DOIS recortes (%q e %q) para uma página só — "+
				"a deduplicação por perfil (INV-P12) falhou",
				perfil, anterior.expressao, r.expressao)
		}
		gravados[perfil] = r
	}
	if err := linhas.Err(); err != nil {
		t.Fatalf("percorrendo os recortes: %v", err)
	}

	esperados := map[int64]bool{}
	for _, e := range matrizDoDOU {
		esperados[e.perfil] = true

		r, achou := gravados[e.perfil]
		switch {
		case e.casa && !achou:
			t.Errorf("perfil %d %q NÃO gerou recorte; deveria casar (%s)",
				e.perfil, e.expressao, e.porque)
		case !e.casa && achou:
			t.Errorf("perfil %d %q gerou recorte na página %d; NÃO deveria casar (%s)",
				e.perfil, e.expressao, r.pagina, e.porque)
		case e.casa && achou:
			if r.expressao != e.expressao {
				t.Errorf("perfil %d gravou expressao_busca = %q; esperava %q",
					e.perfil, r.expressao, e.expressao)
			}
			if r.pagina != 1 {
				t.Errorf("perfil %d casou na página %d; o documento tem UMA página",
					e.perfil, r.pagina)
			}
		}
	}

	// Perfil de fora da matriz que tenha gerado recorte é sinal de que os
	// perfis sintéticos de `02-dados-de-exemplo.sql` passaram a casar com o
	// documento real — o que seria regressão de busca, não acerto.
	var intrusos []int64
	for perfil := range gravados {
		if !esperados[perfil] {
			intrusos = append(intrusos, perfil)
		}
	}
	sort.Slice(intrusos, func(i, j int) bool { return intrusos[i] < intrusos[j] })
	for _, perfil := range intrusos {
		t.Errorf("perfil %d gerou recorte (%q) e não está na matriz — "+
			"expressão que não deveria casar com este documento",
			perfil, gravados[perfil].expressao)
	}
}

// submissaoDoDOU monta o multipart com o PDF real do repositório.
func submissaoDoDOU(t *testing.T) ([]byte, string) {
	t.Helper()

	const caminho = "../../exemplos/dou-secao1-2026-07-08.pdf"
	conteudo, err := os.ReadFile(caminho)
	if err != nil {
		t.Fatalf("lendo %s: %v", caminho, err)
	}

	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	campos := [][2]string{
		{"data-caderno", "2026-07-08"},
		{"data-disponibilizacao", "2026-07-08"},
		{"id-usuario", "44521"},
		// O caderno 1 é o que `03-perfis-do-dou-real.sql` habilita.
		{"id-caderno", "1"},
	}
	for _, c := range campos {
		if err := w.WriteField(c[0], c[1]); err != nil {
			t.Fatalf("campo %s: %v", c[0], err)
		}
	}
	parte, err := w.CreateFormFile("pdf", "dou-secao1-2026-07-08.pdf")
	if err != nil {
		t.Fatalf("parte do arquivo: %v", err)
	}
	if _, err := parte.Write(conteudo); err != nil {
		t.Fatalf("escrevendo o PDF: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("fechando o multipart: %v", err)
	}
	return buf.Bytes(), w.FormDataContentType()
}

func abrirPool(t *testing.T, dsn string) *pgxpool.Pool {
	t.Helper()
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("abrindo pool de conferência: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// esperarImportacaoFinalizada espera o status 5 e devolve o identificador.
//
// Sonda em vez de dormir um tempo fixo: a extração leva dezenas de
// milissegundos nesta máquina e pode levar mais em outra. O prazo é generoso
// porque o custo de um teste instável é maior que o de esperar.
func esperarImportacaoFinalizada(t *testing.T, pool *pgxpool.Pool) int64 {
	t.Helper()

	limite := time.After(60 * time.Second)
	for {
		var id int64
		var status int32
		err := pool.QueryRow(context.Background(),
			`SELECT id_importacao, status FROM recorte.tb_importacao
			  ORDER BY id_importacao DESC LIMIT 1`).Scan(&id, &status)
		if err == nil {
			switch status {
			case 5:
				return id
			case -1:
				t.Fatalf("importação %d terminou em ERRO (status -1)", id)
			}
		}

		select {
		case <-limite:
			t.Fatal("a importação não terminou em 60s")
		case <-time.After(50 * time.Millisecond):
		}
	}
}
