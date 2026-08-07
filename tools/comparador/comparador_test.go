package comparador

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/gluizcortez/projetorust/internal/domain"
)

// O comparador é o instrumento de medição da fase F12. Um instrumento que
// mede errado é pior que nenhum: ele produz um relatório verde e uma decisão de
// corte apoiada em nada. Os testes abaixo cercam as três coisas que ele
// precisa acertar — o veredito, a redução e a classificação.

// -------------------------------------------------------------------------
// O veredito
// -------------------------------------------------------------------------

// TestAprovadoSoOlhaAsCamadas4e5 fixa o critério de corte.
//
// As camadas 1 a 3 são diagnósticas: uma diferença de termo que não chega a
// mudar recorte não altera o conteúdo do banco, e barrar o corte por causa dela
// seria confundir "sintoma" com "consequência".
func TestAprovadoSoOlhaAsCamadas4e5(t *testing.T) {
	casos := []struct {
		nome     string
		camadas  []ResumoDeCamada
		aprovado bool
	}{
		{"tudo limpo", []ResumoDeCamada{
			{Camada: CamadaExtracao, Iguais: 10},
			{Camada: CamadaRecortes, Iguais: 5},
			{Camada: CamadaEstados, Iguais: 3},
		}, true},
		{"divergência só na extração", []ResumoDeCamada{
			{Camada: CamadaExtracao, Divergentes: 1},
			{Camada: CamadaRecortes, Iguais: 5},
		}, true},
		{"divergência só na normalização", []ResumoDeCamada{
			{Camada: CamadaNormalizacao, Divergentes: 7},
			{Camada: CamadaEstados, Iguais: 3},
		}, true},
		{"divergência de recorte", []ResumoDeCamada{
			{Camada: CamadaRecortes, Divergentes: 1},
		}, false},
		{"divergência de estado", []ResumoDeCamada{
			{Camada: CamadaEstados, Divergentes: 1},
		}, false},
		{"nada comparado", nil, true},
	}

	for _, caso := range casos {
		t.Run(caso.nome, func(t *testing.T) {
			rel := Relatorio{Camadas: caso.camadas}
			if rel.Aprovado() != caso.aprovado {
				t.Errorf("Aprovado() = %t; esperava %t", rel.Aprovado(), caso.aprovado)
			}
		})
	}
}

// TestTaxaComDenominadorZero: uma camada que não comparou nada não pode
// devolver taxa infinita nem entrar em pânico.
func TestTaxaComDenominadorZero(t *testing.T) {
	if taxa := (ResumoDeCamada{}).Taxa(); taxa != 0 {
		t.Errorf("Taxa() = %v com zero comparados; esperava 0", taxa)
	}
}

// -------------------------------------------------------------------------
// A redução automática
// -------------------------------------------------------------------------

func TestReduzirEncontraALinhaEARuna(t *testing.T) {
	casos := []struct {
		nome                    string
		esperado, obtido        string
		linha, runa             int
		trechoEsperado, trechoO string
	}{
		{
			nome:     "diferença na terceira linha",
			esperado: "igual\nigual tambem\nCONTINUACAO do processo",
			obtido:   "igual\nigual tambem\nCONTINUAÇÃO do processo",
			// "CONTINUACAO": o C que vira Ç é a nona runa, índice 8.
			linha: 3, runa: 8,
			trechoEsperado: "CONTINUACAO do processo",
			trechoO:        "CONTINUAÇÃO do processo",
		},
		{
			nome:     "hífen não rejuntado",
			esperado: "a continuacao",
			obtido:   "a conti-",
			linha:    1, runa: 7,
			trechoEsperado: "a continuacao",
			trechoO:        "a conti-",
		},
		{
			nome:     "o obtido tem uma linha a mais",
			esperado: "uma linha",
			obtido:   "uma linha\nsobrando",
			linha:    2, runa: 0,
			trechoEsperado: "",
			trechoO:        "sobrando",
		},
	}

	for _, caso := range casos {
		t.Run(caso.nome, func(t *testing.T) {
			m := reduzir(caso.esperado, caso.obtido)
			if m == nil {
				t.Fatal("reduzir devolveu nulo para cadeias diferentes")
			}
			if m.Linha != caso.linha {
				t.Errorf("linha = %d; esperava %d", m.Linha, caso.linha)
			}
			if m.Runa != caso.runa {
				t.Errorf("runa = %d; esperava %d", m.Runa, caso.runa)
			}
			if m.Esperado != caso.trechoEsperado {
				t.Errorf("trecho esperado = %q; queria %q", m.Esperado, caso.trechoEsperado)
			}
			if m.Obtido != caso.trechoO {
				t.Errorf("trecho obtido = %q; queria %q", m.Obtido, caso.trechoO)
			}

			// A LINHA é menor que a página inteira — é o ponto da redução.
			if len(m.Esperado) > len(caso.esperado) {
				t.Error("a redução devolveu mais que a entrada")
			}
		})
	}
}

// TestReduzirDevolveNuloParaIguais.
func TestReduzirDevolveNuloParaIguais(t *testing.T) {
	if m := reduzir("mesmo texto", "mesmo texto"); m != nil {
		t.Errorf("reduzir devolveu %+v para cadeias iguais", m)
	}
}

// TestReduzirNomeiaOsPontosDeCodigo.
//
// Nomear o ponto de código é o que distingue um caractere invisível de outro:
// espaço estreito e espaço comum imprimem igual e não são o mesmo. Sem isso, a
// divergência de INV-P10 seria ilegível no relatório.
func TestReduzirNomeiaOsPontosDeCodigo(t *testing.T) {
	m := reduzir("a b", "a b") // espaço comum contra espaço inquebrável
	if m == nil {
		t.Fatal("reduzir devolveu nulo")
	}
	if !strings.Contains(m.PontoEsperado, "U+0020") {
		t.Errorf("ponto esperado = %q; deveria nomear U+0020", m.PontoEsperado)
	}
	if !strings.Contains(m.PontoObtido, "U+00A0") {
		t.Errorf("ponto obtido = %q; deveria nomear U+00A0", m.PontoObtido)
	}
}

// -------------------------------------------------------------------------
// A classificação
// -------------------------------------------------------------------------

// TestClassificacaoAgrupaPorCausaProvavel.
//
// A classificação é heurística de TRIAGEM, não diagnóstico: ela existe para que
// mil divergências com a mesma causa virem uma classe com um reprodutor, em vez
// de mil linhas no relatório.
func TestClassificacaoAgrupaPorCausaProvavel(t *testing.T) {
	casos := []struct {
		nome             string
		esperado, obtido string
		classe           Classe
	}{
		{"hífen no fim da linha", "continuacao", "conti-", ClasseHifen},
		{"diacrítico preservado", "CONCEICAO", "CONCEIÇÃO", ClasseDiacritico},
		{"espaço trocado", "a b", "a b", ClasseEspaco},
		{"texto simplesmente outro", "alfa", "beta", ClasseTexto},
	}

	for _, caso := range casos {
		t.Run(caso.nome, func(t *testing.T) {
			if obtida := classeDeTexto(reduzir(caso.esperado, caso.obtido)); obtida != caso.classe {
				t.Errorf("classe = %q; esperava %q", obtida, caso.classe)
			}
		})
	}
}

func TestClasseDeTextoComMenorCasoNulo(t *testing.T) {
	if c := classeDeTexto(nil); c != ClasseDesconhecida {
		t.Errorf("classe = %q; esperava %q", c, ClasseDesconhecida)
	}
}

// -------------------------------------------------------------------------
// A derivação da sequência de status
// -------------------------------------------------------------------------

// TestSequenciaEsperadaPorDesfecho fixa a tradução de docs/ESPECIFICACAO.md
// §3.3.
//
// O oráculo grava o DESFECHO, não a sequência; errar esta derivação faria o
// comparador aprovar uma máquina de estados divergente.
func TestSequenciaEsperadaPorDesfecho(t *testing.T) {
	casos := map[string][]domain.StatusImportacao{
		"finalizado": {
			domain.StatusSelecionado, domain.StatusIndexando,
			domain.StatusRecortando, domain.StatusFinalizado,
		},
		// Falha em criar_indice: NUNCA chega ao status 3 (main.rs:332-335).
		"erro_indexacao": {
			domain.StatusSelecionado, domain.StatusIndexando, domain.StatusErro,
		},
		// Falha numa chave: o índice existe, o 3 foi gravado (main.rs:318-322).
		"erro_recorte": {
			domain.StatusSelecionado, domain.StatusIndexando,
			domain.StatusRecortando, domain.StatusErro,
		},
	}

	for desfecho, esperada := range casos {
		t.Run(desfecho, func(t *testing.T) {
			if obtida := SequenciaEsperada(desfecho); !igualStatus(esperada, obtida) {
				t.Errorf("sequência = %v; esperava %v", obtida, esperada)
			}
		})
	}
}

// -------------------------------------------------------------------------
// Os relatórios
// -------------------------------------------------------------------------

func relatorioDeExemplo() Relatorio {
	return Relatorio{
		GeradoEm:   time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC),
		DirCorpus:  "test/testdata/corpus",
		Documentos: 28,
		Camadas: []ResumoDeCamada{
			{Camada: CamadaExtracao, Comparados: 159, Iguais: 159, Unidade: "páginas"},
			{
				Camada: CamadaRecortes, Comparados: 38, Iguais: 33, Divergentes: 5,
				Unidade: "recortes", PorClasse: map[Classe]int{ClasseHifen: 5},
			},
		},
		Divergencias: []Divergencia{{
			Camada: CamadaRecortes, Classe: ClasseHifen, Documento: "03-x", Pagina: 1,
			Detalhe:   "texto divergente",
			MenorCaso: reduzir("continuacao", "conti-"),
		}},
	}
}

// TestRelatorioTextoTrazVereditoECaso.
func TestRelatorioTextoTrazVereditoECaso(t *testing.T) {
	var b bytes.Buffer
	if err := EscreverTexto(&b, relatorioDeExemplo()); err != nil {
		t.Fatalf("EscreverTexto: %v", err)
	}
	texto := b.String()

	for _, trecho := range []string{
		"REPROVADO",          // o veredito, no topo
		"4-recortes",         // a camada
		"hifen-fim-de-linha", // a classe
		"13.1579%",           // a taxa, 5 de 38
		"03-x",               // o documento
		`"conti-"`,           // o menor caso
		"primeira diferença", // a runa
	} {
		if !strings.Contains(texto, trecho) {
			t.Errorf("o relatório não traz %q\n---\n%s", trecho, texto)
		}
	}
}

// TestRelatorioTextoAprovadoNaoInventaDivergencia.
func TestRelatorioTextoAprovadoNaoInventaDivergencia(t *testing.T) {
	var b bytes.Buffer
	rel := Relatorio{
		GeradoEm: time.Now(),
		Camadas:  []ResumoDeCamada{{Camada: CamadaRecortes, Comparados: 10, Iguais: 10, Unidade: "recortes"}},
	}
	if err := EscreverTexto(&b, rel); err != nil {
		t.Fatalf("EscreverTexto: %v", err)
	}
	if !strings.Contains(b.String(), "APROVADO") {
		t.Error("o relatório sem divergência deveria dizer APROVADO")
	}
	if !strings.Contains(b.String(), "Nenhuma divergência") {
		t.Error("faltou a linha de ausência de divergência")
	}
}

// TestRelatorioJSONEhRedondo: o que sai tem de voltar igual, senão o painel lê
// outra coisa que o texto.
func TestRelatorioJSONEhRedondo(t *testing.T) {
	original := relatorioDeExemplo()

	var b bytes.Buffer
	if err := EscreverJSON(&b, original); err != nil {
		t.Fatalf("EscreverJSON: %v", err)
	}

	var volta Relatorio
	if err := json.Unmarshal(b.Bytes(), &volta); err != nil {
		t.Fatalf("decodificando: %v — %s", err, b.String())
	}

	if volta.Documentos != original.Documentos {
		t.Errorf("documentos = %d; esperava %d", volta.Documentos, original.Documentos)
	}
	if len(volta.Camadas) != len(original.Camadas) {
		t.Fatalf("camadas = %d; esperava %d", len(volta.Camadas), len(original.Camadas))
	}
	if volta.Camadas[1].PorClasse[ClasseHifen] != 5 {
		t.Errorf("contagem por classe não sobreviveu: %+v", volta.Camadas[1].PorClasse)
	}
	if len(volta.Divergencias) != 1 || volta.Divergencias[0].MenorCaso == nil {
		t.Fatalf("o menor caso não sobreviveu: %+v", volta.Divergencias)
	}
	if volta.Divergencias[0].MenorCaso.Obtido != "conti-" {
		t.Errorf("menor caso = %q", volta.Divergencias[0].MenorCaso.Obtido)
	}
}

// -------------------------------------------------------------------------
// O acumulador
// -------------------------------------------------------------------------

// TestAcumuladorGuardaUmRepresentantePorClasse.
//
// É o que impede o relatório de virar uma lista de mil linhas iguais.
func TestAcumuladorGuardaUmRepresentantePorClasse(t *testing.T) {
	ac := novoAcumulador()

	for i := range 100 {
		ac.anotar(Divergencia{
			Camada: CamadaRecortes, Classe: ClasseHifen,
			Documento: "doc", Pagina: i + 1, Detalhe: "divergência",
		})
	}
	ac.anotar(Divergencia{Camada: CamadaRecortes, Classe: ClasseDiacritico, Documento: "outro"})

	reps := ac.representantes()
	if len(reps) != 2 {
		t.Fatalf("representantes = %d; esperava 2 (uma por classe)", len(reps))
	}
	// O PRIMEIRO de cada classe é o guardado: a página 1, não a 100.
	if reps[0].Pagina != 1 {
		t.Errorf("o representante guardado é da página %d; esperava a primeira", reps[0].Pagina)
	}

	resumos := ac.resumos()
	if len(resumos) != 1 {
		t.Fatalf("resumos = %d; esperava 1 camada", len(resumos))
	}
	if resumos[0].PorClasse[ClasseHifen] != 100 {
		t.Errorf("contagem = %d; o total tem de continuar sendo 100",
			resumos[0].PorClasse[ClasseHifen])
	}
}

// TestResumosSaemNaOrdemDoPipeline: o relatório é lido de cima para baixo, e a
// ordem das camadas é a ordem em que a causa se propaga.
func TestResumosSaemNaOrdemDoPipeline(t *testing.T) {
	ac := novoAcumulador()
	// Anotadas fora de ordem de propósito.
	ac.contar(CamadaEstados, "documentos", 1, 0)
	ac.contar(CamadaExtracao, "páginas", 1, 0)
	ac.contar(CamadaRecortes, "recortes", 1, 0)

	resumos := ac.resumos()
	esperada := []Camada{CamadaExtracao, CamadaRecortes, CamadaEstados}
	if len(resumos) != len(esperada) {
		t.Fatalf("resumos = %d; esperava %d", len(resumos), len(esperada))
	}
	for i, c := range esperada {
		if resumos[i].Camada != c {
			t.Errorf("posição %d = %q; esperava %q", i, resumos[i].Camada, c)
		}
	}
}
