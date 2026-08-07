package parity_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gluizcortez/projetorust/internal/adapter/pdftext"
	"github.com/gluizcortez/projetorust/internal/adapter/searchidx"
	"github.com/gluizcortez/projetorust/internal/domain"
	"github.com/gluizcortez/projetorust/internal/usecase"
)

// Camadas 4 e 5 da verificação de paridade — o PIPELINE INTEIRO.
//
// # Por que este arquivo é o centro da fase F12
//
// O oráculo `<documento>.recortes.json` existe desde a fase F0 e, até aqui,
// NENHUM teste o consumia. As fases anteriores mediram peças: F5 o texto bruto,
// F6 o normalizado, F7 a busca de uma expressão isolada. Nenhuma delas mede o
// que o serviço realmente faz — o laço completo, com ordenação, deduplicação
// por perfil, ordem de gravação e desfecho.
//
// É justamente onde moram as invariantes mais sutis: INV-P12 (deduplicação por
// perfil), INV-P13 (o sentinela que inicia em zero), INV-P14 (o parcial que
// permanece quando uma chave falha). Um erro em qualquer uma delas passa por
// todos os testes anteriores e altera o conteúdo do banco.
//
// # O que é comparado
//
//	camada 4  o CONJUNTO e a ORDEM dos recortes, por (perfil, expressão, página)
//	camada 5  o desfecho — que determina a sequência de status — e o total
//
// A camada 5 no BANCO REAL está em `banco_test.go`, sob a etiqueta de
// integração: aqui os repositórios são dublês que registram a ordem exata das
// chamadas, o que torna o teste executável na integração contínua sem banco.

// -------------------------------------------------------------------------
// O oráculo
// -------------------------------------------------------------------------

// recorteGravado é uma linha de `<documento>.recortes.json`, na ordem EXATA em
// que o legado a gravaria.
type recorteGravado struct {
	Ordem          int    `json:"ordem"`
	IDPerfil       int64  `json:"id_perfil"`
	ExpressaoBusca string `json:"expressao_busca"`
	NrPagina       int64  `json:"nr_pagina"`
	TextoSHA256    string `json:"texto_sha256"`
	Texto          string `json:"texto"`
}

// recortesEsperados é o arquivo inteiro.
//
// O campo `desfecho` é a etiqueta da enumeração do Rust, e os demais campos
// dependem dela — `serde(flatten)` os coloca no mesmo nível.
type recortesEsperados struct {
	Documento string `json:"documento"`
	SHA256PDF string `json:"sha256_pdf"`

	// Desfecho é "finalizado", "erro_indexacao" ou "erro_recorte".
	Desfecho string `json:"desfecho"`

	// TotalRecortes só existe em "finalizado".
	TotalRecortes int `json:"total_recortes"`

	// Os três abaixo só existem em "erro_recorte".
	IDPerfil           int64  `json:"id_perfil"`
	ExpressaoNM        string `json:"expressao_nm"`
	RecortesJaGravados int    `json:"recortes_ja_gravados"`

	// Detalhe existe em "erro_indexacao" e "erro_recorte".
	Detalhe string `json:"detalhe"`

	Recortes []recorteGravado `json:"recortes"`
}

// chavePesquisaJSON é uma linha de `corpus/chaves.json`.
type chavePesquisaJSON struct {
	IDPerfil    int64  `json:"id_perfil"`
	ExpressaoNM string `json:"expressao_nm"`
}

func carregarRecortesEsperados(t *testing.T) []recortesEsperados {
	t.Helper()
	arquivos, err := filepath.Glob(filepath.Join(dirEsperado, "*.recortes.json"))
	if err != nil {
		t.Fatalf("procurando oráculos de recorte: %v", err)
	}
	if len(arquivos) == 0 {
		t.Skipf("nenhum oráculo .recortes.json em %s — gere o corpus com `make corpus`", dirEsperado)
	}
	sort.Strings(arquivos)

	saida := make([]recortesEsperados, 0, len(arquivos))
	for _, a := range arquivos {
		bruto, err := os.ReadFile(a) //nolint:gosec // caminho vem do glob sobre o diretório de testes
		if err != nil {
			t.Fatalf("lendo %s: %v", a, err)
		}
		var r recortesEsperados
		if err := json.Unmarshal(bruto, &r); err != nil {
			t.Fatalf("analisando %s: %v", a, err)
		}
		saida = append(saida, r)
	}
	return saida
}

// carregarChaves lê `corpus/chaves.json` NA ORDEM DO ARQUIVO.
//
// A ordem é normativa: ela vem do `ORDER BY tp.id_perfil, tpv.expressao_nm` da
// consulta do legado e governa a deduplicação por perfil (INV-P12). Reordenar
// aqui mudaria qual expressão fica gravada numa página disputada.
func carregarChaves(t *testing.T) []domain.ChavePesquisa {
	t.Helper()
	bruto, err := os.ReadFile(filepath.Join(dirCorpus, "chaves.json"))
	if err != nil {
		t.Fatalf("lendo chaves.json: %v", err)
	}
	var linhas []chavePesquisaJSON
	if err := json.Unmarshal(bruto, &linhas); err != nil {
		t.Fatalf("analisando chaves.json: %v", err)
	}

	chaves := make([]domain.ChavePesquisa, 0, len(linhas))
	for _, l := range linhas {
		chaves = append(chaves, domain.ChavePesquisa{IDPerfil: l.IDPerfil, Expressao: l.ExpressaoNM})
	}
	return chaves
}

// -------------------------------------------------------------------------
// Dublês que registram a ordem exata das gravações
// -------------------------------------------------------------------------

// registroDeGravacao é o que o pipeline produziu, na ordem em que produziu.
type registroDeGravacao struct {
	mu sync.Mutex

	recortes []recorteGravado
	status   []domain.StatusImportacao
	inicios  int
	terminos []int32
}

func (r *registroDeGravacao) Registrar(context.Context, domain.Importacao) (int64, error) {
	return 1, nil
}

func (r *registroDeGravacao) AtualizarStatus(
	_ context.Context, _ int64, s domain.StatusImportacao,
) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.status = append(r.status, s)
	return nil
}

func (r *registroDeGravacao) MarcarInicio(context.Context, int64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.inicios++
	return nil
}

func (r *registroDeGravacao) MarcarTermino(_ context.Context, _ int64, total int32) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.terminos = append(r.terminos, total)
	return nil
}

// Salvar registra os recortes de uma chave, na ordem recebida.
func (r *registroDeGravacao) Salvar(
	_ context.Context, _ int64, chave domain.ChavePesquisa, recortes []domain.Recorte,
) (int32, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, recorte := range recortes {
		nrPagina, err := domain.ParaInt64(recorte.Pagina)
		if err != nil {
			return 0, fmt.Errorf("página %d: %w", recorte.Pagina, err)
		}
		r.recortes = append(r.recortes, recorteGravado{
			Ordem:          len(r.recortes),
			IDPerfil:       chave.IDPerfil,
			ExpressaoBusca: chave.Expressao,
			NrPagina:       nrPagina,
			TextoSHA256:    sha256Hex(recorte.Destaque),
			Texto:          recorte.Destaque,
		})
	}
	return int32(len(recortes)), nil //nolint:gosec // limitado pelo número de páginas do documento
}

// chavesFixas devolve sempre a mesma lista, como o dump de perfis do corpus.
type chavesFixas struct{ chaves []domain.ChavePesquisa }

func (c chavesFixas) ChavesPesquisa(context.Context, int64) ([]domain.ChavePesquisa, error) {
	return c.chaves, nil
}

type relogioParado struct{}

func (relogioParado) Agora() time.Time { return time.Unix(0, 0) }

// -------------------------------------------------------------------------
// Camada 4 — o conjunto e a ordem dos recortes
// -------------------------------------------------------------------------

// TestParidadeDoPipeline é o critério de aceite central da fase F12:
// **zero divergência de recortes sobre o corpus dourado**.
//
// Roda o pipeline REAL — `usecase.Pipeline`, o mesmo que produção usa — sobre
// cada documento do corpus, com as chaves na ordem normativa, e compara com o
// que o legado gravaria.
func TestParidadeDoPipeline(t *testing.T) {
	chaves := carregarChaves(t)
	esperados := carregarRecortesEsperados(t)

	var documentosOK, recortesOK, recortesTotal int

	for _, esp := range esperados {
		t.Run(esp.Documento, func(t *testing.T) {
			obtido := executarPipeline(t, esp.Documento, chaves)

			divergencias := compararRecortes(esp, obtido)
			recortesTotal += len(esp.Recortes)
			recortesOK += len(esp.Recortes) - len(divergencias)

			if len(divergencias) == 0 {
				documentosOK++
				return
			}
			for _, d := range divergencias {
				t.Error(d)
			}
		})
	}

	t.Logf("paridade do pipeline: %d/%d documentos, %d/%d recortes",
		documentosOK, len(esperados), recortesOK, recortesTotal)
}

// resultadoDoPipeline é o que a execução em Go produziu.
type resultadoDoPipeline struct {
	Recortes []recorteGravado
	Status   []domain.StatusImportacao
	Terminos []int32
	Inicios  int
}

// executarPipeline roda o pipeline real sobre um documento do corpus.
func executarPipeline(
	t *testing.T, documento string, chaves []domain.ChavePesquisa,
) resultadoDoPipeline {
	t.Helper()

	conteudo, err := os.ReadFile(filepath.Join(dirCorpus, documento+".pdf"))
	if err != nil {
		t.Fatalf("lendo o PDF: %v", err)
	}

	registro := &registroDeGravacao{}
	pipeline, err := usecase.NovoPipeline(usecase.DependenciasDoPipeline{
		Importacoes: registro,
		Perfis:      chavesFixas{chaves: chaves},
		Recortes:    registro,
		// O extrator de PRODUÇÃO — o normalizado. Usar o cru aqui reproduziria
		// o defeito que este teste existe para pegar.
		Extrator:  pdftext.NovoExtratorNormalizado(),
		Indexador: searchidx.NovoIndexador(),
		Relogio:   relogioParado{},
		Logger:    slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatalf("NovoPipeline: %v", err)
	}

	pipeline.Processar(context.Background(), 1, conteudo)

	registro.mu.Lock()
	defer registro.mu.Unlock()
	return resultadoDoPipeline{
		Recortes: append([]recorteGravado(nil), registro.recortes...),
		Status:   append([]domain.StatusImportacao(nil), registro.status...),
		Terminos: append([]int32(nil), registro.terminos...),
		Inicios:  registro.inicios,
	}
}

// compararRecortes confronta o obtido com o oráculo, campo a campo.
//
// A ORDEM é comparada junto com o conteúdo: dois conjuntos iguais gravados em
// ordens diferentes produzem identificadores diferentes em `tb_recorte`, e a
// ordem é o que INV-P12 e INV-P13 determinam.
func compararRecortes(esp recortesEsperados, obtido resultadoDoPipeline) []string {
	var divergencias []string

	if len(esp.Recortes) != len(obtido.Recortes) {
		divergencias = append(divergencias, fmt.Sprintf(
			"quantidade de recortes: esperados %d, obtidos %d",
			len(esp.Recortes), len(obtido.Recortes)))
	}

	limite := min(len(esp.Recortes), len(obtido.Recortes))
	for i := range limite {
		e, o := esp.Recortes[i], obtido.Recortes[i]
		switch {
		case e.IDPerfil != o.IDPerfil:
			divergencias = append(divergencias, fmt.Sprintf(
				"recorte %d: id_perfil esperado %d, obtido %d", i, e.IDPerfil, o.IDPerfil))
		case e.ExpressaoBusca != o.ExpressaoBusca:
			divergencias = append(divergencias, fmt.Sprintf(
				"recorte %d: expressao_busca esperada %q, obtida %q",
				i, e.ExpressaoBusca, o.ExpressaoBusca))
		case e.NrPagina != o.NrPagina:
			divergencias = append(divergencias, fmt.Sprintf(
				"recorte %d (perfil %d, %q): nr_pagina esperada %d, obtida %d",
				i, e.IDPerfil, e.ExpressaoBusca, e.NrPagina, o.NrPagina))
		case e.TextoSHA256 != o.TextoSHA256:
			divergencias = append(divergencias, fmt.Sprintf(
				"recorte %d (perfil %d, %q, página %d): texto divergente\n%s",
				i, e.IDPerfil, e.ExpressaoBusca, e.NrPagina,
				menorTrechoDivergente(e.Texto, o.Texto)))
		}
	}

	// Sobras, dos dois lados, nomeadas uma a uma: "faltou 1 recorte" não diz
	// qual perfil deixou de aparecer.
	for i := limite; i < len(esp.Recortes); i++ {
		e := esp.Recortes[i]
		divergencias = append(divergencias, fmt.Sprintf(
			"recorte %d FALTOU: perfil %d, %q, página %d", i, e.IDPerfil, e.ExpressaoBusca, e.NrPagina))
	}
	for i := limite; i < len(obtido.Recortes); i++ {
		o := obtido.Recortes[i]
		divergencias = append(divergencias, fmt.Sprintf(
			"recorte %d SOBROU: perfil %d, %q, página %d", i, o.IDPerfil, o.ExpressaoBusca, o.NrPagina))
	}

	return divergencias
}

// -------------------------------------------------------------------------
// Camada 5 — desfecho, sequência de status e total
// -------------------------------------------------------------------------

// TestParidadeDaMaquinaDeEstados compara a SEQUÊNCIA de status observada.
//
// O oráculo não grava a sequência: ele grava o DESFECHO, do qual a sequência é
// derivável por docs/ESPECIFICACAO.md §3.3. A derivação está em
// `sequenciaEsperada`, e é ela que este teste confere — junto com o
// `total_recortes` de `MarcarTermino`, que é a outra coluna observável.
func TestParidadeDaMaquinaDeEstados(t *testing.T) {
	chaves := carregarChaves(t)

	for _, esp := range carregarRecortesEsperados(t) {
		t.Run(esp.Documento, func(t *testing.T) {
			obtido := executarPipeline(t, esp.Documento, chaves)

			esperada := sequenciaEsperada(esp)
			if !mesmaSequencia(esperada, obtido.Status) {
				t.Errorf("sequência de status divergente (desfecho %q)\n  esperada: %v\n  obtida:   %v",
					esp.Desfecho, esperada, obtido.Status)
			}

			// `data_inicio` é gravada UMA vez, sempre, antes do status 1
			// (main.rs:260-261).
			if obtido.Inicios != 1 {
				t.Errorf("MarcarInicio chamado %d vez(es); esperava exatamente 1", obtido.Inicios)
			}

			conferirTermino(t, esp, obtido)
		})
	}
}

// sequenciaEsperada deriva a sequência de status do desfecho capturado.
//
// Vem de docs/ESPECIFICACAO.md §3.3 e das gravações de reference/main.rs:261,
// 268, 272, 320, 325 e 334. NÃO inclui o status 0 do caminho síncrono: ele é
// gravado pela ingestão, não pelo pipeline.
func sequenciaEsperada(esp recortesEsperados) []domain.StatusImportacao {
	base := []domain.StatusImportacao{domain.StatusSelecionado, domain.StatusIndexando}

	switch esp.Desfecho {
	case "erro_indexacao":
		// main.rs:332-335 — falha em criar_indice: nunca chega ao status 3.
		return append(base, domain.StatusErro)
	case "erro_recorte":
		// main.rs:318-322 — o índice existe, o status 3 foi gravado, e a falha
		// numa chave leva a -1. O trabalho parcial PERMANECE (INV-P14).
		return append(base, domain.StatusRecortando, domain.StatusErro)
	default: // "finalizado"
		return append(base, domain.StatusRecortando, domain.StatusFinalizado)
	}
}

func mesmaSequencia(a, b []domain.StatusImportacao) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// conferirTermino verifica `data_fim` e `total_recortes`.
//
// Só o desfecho "finalizado" grava as duas colunas (main.rs:326); os dois
// caminhos de erro encerram antes, e é isso que deixa `data_fim` nula numa
// importação em -1.
func conferirTermino(t *testing.T, esp recortesEsperados, obtido resultadoDoPipeline) {
	t.Helper()

	if esp.Desfecho != "finalizado" {
		if len(obtido.Terminos) != 0 {
			t.Errorf("MarcarTermino chamado no desfecho %q; o legado encerra antes",
				esp.Desfecho)
		}
		return
	}

	if len(obtido.Terminos) != 1 {
		t.Fatalf("MarcarTermino chamado %d vez(es); esperava 1", len(obtido.Terminos))
	}
	if int(obtido.Terminos[0]) != esp.TotalRecortes {
		t.Errorf("total_recortes = %d; o legado gravaria %d",
			obtido.Terminos[0], esp.TotalRecortes)
	}

	// E o total gravado precisa bater com o que foi de fato gravado — o legado
	// conta os FILTRADOS, não os encontrados (main.rs:306).
	if int(obtido.Terminos[0]) != len(obtido.Recortes) {
		t.Errorf("total_recortes = %d mas %d recorte(s) foram gravados",
			obtido.Terminos[0], len(obtido.Recortes))
	}
}

// -------------------------------------------------------------------------
// Minimização — o menor caso reprodutor
// -------------------------------------------------------------------------

// menorTrechoDivergente reduz duas cadeias ao menor trecho que as distingue.
//
// É a redução automática que a fase pede: sem ela, uma divergência de texto
// imprime duas páginas inteiras e quem lê tem de encontrar a diferença no olho.
// A redução é por LINHA primeiro — que é a unidade em que o texto é produzido —
// e depois por RUNA dentro da linha divergente.
func menorTrechoDivergente(esperado, obtido string) string {
	linhasE := strings.Split(esperado, "\n")
	linhasO := strings.Split(obtido, "\n")

	for i := range max(len(linhasE), len(linhasO)) {
		e := linhaOuVazio(linhasE, i)
		o := linhaOuVazio(linhasO, i)
		if e == o {
			continue
		}
		return fmt.Sprintf("    linha %d:\n      esperado: %q\n      obtido:   %q\n      %s",
			i+1, e, o, primeiraRunaDivergente(e, o))
	}

	if len(linhasE) != len(linhasO) {
		return fmt.Sprintf("    número de linhas: esperado %d, obtido %d", len(linhasE), len(linhasO))
	}
	return "    (as cadeias são iguais linha a linha, mas os resumos divergem)"
}

func linhaOuVazio(linhas []string, i int) string {
	if i < len(linhas) {
		return linhas[i]
	}
	return ""
}

// primeiraRunaDivergente aponta a posição exata e nomeia os pontos de código.
//
// Nomear o ponto de código é o que distingue um caractere invisível de outro:
// espaço estreito e espaço comum imprimem igual e não são o mesmo.
func primeiraRunaDivergente(esperado, obtido string) string {
	e := []rune(esperado)
	o := []rune(obtido)
	for i := range min(len(e), len(o)) {
		if e[i] != o[i] {
			return fmt.Sprintf("primeira diferença na runa %d: U+%04X (%q) vs U+%04X (%q)",
				i, e[i], string(e[i]), o[i], string(o[i]))
		}
	}
	if len(e) < len(o) {
		return fmt.Sprintf("o obtido tem %d runa(s) a mais, a partir de U+%04X (%q)",
			len(o)-len(e), o[len(e)], string(o[len(e)]))
	}
	if len(o) < len(e) {
		return fmt.Sprintf("faltam %d runa(s) no obtido, a partir de U+%04X (%q)",
			len(e)-len(o), e[len(o)], string(e[len(o)]))
	}
	return "as linhas são iguais"
}

// sha256Hex é o mesmo resumo que a captura usa em `texto_sha256`.
func sha256Hex(s string) string {
	soma := sha256.Sum256([]byte(s))
	return hex.EncodeToString(soma[:])
}
