package parity_test

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math/rand/v2"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/gluizcortez/projetorust/internal/adapter/pdftext"
	"github.com/gluizcortez/projetorust/internal/adapter/searchidx"
	"github.com/gluizcortez/projetorust/internal/domain"
)

// Teste de propriedade do LAÇO INTEIRO — fase F12.
//
// # O que ele mede que os outros não medem
//
// `TestPropriedadeNormalizacao` (F6) mede o pipeline de texto sobre um milhão
// de cadeias. `TestPropriedadeFiltroOperador` (F7) mede o filtro do operador
// `&`. Nenhum dos dois toca no LAÇO: ordenação por página, deduplicação por
// perfil, ordem de gravação e desfecho.
//
// É onde estão as invariantes mais sutis da migração — INV-P12, INV-P13,
// INV-P14 — e o corpus dourado tem 28 documentos, o que é pouco para exercitar
// combinações de perfil, página e expressão.
//
// # O gerador
//
// Combina as oito construções que o enunciado da fase lista, e as combina de
// verdade: um caso típico traz várias ao mesmo tempo, porque é a interação
// entre elas que o corpus não alcança.
//
//	acentuação                       INV-P07, INV-P19
//	hífen no fim da linha            INV-P02
//	expressões com `&`               INV-P01, INV-P23, D-06
//	termos longos                    INV-P03
//	quebra de linha no meio da frase INV-P05
//	caracteres não decomponíveis     INV-P07 (caso negativo)
//	páginas vazias                   INV-P06
//	sequências repetidas             deduplicação por página

const caminhoOraculoLaco = "../../tools/capturar-corpus/target/release/oraculo-laco"

// sepItem separa itens de uma lista no protocolo do oráculo. É a unidade de
// registro do ASCII, que não aparece em hexadecimal nem em identificador.
const sepItem = "\x1e"

// TotalDeCasosDoLaco é o mínimo exigido pelo critério de aceite da fase.
const totalDeCasosDoLaco = 10_000

// casoDoLaco é um documento sintético com suas chaves de pesquisa.
type casoDoLaco struct {
	// Paginas é o texto BRUTO — o oráculo e o Go normalizam cada um do seu
	// lado, e é justamente por isso que o teste também cobre a normalização
	// dentro da cadeia, e não só isolada.
	Paginas []string
	// Chaves vêm ORDENADAS por (id_perfil, expressão), que é a ordem normativa
	// do `ORDER BY` do legado e a que governa a deduplicação (INV-P12).
	Chaves []domain.ChavePesquisa
}

// TestPropriedadeDoLaco é o critério de aceite da fase F12: zero divergência de
// recortes em 10.000 casos gerados.
func TestPropriedadeDoLaco(t *testing.T) {
	if testing.Short() {
		t.Skip("teste de propriedade é demorado")
	}
	if _, err := os.Stat(caminhoOraculoLaco); err != nil {
		t.Skipf("oráculo indisponível (%v) — compile com "+
			"`cargo build --release --manifest-path tools/capturar-corpus/Cargo.toml "+
			"--bin oraculo-laco`", err)
	}

	// Semente fixa: uma falha precisa ser reproduzível.
	fonte := rand.New(rand.NewPCG(0xF12, 0x1AC0))

	casos := make([]casoDoLaco, totalDeCasosDoLaco)
	var entrada strings.Builder
	for i := range casos {
		casos[i] = gerarCasoDoLaco(fonte)
		entrada.WriteString(codificarCaso(casos[i]))
		entrada.WriteByte('\n')
	}

	respostas := consultarOraculoDoLaco(t, entrada.String())
	if len(respostas) != len(casos) {
		t.Fatalf("o oráculo devolveu %d respostas para %d casos", len(respostas), len(casos))
	}

	var divergencias, comparados, panicos, ignorados int

	for i, caso := range casos {
		esperado := respostas[i]

		// O legado ENTRA EM PÂNICO quando a expressão do filtro `&` não compila
		// (D-06). O porte em Go transforma isso em importação PRESA, que é o
		// mesmo efeito observável no banco — mas o oráculo não emite recortes
		// nesse caso, e comparar listas seria comparar com o vazio.
		// `TestPropriedadeFiltroOperador` já cobre esse caminho isoladamente.
		if esperado.desfecho == "panico" {
			panicos++
			continue
		}
		if esperado.desfecho == "entrada_invalida" {
			ignorados++
			continue
		}

		obtido := executarLacoEmGo(caso)
		comparados++

		if d := compararGravacoes(esperado.recortes, obtido); d != "" {
			divergencias++
			if divergencias <= 5 {
				t.Errorf("caso %d divergiu:\n%s\n%s", i, d, descreverCaso(caso))
			}
		}
	}

	if divergencias > 0 {
		t.Errorf("%d de %d casos divergiram", divergencias, comparados)
	}
	t.Logf("laço: %d casos comparados, %d idênticos; %d pânicos do legado (D-06) e %d ignorados",
		comparados, comparados-divergencias, panicos, ignorados)

	// Um gerador que nunca produza os casos interessantes passaria com folga e
	// não mediria nada. Os pânicos são a evidência de que expressões com `&`
	// inválidas estão sendo geradas de fato.
	if comparados < totalDeCasosDoLaco/2 {
		t.Errorf("só %d de %d casos foram comparados; o gerador está degenerado",
			comparados, totalDeCasosDoLaco)
	}
}

// -------------------------------------------------------------------------
// O gerador
// -------------------------------------------------------------------------

// vocabulario são os termos que aparecem nas páginas e nas expressões.
//
// Alguns são acentuados de propósito: depois da normalização eles perdem o
// acento, e uma expressão acentuada deixa de casar — é INV-P19, o defeito
// preservado que torna perfis com acento inertes.
var vocabulario = []string{
	"joao", "silva", "maria", "souza", "alfa", "beta", "gama",
	"construcoes", "servicos", "contrato", "portaria", "extrato",
	"conceição", "josé", "münchen", "kierkegaard", "ærø", "strasse",
}

// termoLongo excede o limite do analisador léxico do legado (INV-P03).
const termoLongo = "BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB"

// naoDecomponiveis não têm forma decomposta e SOBREVIVEM à remoção de
// diacríticos (INV-P07, caso negativo).
var naoDecomponiveis = []rune{'ø', 'æ', 'đ', 'ł', 'ß', 'þ'}

func gerarCasoDoLaco(f *rand.Rand) casoDoLaco {
	nPaginas := 1 + f.IntN(4)
	paginas := make([]string, nPaginas)
	for i := range paginas {
		paginas[i] = gerarPagina(f)
	}

	nChaves := 1 + f.IntN(5)
	chaves := make([]domain.ChavePesquisa, 0, nChaves)
	for range nChaves {
		chaves = append(chaves, domain.ChavePesquisa{
			// Poucos perfis, de propósito: é a REPETIÇÃO de perfil entre chaves
			// consecutivas que exercita a deduplicação (INV-P12), e o perfil
			// ZERO que exercita o sentinela (INV-P13).
			IDPerfil:  int64(f.IntN(4)),
			Expressao: gerarExpressao(f),
		})
	}

	// A ordem normativa do `ORDER BY tp.id_perfil, tpv.expressao_nm`.
	sort.SliceStable(chaves, func(i, j int) bool {
		if chaves[i].IDPerfil != chaves[j].IDPerfil {
			return chaves[i].IDPerfil < chaves[j].IDPerfil
		}
		return chaves[i].Expressao < chaves[j].Expressao
	})

	return casoDoLaco{Paginas: paginas, Chaves: chaves}
}

func gerarPagina(f *rand.Rand) string {
	// Página VAZIA: exercita o índice sem termos (INV-P06).
	if f.IntN(12) == 0 {
		return ""
	}

	var b strings.Builder
	linhas := 1 + f.IntN(6)
	for range linhas {
		b.WriteString(gerarLinha(f))
		b.WriteByte('\n')
	}
	return b.String()
}

func gerarLinha(f *rand.Rand) string {
	var b strings.Builder
	palavras := 1 + f.IntN(6)

	for i := range palavras {
		if i > 0 {
			b.WriteByte(' ')
		}
		switch f.IntN(10) {
		case 0:
			// Termo longo, acima do limite do analisador (INV-P03).
			b.WriteString(termoLongo)
		case 1:
			// Caractere não decomponível colado a um termo.
			b.WriteString(vocabulario[f.IntN(len(vocabulario))])
			b.WriteRune(naoDecomponiveis[f.IntN(len(naoDecomponiveis))])
		case 2:
			// Sequência repetida: a mesma palavra várias vezes na linha, o que
			// faz a busca de frase encontrar a página mais de uma vez — e o
			// legado grava UM recorte por página, não um por ocorrência.
			p := vocabulario[f.IntN(len(vocabulario))]
			b.WriteString(strings.Repeat(p+" ", 2+f.IntN(3)))
		default:
			b.WriteString(vocabulario[f.IntN(len(vocabulario))])
		}
	}

	// Hífen no FIM da linha: a normalização rejunta com a linha seguinte
	// (INV-P02) — e não rejunta quando o caractere anterior não é de palavra,
	// que é o caso negativo.
	if f.IntN(6) == 0 {
		b.WriteByte('-')
	}

	// A linha sai APARADA. O caso gerado representa texto que já saiu da
	// extração, e a extração do legado apara cada linha antes de acrescentar o
	// `\n` (main.rs:494-497). Deixar espaço nas bordas aqui mediria a aparagem
	// em vez do laço — e a aparagem já tem o teste de paridade da fase F5.
	return strings.TrimSpace(b.String())
}

func gerarExpressao(f *rand.Rand) string {
	n := 1 + f.IntN(3)
	termos := make([]string, 0, n+1)
	for range n {
		termos = append(termos, vocabulario[f.IntN(len(vocabulario))])
	}
	expressao := strings.Join(termos, " ")

	switch f.IntN(8) {
	case 0:
		// Filtro do operador `&` com padrão VÁLIDO.
		return expressao + " & " + vocabulario[f.IntN(len(vocabulario))]
	case 1:
		// Filtro com padrão INVÁLIDO: no legado é `.unwrap()` e PÂNICO (D-06).
		return expressao + " & ["
	case 2:
		// Frase que atravessa quebra de linha (INV-P05).
		return expressao + " " + vocabulario[f.IntN(len(vocabulario))]
	case 3:
		return strings.ToUpper(expressao)
	case 4:
		// Termo longo NO MEIO da expressão. O analisador o descarta e deixa um
		// BURACO na numeração da consulta, que passa a exigir a mesma distância
		// no documento. É o caso que o corpus dourado não alcança e que expôs a
		// divergência de posição da fase F12.
		return expressao + " " + termoLongo + " " + vocabulario[f.IntN(len(vocabulario))]
	default:
		return expressao
	}
}

// -------------------------------------------------------------------------
// Execução em Go
// -------------------------------------------------------------------------

// gravacao é uma linha na ordem de gravação, no formato comparável.
type gravacao struct {
	IDPerfil    int64
	Expressao   string
	NrPagina    int64
	TextoSHA256 string
}

// executarLacoEmGo reproduz o laço com os componentes de PRODUÇÃO.
//
// Não usa o `usecase.Pipeline` porque este teste parte de TEXTO, não de PDF: o
// pipeline começa na extração. O que se compara aqui é o laço — ordenação,
// deduplicação e ordem de gravação —, e ele está reproduzido abaixo com a mesma
// estrutura do porte, sobre o índice e a normalização reais.
func executarLacoEmGo(caso casoDoLaco) []gravacao {
	paginas := make([]string, len(caso.Paginas))
	for i, bruta := range caso.Paginas {
		paginas[i] = pdftext.Normalizar(bruta)
	}

	indice, err := searchidx.NovoIndexador().Construir(context.Background(), paginas)
	if err != nil {
		return nil
	}
	defer func() { _ = indice.Fechar() }()

	var (
		gravadas       []gravacao
		vistas         = map[uint64]struct{}{}
		perfilCorrente int64
		primeira       = true
	)

	for _, chave := range caso.Chaves {
		// INV-P17: expressão com aspas duplas derruba a consulta do legado. O
		// gerador não as produz, mas a guarda mantém o laço fiel.
		if strings.ContainsRune(chave.Expressao, '"') {
			return gravadas
		}

		recortes, err := indice.Frase(context.Background(), chave.Expressao)
		if err != nil {
			// Expressão inválida do filtro `&`: no legado é pânico, e o caso já
			// foi descartado pela resposta do oráculo.
			return gravadas
		}

		sort.SliceStable(recortes, func(i, j int) bool {
			return recortes[i].Pagina < recortes[j].Pagina
		})

		if primeira || perfilCorrente != chave.IDPerfil {
			vistas = map[uint64]struct{}{}
			perfilCorrente = chave.IDPerfil
			primeira = false
		}

		for _, r := range recortes {
			if _, jaVista := vistas[r.Pagina]; !jaVista {
				soma := sha256.Sum256([]byte(r.Destaque))
				gravadas = append(gravadas, gravacao{
					IDPerfil:    chave.IDPerfil,
					Expressao:   chave.Expressao,
					NrPagina:    int64(r.Pagina), //nolint:gosec // páginas geradas cabem folgadamente
					TextoSHA256: hex.EncodeToString(soma[:]),
				})
			}
			// Inserção INCONDICIONAL, como main.rs:303.
			vistas[r.Pagina] = struct{}{}
		}
	}
	return gravadas
}

// -------------------------------------------------------------------------
// Protocolo do oráculo
// -------------------------------------------------------------------------

func codificarCaso(c casoDoLaco) string {
	paginas := make([]string, len(c.Paginas))
	for i, p := range c.Paginas {
		paginas[i] = hex.EncodeToString([]byte(p))
	}
	chaves := make([]string, len(c.Chaves))
	for i, k := range c.Chaves {
		chaves[i] = strconv.FormatInt(k.IDPerfil, 10) + ":" + hex.EncodeToString([]byte(k.Expressao))
	}
	return strings.Join(paginas, sepItem) + "\t" + strings.Join(chaves, sepItem)
}

type respostaDoOraculo struct {
	desfecho string
	recortes []gravacao
}

func consultarOraculoDoLaco(t *testing.T, entrada string) []respostaDoOraculo {
	t.Helper()

	cmd := exec.Command(caminhoOraculoLaco)
	cmd.Stdin = strings.NewReader(entrada)
	saida, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("abrindo a saída do oráculo: %v", err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatalf("iniciando o oráculo: %v", err)
	}

	var respostas []respostaDoOraculo
	leitor := bufio.NewScanner(saida)
	// Uma resposta pode ser longa: várias gravações, cada uma com um resumo de
	// 64 caracteres.
	leitor.Buffer(make([]byte, 0, 1<<20), 1<<24)
	for leitor.Scan() {
		respostas = append(respostas, analisarResposta(t, leitor.Text()))
	}
	if err := leitor.Err(); err != nil {
		t.Fatalf("lendo a saída do oráculo: %v", err)
	}
	if err := cmd.Wait(); err != nil {
		t.Fatalf("o oráculo terminou com erro: %v", err)
	}
	return respostas
}

func analisarResposta(t *testing.T, linha string) respostaDoOraculo {
	t.Helper()

	desfecho, resto, achou := strings.Cut(linha, "\t")
	if !achou {
		t.Fatalf("resposta sem separador: %q", linha)
	}

	r := respostaDoOraculo{desfecho: desfecho}
	if resto == "" {
		return r
	}

	for _, campo := range strings.Split(resto, sepItem) {
		partes := strings.Split(campo, ":")
		if len(partes) != 4 {
			t.Fatalf("gravação malformada: %q", campo)
		}
		idPerfil, err := strconv.ParseInt(partes[0], 10, 64)
		if err != nil {
			t.Fatalf("id_perfil inválido em %q: %v", campo, err)
		}
		expressao, err := hex.DecodeString(partes[1])
		if err != nil {
			t.Fatalf("expressão inválida em %q: %v", campo, err)
		}
		nrPagina, err := strconv.ParseInt(partes[2], 10, 64)
		if err != nil {
			t.Fatalf("nr_pagina inválida em %q: %v", campo, err)
		}
		r.recortes = append(r.recortes, gravacao{
			IDPerfil:    idPerfil,
			Expressao:   string(expressao),
			NrPagina:    nrPagina,
			TextoSHA256: partes[3],
		})
	}
	return r
}

// -------------------------------------------------------------------------
// Comparação e diagnóstico
// -------------------------------------------------------------------------

// compararGravacoes devolve a descrição da primeira divergência, ou vazio.
//
// A ORDEM é comparada junto com o conteúdo: dois conjuntos iguais gravados em
// ordens diferentes produzem identificadores diferentes em `tb_recorte`.
func compararGravacoes(esperadas, obtidas []gravacao) string {
	if len(esperadas) != len(obtidas) {
		return fmt.Sprintf("  gravações: esperadas %d, obtidas %d\n%s",
			len(esperadas), len(obtidas), listar(esperadas, obtidas))
	}
	for i := range esperadas {
		if esperadas[i] != obtidas[i] {
			return fmt.Sprintf("  gravação %d:\n    esperada: %+v\n    obtida:   %+v",
				i, esperadas[i], obtidas[i])
		}
	}
	return ""
}

func listar(esperadas, obtidas []gravacao) string {
	var b strings.Builder
	b.WriteString("    esperadas:\n")
	for _, g := range esperadas {
		fmt.Fprintf(&b, "      perfil %d, %q, página %d\n", g.IDPerfil, g.Expressao, g.NrPagina)
	}
	b.WriteString("    obtidas:\n")
	for _, g := range obtidas {
		fmt.Fprintf(&b, "      perfil %d, %q, página %d\n", g.IDPerfil, g.Expressao, g.NrPagina)
	}
	return b.String()
}

// descreverCaso imprime o caso em forma reproduzível.
func descreverCaso(c casoDoLaco) string {
	var b strings.Builder
	b.WriteString("  caso:\n")
	for i, p := range c.Paginas {
		fmt.Fprintf(&b, "    página %d: %q\n", i+1, p)
	}
	for _, k := range c.Chaves {
		fmt.Fprintf(&b, "    chave: perfil %d, %q\n", k.IDPerfil, k.Expressao)
	}
	return b.String()
}
