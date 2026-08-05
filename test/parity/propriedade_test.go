package parity_test

import (
	"bufio"
	"encoding/hex"
	"math/rand/v2"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/gluizcortez/projetorust/internal/adapter/pdftext"
	"github.com/gluizcortez/projetorust/internal/adapter/searchidx"
)

const caminhoOraculo = "../../tools/capturar-corpus/target/release/oraculo-normalizacao"

// separadorDeTermos é o mesmo que o oráculo usa: unidade de separação do ASCII,
// escolhido por não aparecer em nenhum termo.
const separadorDeTermos = "\x1f"

// TestPropriedadeNormalizacao compara a implementação Go com o pipeline do
// legado sobre um milhão de cadeias geradas aleatoriamente.
//
// É o critério de aceite da fase F6. A geração é enviesada de propósito para
// as construções que o corpus não cobre em volume: hífens no fim da linha,
// letras acentuadas, caracteres não decomponíveis, expoentes e frações,
// sequências longas e marcas combinantes soltas.
func TestPropriedadeNormalizacao(t *testing.T) {
	if testing.Short() {
		t.Skip("teste de propriedade é demorado")
	}
	if _, err := os.Stat(caminhoOraculo); err != nil {
		t.Skipf("oráculo indisponível (%v) — compile com "+
			"`cargo build --release --manifest-path tools/capturar-corpus/Cargo.toml "+
			"--bin oraculo-normalizacao`", err)
	}

	const total = 1_000_000
	// Semente fixa: uma falha precisa ser reproduzível.
	fonte := rand.New(rand.NewPCG(0x5EED, 0xF6))

	casos := make([]string, total)
	var entrada strings.Builder
	entrada.Grow(total * 48)
	for i := range total {
		casos[i] = cadeiaAleatoria(fonte)
		entrada.WriteString(hex.EncodeToString([]byte(casos[i])))
		entrada.WriteByte('\n')
	}

	cmd := exec.Command(caminhoOraculo)
	cmd.Stdin = strings.NewReader(entrada.String())
	saida, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("abrindo saída do oráculo: %v", err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatalf("iniciando o oráculo: %v", err)
	}

	sc := bufio.NewScanner(saida)
	sc.Buffer(make([]byte, 0, 1<<16), 1<<22)

	var (
		lidos              int
		divergenciasNorm   int
		divergenciasTermos int
	)

	for sc.Scan() {
		if lidos >= total {
			t.Fatalf("o oráculo devolveu mais linhas que o esperado")
		}
		entradaOriginal := casos[lidos]
		lidos++

		campos := strings.SplitN(sc.Text(), "\t", 2)
		if len(campos) != 2 {
			t.Fatalf("linha malformada do oráculo: %q", sc.Text())
		}
		bytesEsperados, err := hex.DecodeString(campos[0])
		if err != nil {
			t.Fatalf("hexadecimal inválido do oráculo: %v", err)
		}
		normEsperada := string(bytesEsperados)

		var termosEsperados []string
		if campos[1] != "" {
			termosEsperados = strings.Split(campos[1], separadorDeTermos)
		}

		normObtida := pdftext.Normalizar(entradaOriginal)
		if normObtida != normEsperada {
			divergenciasNorm++
			if divergenciasNorm <= 5 {
				t.Errorf("NORMALIZAÇÃO diverge\n  entrada : %q\n  obtida  : %q\n  legado  : %q",
					entradaOriginal, normObtida, normEsperada)
			}
			continue
		}

		termosObtidos := searchidx.Tokenizar(normObtida)
		if !mesmosTermos(termosObtidos, termosEsperados) {
			divergenciasTermos++
			if divergenciasTermos <= 5 {
				t.Errorf("TOKENIZAÇÃO diverge\n  entrada : %q\n  normal. : %q\n  obtidos : %q\n  legado  : %q",
					entradaOriginal, normObtida, termosObtidos, termosEsperados)
			}
		}
	}

	if err := sc.Err(); err != nil {
		t.Fatalf("lendo a saída do oráculo: %v", err)
	}
	if err := cmd.Wait(); err != nil {
		t.Fatalf("oráculo encerrou com erro: %v", err)
	}
	if lidos != total {
		t.Fatalf("o oráculo devolveu %d linhas, esperava %d", lidos, total)
	}

	if divergenciasNorm > 0 || divergenciasTermos > 0 {
		t.Errorf("total: %d divergências de normalização, %d de tokenização, em %d casos",
			divergenciasNorm, divergenciasTermos, total)
		return
	}
	t.Logf("%d casos aleatórios sem divergência de normalização nem de tokenização", total)
}

func mesmosTermos(a, b []string) bool {
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

// alfabetos são os blocos de caracteres que a geração combina. Cada um existe
// por causa de uma invariante que o corpus não exercita em volume.
var alfabetos = []([]rune){
	[]rune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"), // base
	[]rune("0123456789"), // dígitos
	[]rune("áéíóúàâêôãõçñüÁÉÍÓÚÂÊÔÃÕÇÑÜ"), // INV-P07: decomponíveis
	[]rune("øđßæłħŧĸŋŒœÐÞ"),               // INV-P07: não decomponíveis
	[]rune("²³¹¼½¾ⅧⅣ①"),                   // INV-P22: No e Nl
	[]rune("-\n \t.,;:!?()[]{}\"'&/@#"),   // separadores e o operador &
	[]rune("̧́̃̈"),                        // marcas combinantes soltas
	[]rune("αβγδεζηθ"),                    // sobrevivem à normalização
	[]rune("\u00a0\u200b\u3000"),          // espaços especiais: NBSP, largura zero, ideográfico
}

// cadeiaAleatoria monta uma cadeia curta combinando os alfabetos, com viés
// para as construções críticas.
func cadeiaAleatoria(fonte *rand.Rand) string {
	var b strings.Builder
	comprimento := 1 + fonte.IntN(40)

	for range comprimento {
		// 12% de chance de inserir um hífen no fim de linha, que é o gatilho
		// da junção (INV-P02) e é raro numa distribuição uniforme.
		if fonte.IntN(100) < 12 {
			b.WriteString("-\n")
			continue
		}
		alfabeto := alfabetos[fonte.IntN(len(alfabetos))]
		b.WriteRune(alfabeto[fonte.IntN(len(alfabeto))])
	}

	// 8% de chance de acrescentar uma sequência longa, que exercita o descarte
	// por comprimento (INV-P03, INV-P04).
	if fonte.IntN(100) < 8 {
		alfabeto := alfabetos[fonte.IntN(len(alfabetos))]
		repeticoes := 30 + fonte.IntN(20)
		for range repeticoes {
			b.WriteRune(alfabeto[fonte.IntN(len(alfabeto))])
		}
	}

	return b.String()
}
