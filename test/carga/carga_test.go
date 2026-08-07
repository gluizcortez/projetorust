//go:build carga

// Package carga_test mede CORREÇÃO sob carga e memória por importação.
//
// # Por que um marcador de compilação próprio
//
// Estes testes rodam por dezenas de segundos e alocam gigabytes: não podem
// entrar na suíte de todo dia. Rodam com `make load-test`.
//
// # Por que dentro do processo, e não por HTTP
//
// O que a fase pede é verificar CORREÇÃO sob carga, não latência de rede. A
// camada HTTP não guarda estado entre requisições — o que pode se corromper sob
// concorrência é o pipeline, e é ele que estes testes martelam. Medir por HTTP
// acrescentaria o custo do multipart e do banco a cada caso, o que reduziria o
// número de importações simultâneas alcançável sem medir nada a mais.
//
// A latência do caminho SÍNCRONO — o critério de p99 da fase — é outra
// medição, e depende de comparar com o legado em produção: ver
// docs/RELATORIO-PARIDADE.md.
package carga_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/gluizcortez/projetorust/internal/domain"
	"github.com/gluizcortez/projetorust/tools/comparador"
)

const dirCorpus = "../testdata/corpus"

// PicoHistoricoPorHora é o pico de documentos por hora assumido.
//
// ATENÇÃO — É UMA SUPOSIÇÃO, não uma medição. O valor real é a decisão aberta
// **D-04**, que nunca foi respondida. Está aqui, nomeado e visível, justamente
// para que ninguém leia o resultado deste teste como "aguenta o dobro do pico"
// sem saber de que pico se fala.
//
// Sobrescreva com PICO_HISTORICO_POR_HORA quando o número real existir.
const PicoHistoricoPorHora = 500

// FatorDeCarga é o multiplicador exigido pela fase: o DOBRO do pico.
const FatorDeCarga = 2

func picoPorHora(t *testing.T) int {
	t.Helper()
	if v := os.Getenv("PICO_HISTORICO_POR_HORA"); v != "" {
		var n int
		if _, err := fmt.Sscanf(v, "%d", &n); err == nil && n > 0 {
			return n
		}
		t.Fatalf("PICO_HISTORICO_POR_HORA=%q não é um inteiro positivo", v)
	}
	return PicoHistoricoPorHora
}

// maiorDocumento devolve o PDF mais pesado do corpus.
func maiorDocumento(t *testing.T) (string, []byte) {
	t.Helper()

	arquivos, err := filepath.Glob(filepath.Join(dirCorpus, "*.pdf"))
	if err != nil || len(arquivos) == 0 {
		t.Skipf("corpus indisponível — gere com `make corpus`")
	}

	var (
		maiorNome     string
		maiorConteudo []byte
	)
	for _, a := range arquivos {
		conteudo, err := os.ReadFile(a) //nolint:gosec // caminho vem do glob do corpus
		if err != nil {
			continue
		}
		if len(conteudo) > len(maiorConteudo) {
			maiorNome, maiorConteudo = filepath.Base(a), conteudo
		}
	}
	if maiorConteudo == nil {
		t.Skip("nenhum PDF legível no corpus")
	}
	return maiorNome, maiorConteudo
}

func carregarChaves(t *testing.T) []domain.ChavePesquisa {
	t.Helper()

	bruto, err := os.ReadFile(filepath.Join(dirCorpus, "chaves.json"))
	if err != nil {
		t.Skipf("chaves.json indisponível: %v", err)
	}
	var linhas []struct {
		IDPerfil    int64  `json:"id_perfil"`
		ExpressaoNM string `json:"expressao_nm"`
	}
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
// Correção sob carga
// -------------------------------------------------------------------------

// TestCargaCorrecaoSobDobroDoPico processa o maior documento do corpus em
// paralelo e exige que TODAS as execuções produzam o MESMO resultado.
//
// # O que se mede
//
// Não é vazão: é CORREÇÃO. O pipeline compartilha um índice por importação, mas
// as buscas dentro de uma importação são sequenciais e o índice é imutável — se
// houver estado compartilhado indevido entre importações simultâneas, ele
// aparece como resultado diferente entre execuções idênticas.
//
// Rodar com `-race` é o que torna o teste capaz de acusar a corrida em si, e
// não só o sintoma.
func TestCargaCorrecaoSobDobroDoPico(t *testing.T) {
	nome, conteudo := maiorDocumento(t)
	chaves := carregarChaves(t)

	// A carga por hora vira concorrência instantânea: processar `n` documentos
	// ao mesmo tempo é MAIS agressivo que distribuí-los por uma hora, e é o
	// pior caso que o teto de concorrência precisa suportar.
	simultaneas := simultaneasParaOPico(picoPorHora(t) * FatorDeCarga)

	t.Logf("documento %s (%d bytes), %d importações simultâneas "+
		"(equivalente a %d/h, o dobro do pico assumido de %d/h — ver D-04)",
		nome, len(conteudo), simultaneas, picoPorHora(t)*FatorDeCarga, picoPorHora(t))

	// A referência é UMA execução isolada, antes de qualquer concorrência.
	referencia := comparador.ExecutarPipeline(context.Background(), conteudo, chaves)
	if len(referencia.Recortes) == 0 {
		t.Fatalf("o documento %s não produziu recorte algum; o teste não mediria nada", nome)
	}

	resultados := make([]comparador.ResultadoDoPipeline, simultaneas)
	var grupo sync.WaitGroup

	inicio := time.Now()
	for i := range simultaneas {
		grupo.Add(1)
		go func(i int) {
			defer grupo.Done()
			resultados[i] = comparador.ExecutarPipeline(context.Background(), conteudo, chaves)
		}(i)
	}
	grupo.Wait()
	duracao := time.Since(inicio)

	var divergentes int
	for i, r := range resultados {
		if d := compararResultados(referencia, r); d != "" {
			divergentes++
			if divergentes <= 3 {
				t.Errorf("execução %d divergiu da referência:\n%s", i, d)
			}
		}
	}
	if divergentes > 0 {
		t.Errorf("%d de %d execuções simultâneas divergiram", divergentes, simultaneas)
	}

	t.Logf("carga: %d importações em %s (%.1f/s, %.0f/h), %d recorte(s) cada, zero divergência",
		simultaneas, duracao.Round(time.Millisecond),
		float64(simultaneas)/duracao.Seconds(),
		float64(simultaneas)/duracao.Seconds()*3600,
		len(referencia.Recortes))
}

// simultaneasParaOPico converte documentos/hora em importações simultâneas.
//
// O teto é deliberado: além de algumas dezenas de importações do maior
// documento, o teste mede a memória da máquina de teste e não a correção do
// serviço — e uma morte por falta de memória não é resultado, é ruído.
func simultaneasParaOPico(porHora int) int {
	const teto = 64
	if porHora < teto {
		return porHora
	}
	return teto
}

func compararResultados(a, b comparador.ResultadoDoPipeline) string {
	if len(a.Recortes) != len(b.Recortes) {
		return fmt.Sprintf("  recortes: referência %d, obtidos %d", len(a.Recortes), len(b.Recortes))
	}
	for i := range a.Recortes {
		if a.Recortes[i] != b.Recortes[i] {
			return fmt.Sprintf("  recorte %d:\n    referência: %+v\n    obtido:     %+v",
				i, a.Recortes[i], b.Recortes[i])
		}
	}
	if len(a.Status) != len(b.Status) {
		return fmt.Sprintf("  status: referência %v, obtidos %v", a.Status, b.Status)
	}
	for i := range a.Status {
		if a.Status[i] != b.Status[i] {
			return fmt.Sprintf("  sequência de status: referência %v, obtida %v", a.Status, b.Status)
		}
	}
	return ""
}

// -------------------------------------------------------------------------
// Memória por importação
// -------------------------------------------------------------------------

// TestCargaMemoriaPorImportacao mede quanto uma importação segura no pico, e
// deriva o teto de concorrência recomendado.
//
// # Duas medidas, porque uma sozinha engana
//
//	PICO VIVO      o maior `HeapAlloc` observado DURANTE o processamento. É o
//	               que dimensiona o teto de concorrência: é essa memória que N
//	               importações simultâneas seguram ao mesmo tempo.
//	ALOCAÇÃO TOTAL tudo que foi alocado, inclusive o já recolhido. NÃO é o
//	               consumo simultâneo — é a pressão sobre o coletor.
//
// Reportar só a alocação total superestimaria o teto necessário; reportar só o
// pico esconderia o custo de coleta. As duas juntas descrevem o que acontece.
//
// # O que este teste NÃO faz
//
// Não reprova. Ele MEDE e RELATA: não existe um valor "certo" contra o qual
// comparar, porque o número de produção depende do maior PDF real — decisão
// aberta **D-04** — e o corpus aqui é sintético e minúsculo (**D-11**).
//
// # Por que o fator medido NÃO se transfere para um diário real
//
// O documento do corpus tem ~100 KiB. Boa parte do que uma importação aloca é
// custo FIXO — tabelas do tokenizador, mapas do índice, estruturas do pipeline
// — e não escala com o tamanho do PDF. Sobre 100 KiB, esse custo fixo domina e
// infla o fator; sobre 30 MiB ele desapareceria no ruído. Portanto o fator
// medido aqui é um TETO para documentos pequenos, e não uma previsão para
// documentos grandes.
func TestCargaMemoriaPorImportacao(t *testing.T) {
	nome, conteudo := maiorDocumento(t)
	chaves := carregarChaves(t)

	// Uma execução de aquecimento: a primeira paga tabelas e caches que não
	// pertencem à importação.
	_ = comparador.ExecutarPipeline(context.Background(), conteudo, chaves)

	const amostras = 5
	var (
		picos  = make([]uint64, 0, amostras)
		totais = make([]uint64, 0, amostras)
	)

	for range amostras {
		pico, total := medirUmaImportacao(conteudo, chaves)
		picos = append(picos, pico)
		totais = append(totais, total)
	}

	picoMediano := mediana(picos)
	totalMediano := mediana(totais)
	fatorPico := float64(picoMediano) / float64(len(conteudo))
	fatorTotal := float64(totalMediano) / float64(len(conteudo))

	t.Logf("memória por importação — documento %s", nome)
	t.Logf("  tamanho do PDF         %s", emMiB(uint64(len(conteudo))))
	t.Logf("  pico vivo              %s  (%.1f× o PDF)", emMiB(picoMediano), fatorPico)
	t.Logf("  alocação total         %s  (%.1f× o PDF)", emMiB(totalMediano), fatorTotal)
	t.Logf("  medianas de %d amostras", amostras)
	t.Logf("")

	// O confronto com a regra de bolso de docs/OPERACAO.md §5, dito como é —
	// e não como seria conveniente.
	const regraDeBolso = 8.0
	switch {
	case fatorPico > regraDeBolso:
		t.Logf("  ATENÇÃO: o pico medido (%.1f×) está ACIMA da regra de bolso de %.0f×", fatorPico, regraDeBolso)
		t.Logf("  de docs/OPERACAO.md §5. Sobre um documento de %s, o custo FIXO por", emMiB(uint64(len(conteudo))))
		t.Logf("  importação domina e infla o fator — mas a regra segue SEM VALIDAÇÃO")
		t.Logf("  em documento de porte real. Ver D-04 e D-11.")
	default:
		t.Logf("  o pico medido (%.1f×) cabe na regra de bolso de %.0f× de", fatorPico, regraDeBolso)
		t.Logf("  docs/OPERACAO.md §5 — para ESTE tamanho de documento.")
	}
	t.Logf("")

	// O teto derivado usa o PICO MEDIDO, não a regra de bolso: é o número que
	// este teste realmente sustenta. Os dois estão lado a lado de propósito.
	t.Logf("  teto de concorrência derivado, com folga de 40%% para o coletor,")
	t.Logf("  o pool de conexões e o servidor HTTP:")
	for _, gib := range []uint64{2, 4, 8, 16} {
		disponivel := float64(gib<<30) * 0.6
		pelaMedicao := int(disponivel / float64(picoMediano))
		pelaRegra := int(disponivel / (regraDeBolso * float64(len(conteudo))))
		t.Logf("    %2d GiB → %d pela medição deste documento, %d pela regra de bolso",
			gib, pelaMedicao, pelaRegra)
	}
	t.Logf("")
	t.Logf("  NENHUM desses números vale para produção enquanto D-04 não disser")
	t.Logf("  qual é o maior PDF real. Ver docs/RELATORIO-PARIDADE.md.")
}

// medirUmaImportacao devolve o pico de memória viva e o total alocado.
//
// O pico é amostrado por uma goroutine que lê `HeapAlloc` enquanto a importação
// corre. É amostragem, não instrumentação: o valor é um PISO do pico real,
// porque um transiente entre duas leituras passa despercebido. O intervalo é
// curto o bastante para que isso importe pouco, e a alternativa — instrumentar
// o alocador — mediria o instrumento junto.
func medirUmaImportacao(conteudo []byte, chaves []domain.ChavePesquisa) (pico, total uint64) {
	runtime.GC()

	var antes runtime.MemStats
	runtime.ReadMemStats(&antes)

	parar := make(chan struct{})
	medido := make(chan uint64, 1)

	go func() {
		var maior uint64
		var m runtime.MemStats
		for {
			select {
			case <-parar:
				medido <- maior
				return
			default:
			}
			runtime.ReadMemStats(&m)
			if m.HeapAlloc > maior {
				maior = m.HeapAlloc
			}
			// `ReadMemStats` PARA O MUNDO. Sem a pausa, a amostragem
			// dominaria o tempo da importação e mediria a si mesma.
			time.Sleep(intervaloDeAmostragem)
		}
	}()

	resultado := comparador.ExecutarPipeline(context.Background(), conteudo, chaves)

	var depois runtime.MemStats
	runtime.ReadMemStats(&depois)
	close(parar)
	maiorObservado := <-medido

	runtime.KeepAlive(resultado)

	// O pico é o maior heap observado MENOS o que já havia antes: o que
	// interessa é o custo DESTA importação, não o do processo de teste.
	if maiorObservado > antes.HeapAlloc {
		pico = maiorObservado - antes.HeapAlloc
	}
	return pico, depois.TotalAlloc - antes.TotalAlloc
}

// intervaloDeAmostragem é a pausa entre leituras de memória.
const intervaloDeAmostragem = 200 * time.Microsecond

func mediana(valores []uint64) uint64 {
	ordenados := append([]uint64(nil), valores...)
	sort.Slice(ordenados, func(i, j int) bool { return ordenados[i] < ordenados[j] })
	return ordenados[len(ordenados)/2]
}

func emMiB(bytes uint64) string {
	return fmt.Sprintf("%.1f MiB", float64(bytes)/(1<<20))
}
