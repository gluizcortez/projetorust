package searchidx

import (
	"errors"
	"regexp"
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"

	"github.com/gluizcortez/projetorust/internal/domain"
)

// brancosDoRust são as 25 runas que o modo `x` do crate `regex` descarta, e que
// o `\s` do mesmo crate aceita. Os dois conjuntos foram medidos
// INDEPENDENTEMENTE, varrendo todo o Unicode com
// `tools/capturar-corpus/src/bin/sonda-extended.rs`, e coincidem.
//
// A lista está aqui transcrita à mão de propósito: é o oráculo contra o qual
// `unicode.IsSpace` e a classe de espaço são verificados. Derivá-la das mesmas
// funções que ela verifica não provaria nada.
var brancosDoRust = []rune{
	0x0009, 0x000A, 0x000B, 0x000C, 0x000D, 0x0020, 0x0085, 0x00A0,
	0x1680, 0x2000, 0x2001, 0x2002, 0x2003, 0x2004, 0x2005, 0x2006,
	0x2007, 0x2008, 0x2009, 0x200A, 0x2028, 0x2029, 0x202F, 0x205F,
	0x3000,
}

// TestIsSpaceCoincideComOModoExtended percorre TODO o Unicode e exige que
// `unicode.IsSpace` do Go decida exatamente como o modo `x` do Rust.
//
// É a fundação de StripExtended: se este teste cair, a função descarta runas a
// mais ou a menos e INV-P01 vai junto.
func TestIsSpaceCoincideComOModoExtended(t *testing.T) {
	esperado := make(map[rune]bool, len(brancosDoRust))
	for _, r := range brancosDoRust {
		esperado[r] = true
	}

	var divergencias int
	for r := rune(0); r <= unicode.MaxRune; r++ {
		if unicode.IsSpace(r) != esperado[r] {
			divergencias++
			if divergencias <= 10 {
				t.Errorf("U+%04X: unicode.IsSpace = %t, o modo x do Rust = %t",
					r, unicode.IsSpace(r), esperado[r])
			}
		}
	}
	if divergencias > 0 {
		t.Errorf("total de %d runas divergentes", divergencias)
	}
}

// TestClasseDeEspacoCoincideComOBarraSDoRust exige que a classe injetada no
// lugar de `\s` aceite exatamente as mesmas runas que o `\s` do Rust — nem uma
// a mais, nem uma a menos.
//
// O teste também demonstra por que a tradução é necessária: o `\s` NATIVO do
// RE2 falha nas mesmas runas.
func TestClasseDeEspacoCoincideComOBarraSDoRust(t *testing.T) {
	esperado := make(map[rune]bool, len(brancosDoRust))
	for _, r := range brancosDoRust {
		esperado[r] = true
	}

	traduzida := regexp.MustCompile("^" + classeDeEspacoDoRust + "$")
	nativa := regexp.MustCompile(`^\s$`)

	var divergencias, divergenciasNativa int
	for r := rune(0); r <= unicode.MaxRune; r++ {
		if !utf8.ValidRune(r) {
			continue // substitutos não têm representação em UTF-8
		}
		s := string(r)
		if traduzida.MatchString(s) != esperado[r] {
			divergencias++
			if divergencias <= 10 {
				t.Errorf("U+%04X: a classe traduzida = %t, o \\s do Rust = %t",
					r, traduzida.MatchString(s), esperado[r])
			}
		}
		if nativa.MatchString(s) != esperado[r] {
			divergenciasNativa++
		}
	}
	if divergencias > 0 {
		t.Errorf("total de %d runas divergentes", divergencias)
	}

	// A prova de que a tradução não é decorativa.
	if divergenciasNativa == 0 {
		t.Error("o \\s nativo do RE2 concordou com o do Rust — a premissa da tradução caiu")
	}
	t.Logf("o \\s nativo do RE2 diverge do \\s do Rust em %d runas; a classe traduzida, em 0",
		divergenciasNativa)
}

// TestStripExtendedTabela é a tabela entrada→saída exigida pelo procedimento da
// fase F7, com cada linha rotulada pela regra que exercita.
//
// Roda com -v para ver a tabela impressa.
func TestStripExtendedTabela(t *testing.T) {
	casos := []struct {
		regra    string
		entrada  string
		esperado string
	}{
		// Regra 1 — espaço em branco descartado.
		{"espaço simples", "ACME FILHOS", "ACMEFILHOS"},
		{"espaços múltiplos", "ACME   FILHOS", "ACMEFILHOS"},
		{"espaços nas bordas", "  ACME  ", "ACME"},
		{"tabulação", "ACME\tFILHOS", "ACMEFILHOS"},
		{"nova linha", "ACME\nFILHOS", "ACMEFILHOS"},
		{"retorno de carro", "ACME\rFILHOS", "ACMEFILHOS"},
		{"tabulação vertical", "ACME\vFILHOS", "ACMEFILHOS"},
		{"avanço de página", "ACME\fFILHOS", "ACMEFILHOS"},
		{"espaço não quebrável", "ACME FILHOS", "ACMEFILHOS"},
		{"espaço ideográfico", "ACME　FILHOS", "ACMEFILHOS"},
		{"espaço OGHAM", "ACME FILHOS", "ACMEFILHOS"},
		{"NEL", "ACME\u0085FILHOS", "ACMEFILHOS"},
		{"largura zero NÃO é branco", "ACME\u200bFILHOS", "ACME\u200bFILHOS"},

		// Regra 1 dentro de classe de caracteres — a correção da especificação.
		{"espaço dentro de [a b]", "[a b]", "[ab]"},
		{"espaço dentro de classe negada", "[^a b]", "[^ab]"},

		// Regra 2 — comentários.
		{"comentário até a nova linha", "AB # comentário\nCD", "ABCD"},
		{"comentário até o fim", "AB # comentário", "AB"},
		{"comentário no início", "# só comentário\nAB", "AB"},
		{"comentário NÃO termina em \\r", "A#x\rB", "A"},
		{"comentário engole o ] da classe", "[a # b]", "[a"},

		// Regra 3 — escapes.
		{"espaço escapado sobrevive", `ACME\ FILHOS`, `ACME\ FILHOS`},
		{"escapado e depois solto", `A\  B`, `A\ B`},
		{"cerquilha escapada não comenta", `AB\#CD`, `AB\#CD`},
		{"barra dupla libera a cerquilha", "AB\\\\#CD\nEF", `AB\\EF`},
		{"espaço escapado dentro de classe", `[a\ b]`, `[a\ b]`},
		{"barra sozinha no fim", `AB\`, `AB\`},

		// Nada a fazer — o caminho rápido.
		{"sem espaço nem cerquilha", `ACME\s*&\s*FILHOS`, `ACME\s*&\s*FILHOS`},
		{"vazio", "", ""},

		// O caso real, e a razão de tudo isto existir.
		{"a expressão de produção", `ACME \s*&\s* FILHOS`, `ACME\s*&\s*FILHOS`},
	}

	t.Log("| regra | entrada | saída |")
	t.Log("|---|---|---|")
	for _, c := range casos {
		t.Run(c.regra, func(t *testing.T) {
			obtido := StripExtended(c.entrada)
			if obtido != c.esperado {
				t.Errorf("StripExtended(%q)\n  obtido   = %q\n  esperado = %q",
					c.entrada, obtido, c.esperado)
			}
		})
		t.Logf("| %s | %q | %q |", c.regra, c.entrada, StripExtended(c.entrada))
	}
}

// TestStripExtendedEIdempotente: cortar duas vezes é o mesmo que cortar uma.
//
// Não é uma propriedade exigida pelo legado — é uma sanidade. Se falhar, a
// função está reintroduzindo o que remove, ou consumindo escapes que deveria
// preservar.
func TestStripExtendedEIdempotente(t *testing.T) {
	for _, entrada := range []string{
		"ACME & FILHOS", `A\ B # c`, "[a b]", `AB\\#CD` + "\nEF", `x\`,
		"  ", "#", `\#`, "a#b\nc",
	} {
		uma := StripExtended(entrada)
		duas := StripExtended(uma)
		if uma != duas {
			t.Errorf("não idempotente para %q: %q → %q", entrada, uma, duas)
		}
	}
}

// TestTraduzirClassesPerl cobre a segunda tradução: as classes Perl que o RE2
// define de forma mais estreita que o Rust.
func TestTraduzirClassesPerl(t *testing.T) {
	casos := []struct {
		nome     string
		entrada  string
		esperado string
	}{
		{"sem barra alguma", "ACME&FILHOS", "ACME&FILHOS"},
		{"\\s fora de classe", `A\sB`, "A" + classeDeEspacoDoRust + "B"},
		{"\\s dentro de classe entra sem colchetes", `[a\sb]`, `[a` + intervalosDeEspacoDoRust + `b]`},
		{"\\d vira \\p{Nd}", `A\dB`, `A\p{Nd}B`},
		{"\\d dentro de classe", `[\d]`, `[\p{Nd}]`},
		{"barra dupla protege o s", `A\\sB`, `A\\sB`},
		{"\\S vira o complemento", `A\SB`, "A" + classeDeNaoEspacoDoRust + "B"},
		{"\\S dentro de classe", `[\S]`, `[` + intervalosNaoEspacoDoRust + `]`},
		{"\\D vira \\P{Nd}", `A\DB`, `A\P{Nd}B`},
		{"classe fechada volta ao modo fora", `[a\sb]\s`, `[a` + intervalosDeEspacoDoRust + `b]` + classeDeEspacoDoRust},
		{"barra incompleta no fim", `AB\`, `AB\`},
		{"colchete escapado não abre classe", `\[\s`, `\[` + classeDeEspacoDoRust},
	}

	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			obtido, err := traduzirClassesPerl(c.entrada)
			if err != nil {
				t.Fatalf("traduzirClassesPerl(%q): %v", c.entrada, err)
			}
			if obtido != c.esperado {
				t.Errorf("traduzirClassesPerl(%q)\n  obtido   = %q\n  esperado = %q",
					c.entrada, obtido, c.esperado)
			}
		})
	}
}

// TestTraduzirClassesPerlRecusaOInexprimivel fixa a escolha de FALHAR ALTO em
// vez de aproximar: `\w`, `\W`, `\b` e `\B` do crate `regex` não têm forma
// exata em RE2, e uma aproximação gravaria recorte errado em silêncio.
//
// Ver docs/DECISOES-ABERTAS.md, D-19.
func TestTraduzirClassesPerlRecusaOInexprimivel(t *testing.T) {
	for _, entrada := range []string{`A\wB`, `A\WB`, `A\bB`, `A\BB`, `[a\wb]`} {
		if _, err := traduzirClassesPerl(entrada); err == nil {
			t.Errorf("traduzirClassesPerl(%q) aceitou; esperava recusa", entrada)
		}
	}

	// Classe ANINHADA: o crate `regex` lê união de conjuntos, o RE2 lê `[`
	// literal. Os dois compilam e produzem autômatos diferentes — foi a última
	// divergência silenciosa que o teste de propriedade encontrou.
	for _, entrada := range []string{`[a[bc]]`, `[ \S[a b]CIA,]`, `[[:alpha:]]`} {
		if _, err := traduzirClassesPerl(entrada); err == nil {
			t.Errorf("traduzirClassesPerl(%q) aceitou classe aninhada; esperava recusa", entrada)
		}
	}
	// Mas um `[` ESCAPADO dentro da classe é literal nos dois motores.
	if _, err := traduzirClassesPerl(`[a\[b]`); err != nil {
		t.Errorf(`traduzirClassesPerl("[a\\[b]") recusou: %v`, err)
	}
	// E a barra dupla NÃO dispara a recusa: `\\w` é barra literal seguida de w.
	if _, err := traduzirClassesPerl(`A\\wB`); err != nil {
		t.Errorf(`traduzirClassesPerl("A\\\\wB") recusou: %v`, err)
	}

	if _, err := CompilarFiltro(`ACME & \wFILHOS`); !errors.Is(err, domain.ErrExpressaoInvalida) {
		t.Errorf("CompilarFiltro devolveu %v; esperava ErrExpressaoInvalida", err)
	}
}

// TestClasseDeNaoEspacoEOComplemento verifica, runa a runa em todo o Unicode,
// que a classe escrita à mão é exatamente o complemento da classe de espaço.
//
// Onze intervalos com bordas adjacentes a runas isoladas não são coisa para
// conferir na leitura.
func TestClasseDeNaoEspacoEOComplemento(t *testing.T) {
	espaco := regexp.MustCompile("^" + classeDeEspacoDoRust + "$")
	naoEspaco := regexp.MustCompile("^" + classeDeNaoEspacoDoRust + "$")

	var divergencias int
	for r := rune(0); r <= unicode.MaxRune; r++ {
		if !utf8.ValidRune(r) {
			continue
		}
		s := string(r)
		if naoEspaco.MatchString(s) == espaco.MatchString(s) {
			divergencias++
			if divergencias <= 10 {
				t.Errorf("U+%04X: espaço=%t e não-espaço=%t deveriam ser opostos",
					r, espaco.MatchString(s), naoEspaco.MatchString(s))
			}
		}
	}
	if divergencias > 0 {
		t.Errorf("total de %d runas em que as duas classes não se complementam", divergencias)
	}
}

// TestCompilarFiltroINVP01 é o caso nomeado de INV-P01: os três espaçamentos do
// operador `&` que o legado casa, e a demonstração de que a tradução ingênua
// não casaria.
func TestCompilarFiltroINVP01(t *testing.T) {
	const expressao = "ACME & FILHOS"

	re, err := CompilarFiltro(expressao)
	if err != nil {
		t.Fatalf("CompilarFiltro(%q): %v", expressao, err)
	}

	deveCasar := []string{
		"Contratada: ACME & FILHOS LTDA",
		"Contratada: ACME&FILHOS LTDA",
		"Contratada: ACME   &   FILHOS LTDA",
		"contratada: acme & filhos ltda",          // a flag i
		"ACME & FILHOS",                           // espaço não quebrável: o \s do Rust aceita
		"ACME\v&\vFILHOS",                         // tabulação vertical: idem
		"ACME　&　FILHOS",                           // espaço ideográfico: idem
		"linha anterior\nACME & FILHOS\nseguinte", // a flag m
	}
	for _, texto := range deveCasar {
		if !re.MatchString(texto) {
			t.Errorf("deveria casar, e não casou: %q\n  padrão = %q", texto, re.String())
		}
	}

	naoDeveCasar := []string{
		"ACME FILHOS",
		"ACME e FILHOS",
		"ACME & IRMAOS",
		"ACME &\u200bFILHOS", // largura zero não é branco para o Rust
	}
	for _, texto := range naoDeveCasar {
		if re.MatchString(texto) {
			t.Errorf("não deveria casar, e casou: %q\n  padrão = %q", texto, re.String())
		}
	}
}

// TestFiltroIngenuoFalharia é o teste que o procedimento da fase pede: aquele
// que DEIXA DE PASSAR se StripExtended for removido.
//
// Ele reconstrói a tradução ingênua — substituir `&` e compilar sem consumir a
// flag `x` — e exige que ela erre exatamente onde se espera que erre. Se um dia
// StripExtended virar identidade, o filtro real passa a se comportar como o
// ingênuo e o teste de cima cai; este aqui documenta por quê.
func TestFiltroIngenuoFalharia(t *testing.T) {
	const expressao = "ACME & FILHOS"

	traduzido, err := traduzirClassesPerl(strings.ReplaceAll(expressao, "&", substituicaoDoOperador))
	if err != nil {
		t.Fatalf("traduzirClassesPerl: %v", err)
	}
	ingenuo := regexp.MustCompile("(?im)" + traduzido)

	// Sem o corte, os espaços literais ao redor do `&` continuam EXIGIDOS: o
	// padrão vira `ACME`+espaço+`\s*&\s*`+espaço+`FILHOS`. O texto sem espaço
	// algum ao redor do operador deixa de casar.
	naoCasariam := []string{
		"Contratada: ACME&FILHOS LTDA",
		"Contratada: ACME &FILHOS LTDA",
		"Contratada: ACME& FILHOS LTDA",
	}
	for _, texto := range naoCasariam {
		if ingenuo.MatchString(texto) {
			t.Errorf("a tradução ingênua casou %q — a premissa de INV-P01 caiu", texto)
		}
	}

	// O defeito é difícil de perceber sem oráculo justamente porque a tradução
	// ingênua ACERTA sempre que o texto tem pelo menos um espaço de cada lado:
	// o espaço literal consome um e o `\s*` consome o resto.
	for _, texto := range []string{
		"Contratada: ACME & FILHOS LTDA",
		"Contratada: ACME   &   FILHOS LTDA",
	} {
		if !ingenuo.MatchString(texto) {
			t.Errorf("a tradução ingênua não casou %q — o teste não mede o que pretende", texto)
		}
	}

	// E o filtro real casa os cinco.
	for _, texto := range append(naoCasariam,
		"Contratada: ACME & FILHOS LTDA",
		"Contratada: ACME   &   FILHOS LTDA",
	) {
		filtro, err := CompilarFiltro(expressao)
		if err != nil {
			t.Fatalf("CompilarFiltro: %v", err)
		}
		if !filtro.MatchString(texto) {
			t.Errorf("o filtro real não casou %q", texto)
		}
	}

	real, errReal := CompilarFiltro(expressao)
	if errReal != nil {
		t.Fatalf("CompilarFiltro: %v", errReal)
	}
	if real.String() == ingenuo.String() {
		t.Errorf("o filtro real e o ingênuo produziram o mesmo padrão %q", real.String())
	}
}

// TestCompilarFiltroExpressaoInvalida cobre o caminho de erro que no legado é
// um pânico (D-19).
func TestCompilarFiltroExpressaoInvalida(t *testing.T) {
	for _, expressao := range []string{
		"ACME & FILHOS (",
		"ACME & [FILHOS",
		"* & FILHOS",
	} {
		if _, err := CompilarFiltro(expressao); err == nil {
			t.Errorf("CompilarFiltro(%q) devolveu nil; esperava erro", expressao)
		}
	}
}

// TestCacheDeFiltrosMemoriza confirma que a expressão regular é compilada UMA
// vez por expressão — o achado A08 — e que a falha também é memorizada.
func TestCacheDeFiltrosMemoriza(t *testing.T) {
	c := novoCacheDeFiltros()

	primeira, err := c.Obter("ACME & FILHOS")
	if err != nil {
		t.Fatalf("primeira compilação: %v", err)
	}
	segunda, err := c.Obter("ACME & FILHOS")
	if err != nil {
		t.Fatalf("segunda compilação: %v", err)
	}
	if primeira != segunda {
		t.Error("a segunda chamada recompilou: o cache não está memorizando")
	}

	if _, err := c.Obter("ACME & ("); err == nil {
		t.Fatal("esperava erro para expressão inválida")
	}
	if _, err := c.Obter("ACME & ("); err == nil {
		t.Fatal("a falha memorizada deveria continuar sendo falha")
	}
}
