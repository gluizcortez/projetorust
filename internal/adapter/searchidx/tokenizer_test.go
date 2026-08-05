package searchidx_test

import (
	"strings"
	"testing"

	"github.com/gluizcortez/projetorust/internal/adapter/searchidx"
)

// TestLimiteDeComprimentoEmBytes cobre a fronteira do descarte por
// comprimento — INV-P03 e INV-P04.
//
// O limite é 40 BYTES, e a regra é descartar comprimento >= 40. Confirmado na
// fonte do tantivy 0.22.1 (remove_long.rs:36, tokenizer_manager.rs:65).
func TestLimiteDeComprimentoEmBytes(t *testing.T) {
	const limite = searchidx.LimiteComprimentoTermo
	if limite != 40 {
		t.Fatalf("LimiteComprimentoTermo = %d, a fonte do tantivy 0.22.1 usa 40", limite)
	}

	casos := []struct {
		nome    string
		termo   string
		bytes   int
		runas   int
		mantido bool
	}{
		// --- ASCII: bytes e runas coincidem ---
		{"limite-1 ASCII", strings.Repeat("a", limite-1), 39, 39, true},
		{"limite ASCII", strings.Repeat("a", limite), 40, 40, false},
		{"limite+1 ASCII", strings.Repeat("a", limite+1), 41, 41, false},

		// --- multibyte: é aqui que bytes e runas divergem ---
		// 'α' ocupa 2 bytes. 19 runas = 38 bytes, mantido.
		{"19 runas / 38 bytes", strings.Repeat("α", 19), 38, 19, true},
		// 20 runas = 40 bytes: DESCARTADO, embora sejam só 20 runas.
		// Uma implementação que contasse runas o manteria.
		{"20 runas / 40 bytes", strings.Repeat("α", 20), 40, 20, false},
		{"39 runas / 78 bytes", strings.Repeat("α", 39), 78, 39, false},

		// 'あ' ocupa 3 bytes. 13 runas = 39 bytes, mantido.
		{"13 runas / 39 bytes", strings.Repeat("あ", 13), 39, 13, true},
		{"14 runas / 42 bytes", strings.Repeat("あ", 14), 42, 14, false},
	}

	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			if len(c.termo) != c.bytes {
				t.Fatalf("o caso está errado: %d bytes, esperado %d", len(c.termo), c.bytes)
			}
			if len([]rune(c.termo)) != c.runas {
				t.Fatalf("o caso está errado: %d runas, esperado %d", len([]rune(c.termo)), c.runas)
			}

			termos := searchidx.Tokenizar(c.termo)
			mantido := len(termos) == 1

			if mantido != c.mantido {
				t.Errorf("%d bytes / %d runas: mantido=%v, esperado %v — "+
					"a medida é em BYTES (INV-P04)", c.bytes, c.runas, mantido, c.mantido)
			}
		})
	}
}

// TestClassificacaoAlfanumerica cobre INV-P22.
func TestClassificacaoAlfanumerica(t *testing.T) {
	casos := []struct {
		nome     string
		entrada  string
		esperado []string
	}{
		{"expoente faz parte do termo", "m²", []string{"m²"}},
		{"fração faz parte do termo", "½kg", []string{"½kg"}},
		{"ordinal masculino é letra", "5º", []string{"5º"}},
		// Ⅷ é Nl: faz parte do termo (INV-P22) E tem minúscula, ⅷ.
		{"numeral de letra", "capítuloⅧ", []string{"capítuloⅷ"}},
		{"pontuação separa", "a.b,c", []string{"a", "b", "c"}},
		{"espaço separa", "a b", []string{"a", "b"}},
		{"quebra de linha separa", "a\nb", []string{"a", "b"}},
		{"hífen separa", "a-b", []string{"a", "b"}},
		{"minúsculas", "ABC", []string{"abc"}},
		{"vazio", "", nil},
		{"só separadores", "---", nil},
		{"acentos preservados", "ação", []string{"ação"}},
	}

	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			obtidos := searchidx.Tokenizar(c.entrada)
			if len(obtidos) != len(c.esperado) {
				t.Fatalf("%q -> %q, esperado %q", c.entrada, obtidos, c.esperado)
			}
			for i := range c.esperado {
				if obtidos[i] != c.esperado[i] {
					t.Errorf("%q -> termo %d = %q, esperado %q", c.entrada, i, obtidos[i], c.esperado[i])
				}
			}
		})
	}
}

func TestOrdemDosEstagios(t *testing.T) {
	// O descarte por comprimento acontece ANTES da conversão para minúsculas.
	// 'İ' (U+0130) ocupa 2 bytes e vira 3 bytes em minúsculas — um termo de
	// 39 bytes com ele passa no filtro e só depois cresce.
	termo := strings.Repeat("İ", 19) + "a" // 39 bytes antes de minúsculas
	if len(termo) != 39 {
		t.Fatalf("o caso está errado: %d bytes", len(termo))
	}
	termos := searchidx.Tokenizar(termo)
	if len(termos) != 1 {
		t.Fatalf("o termo de 39 bytes deveria passar no filtro: %q", termos)
	}
	if len(termos[0]) <= 39 {
		t.Logf("minúsculas não expandiu neste caso (%d bytes)", len(termos[0]))
	}
}

func BenchmarkTokenizar(b *testing.B) {
	// Texto representativo de uma página de diário, repetido até ~1 MiB.
	pagina := strings.Repeat(
		"PORTARIA No 1.024, DE 15 DE MARCO DE 2024 O SECRETARIO MUNICIPAL DE "+
			"ADMINISTRACAO, no uso de suas atribuicoes legais, resolve nomear "+
			"JOAO SILVA matricula 44521 e MARIA SOUZA matricula 44522. "+
			"Contratada: ALFA CONSTRUCOES LTDA, area de 350 m2.\n", 3000)
	b.SetBytes(int64(len(pagina)))
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		searchidx.Tokenizar(pagina)
	}
}
