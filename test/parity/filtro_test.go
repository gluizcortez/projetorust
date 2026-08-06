package parity_test

import (
	"bufio"
	"encoding/hex"
	"errors"
	"math/rand/v2"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/gluizcortez/projetorust/internal/adapter/searchidx"
	"github.com/gluizcortez/projetorust/internal/domain"
)

const caminhoOraculoFiltro = "../../tools/capturar-corpus/target/release/oraculo-extended"

// Os testes deste arquivo comparam o filtro do operador `&` em Go com o do
// legado sobre pares expressão/texto gerados aleatoriamente.
//
// O corpus dourado cobre as expressões que existem em produção; estes testes
// cobrem o ESPAÇO DE CONSTRUÇÕES. Foi um teste de propriedade assim que, na
// fase F6, encontrou 320.142 divergências que as 159 páginas do corpus não
// tinham revelado.
//
// São DOIS testes porque há duas propriedades diferentes, com forças
// diferentes:
//
//	TestPropriedadeFiltroOperador       exatidão sobre o que uma expressão de
//	                                    perfil pode realmente ser. Zero
//	                                    divergência de qualquer espécie.
//	TestCaracterizacaoSintaxeDivergente caracterização do espaço adversarial.
//	                                    Os dois motores podem DISCORDAR SOBRE
//	                                    ACEITAR um padrão — são analisadores
//	                                    sintáticos diferentes, e nada em Go
//	                                    conserta isso. O que não podem é
//	                                    discordar sobre o RESULTADO quando os
//	                                    dois aceitam.
//
// A segunda é a que importa para o risco: uma divergência de aceitação FALHA
// ALTO — a importação não conclui, e alguém percebe. Uma divergência de
// resultado seria silenciosa, e é a que precisa ser zero.

// vereditos possíveis, no vocabulário compartilhado com o oráculo em Rust.
const (
	vereditoCasou     = "casou"
	vereditoNaoCasou  = "naocasou"
	vereditoSemFiltro = "semfiltro"
	vereditoRecusou   = "panico" // o legado entra em pânico; o Go devolve erro
)

type casoDeFiltro struct{ expressao, texto string }

// consultarOraculo roda o oráculo em Rust sobre todos os casos de uma vez e
// devolve o veredito do legado para cada um.
func consultarOraculo(t *testing.T, casos []casoDeFiltro) []string {
	t.Helper()

	var entrada strings.Builder
	entrada.Grow(len(casos) * 64)
	for _, c := range casos {
		entrada.WriteString(hex.EncodeToString([]byte(c.expressao)))
		entrada.WriteByte('\t')
		entrada.WriteString(hex.EncodeToString([]byte(c.texto)))
		entrada.WriteByte('\n')
	}

	cmd := exec.Command(caminhoOraculoFiltro)
	cmd.Stdin = strings.NewReader(entrada.String())
	saida, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("abrindo saída do oráculo: %v", err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatalf("iniciando o oráculo: %v", err)
	}

	sc := bufio.NewScanner(saida)
	sc.Buffer(make([]byte, 0, 1<<16), 1<<20)

	vereditos := make([]string, 0, len(casos))
	for sc.Scan() {
		vereditos = append(vereditos, sc.Text())
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("lendo a saída do oráculo: %v", err)
	}
	if err := cmd.Wait(); err != nil {
		t.Fatalf("oráculo encerrou com erro: %v", err)
	}
	if len(vereditos) != len(casos) {
		t.Fatalf("o oráculo devolveu %d linhas, esperava %d", len(vereditos), len(casos))
	}
	return vereditos
}

// exigirVereditoConhecido protege contra um defeito do PRÓPRIO ARNÊS. O
// oráculo responde `entradainvalida` quando o caso não é UTF-8 válido, e um
// gerador com defeito produziria milhares desses — que, contados como
// divergência, apontariam para o código em vez de para o gerador.
func exigirVereditoConhecido(t *testing.T, veredito string, caso casoDeFiltro) {
	t.Helper()
	switch veredito {
	case vereditoCasou, vereditoNaoCasou, vereditoSemFiltro, vereditoRecusou:
		return
	default:
		t.Fatalf("o oráculo devolveu %q — defeito do arnês, não do código.\n"+
			"  expressão: %q\n  texto    : %q", veredito, caso.expressao, caso.texto)
	}
}

func exigirOraculo(t *testing.T) {
	t.Helper()
	if testing.Short() {
		t.Skip("teste de propriedade é demorado")
	}
	if _, err := os.Stat(caminhoOraculoFiltro); err != nil {
		t.Skipf("oráculo indisponível (%v) — compile com "+
			"`cargo build --release --manifest-path tools/capturar-corpus/Cargo.toml "+
			"--bin oraculo-extended`", err)
	}
}

// TestPropriedadeFiltroOperador exige acordo TOTAL — inclusive sobre aceitar a
// expressão — no espaço do que uma `expressao_nm` de `tb_perfil_variacao` pode
// realmente conter: nome de empresa ou de pessoa, com o operador `&`.
//
// É aqui que INV-P01 vive. Se StripExtended errar em qualquer construção
// alcançável em produção, este teste cai.
func TestPropriedadeFiltroOperador(t *testing.T) {
	exigirOraculo(t)

	const total = 250_000
	// Semente fixa: uma falha precisa ser reproduzível.
	fonte := rand.New(rand.NewPCG(0xF117, 0x0))

	casos := make([]casoDeFiltro, total)
	for i := range total {
		casos[i] = casoDeFiltro{
			expressao: expressaoRealista(fonte),
			texto:     textoAleatorio(fonte),
		}
	}

	vereditos := consultarOraculo(t, casos)

	var divergencias int
	porVeredito := map[string]int{}
	for i, esperado := range vereditos {
		exigirVereditoConhecido(t, esperado, casos[i])
		porVeredito[esperado]++
		obtido, err := vereditoEmGo(casos[i].expressao, casos[i].texto)
		if obtido == esperado {
			continue
		}
		divergencias++
		if divergencias <= 8 {
			t.Errorf("FILTRO diverge\n  expressão: %q\n  texto    : %q\n  Go       : %s (%v)\n  legado   : %s",
				casos[i].expressao, casos[i].texto, obtido, err, esperado)
		}
	}

	// Uma geração que nunca produz `&`, ou que nunca casa, não mede nada.
	for _, exigido := range []string{vereditoCasou, vereditoNaoCasou, vereditoSemFiltro} {
		if porVeredito[exigido] == 0 {
			t.Errorf("a geração não produziu nenhum caso de veredito %q", exigido)
		}
	}
	// E não pode produzir recusa: nada em uma expressão realista deve fazer o
	// legado entrar em pânico.
	if porVeredito[vereditoRecusou] > 0 {
		t.Errorf("%d expressões realistas fizeram o legado entrar em pânico — "+
			"a geração escapou do espaço que pretende cobrir", porVeredito[vereditoRecusou])
	}

	if divergencias > 0 {
		t.Errorf("total: %d divergências em %d casos", divergencias, total)
		return
	}
	t.Logf("%d casos sem divergência — %d casou, %d não casou, %d sem filtro",
		total, porVeredito[vereditoCasou], porVeredito[vereditoNaoCasou],
		porVeredito[vereditoSemFiltro])
}

// TestCaracterizacaoSintaxeDivergente varre o espaço ADVERSARIAL: expressões
// com sintaxe de expressão regular arbitrária, que uma `expressao_nm` não
// deveria conter mas que nada no banco impede.
//
// Aqui os dois motores PODEM discordar sobre aceitar o padrão — o crate `regex`
// e o RE2 são analisadores diferentes, e `(?-x)`, por exemplo, simplesmente não
// existe no RE2. Isso está registrado em D-19 e falha alto: a importação não
// conclui e alguém percebe.
//
// O que este teste EXIGE é a propriedade silenciosa: sempre que os DOIS motores
// aceitam a expressão, eles concordam sobre o resultado. Uma divergência aí
// gravaria recortes errados sem que ninguém notasse.
func TestCaracterizacaoSintaxeDivergente(t *testing.T) {
	exigirOraculo(t)

	const total = 250_000
	fonte := rand.New(rand.NewPCG(0xADFE, 0x5A))

	casos := make([]casoDeFiltro, total)
	for i := range total {
		casos[i] = casoDeFiltro{
			expressao: expressaoAdversarial(fonte),
			texto:     textoAleatorio(fonte),
		}
	}

	vereditos := consultarOraculo(t, casos)

	var (
		acordo              int
		soORustRecusa       int
		soOGoRecusa         int
		divergenciaDeResult int
	)
	for i, esperado := range vereditos {
		exigirVereditoConhecido(t, esperado, casos[i])
		obtido, err := vereditoEmGo(casos[i].expressao, casos[i].texto)

		switch {
		case obtido == esperado:
			acordo++
		case esperado == vereditoRecusou:
			soORustRecusa++
		case obtido == vereditoRecusou:
			soOGoRecusa++
		default:
			// Os dois aceitaram e discordaram do resultado. É a classe que não
			// pode existir.
			divergenciaDeResult++
			if divergenciaDeResult <= 8 {
				t.Errorf("DIVERGÊNCIA SILENCIOSA — os dois motores aceitaram e discordaram\n"+
					"  expressão: %q\n  texto    : %q\n  Go       : %s (%v)\n  legado   : %s",
					casos[i].expressao, casos[i].texto, obtido, err, esperado)
			}
		}
	}

	t.Logf("espaço adversarial, %d casos: %d de acordo, %d recusados só pelo Rust, "+
		"%d recusados só pelo Go, %d divergências de resultado",
		total, acordo, soORustRecusa, soOGoRecusa, divergenciaDeResult)

	if divergenciaDeResult > 0 {
		t.Errorf("%d divergências de RESULTADO entre motores que aceitaram a expressão",
			divergenciaDeResult)
	}
	// Se a geração parar de produzir sintaxe divergente, o teste vira decoração.
	if soORustRecusa+soOGoRecusa == 0 {
		t.Error("nenhuma divergência de aceitação — a geração não está cobrindo " +
			"o espaço adversarial que pretende caracterizar")
	}
}

// vereditoEmGo produz o mesmo vocabulário de resposta do oráculo em Rust.
func vereditoEmGo(expressao, texto string) (string, error) {
	if !strings.ContainsRune(expressao, '&') {
		return vereditoSemFiltro, nil
	}
	re, err := searchidx.CompilarFiltro(expressao)
	if err != nil {
		if errors.Is(err, domain.ErrExpressaoInvalida) {
			// O legado entra em pânico onde o Go devolve erro tipado — a
			// diferença de MECANISMO está em D-19. Os dois são "recusou".
			return vereditoRecusou, err
		}
		return "erroinesperado", err
	}
	if re.MatchString(texto) {
		return vereditoCasou, nil
	}
	return vereditoNaoCasou, nil
}

// pedacosRealistas são as peças de uma `expressao_nm` plausível: nome de
// empresa ou de pessoa. Nada aqui é metacaractere de expressão regular, exceto
// o que uma razão social realmente carrega — parênteses BALANCEADOS, ponto,
// hífen, barra e a cerquilha.
var pedacosRealistas = []string{
	"ACME", "FILHOS", "LTDA", "JOAO", "SILVA", "CIA", "ME", "EIRELI",
	"acme", "filhos", "Conceicao", "CONCEIÇÃO", "SØREN", "STRAßE", "2",
	// O operador e seus arredores — o objeto do filtro.
	"&", " & ", "&&", " &", "& ",
	// Espaço em branco, incluindo os que o RE2 não considera branco.
	" ", "  ", "\t", "\v", "\n", " ", "　", " ",
	// Pontuação de razão social.
	".", ",", "-", "/", "'", "S.A.", "(BRASIL)", "(SP)",
	// A cerquilha: plausível em "ACME #1", e inicia COMENTÁRIO no modo x.
	"#", "#1", " # ",
	// Espaço escapado: a barra invertida também aparece em dado sujo.
	"\\ ",
}

func expressaoRealista(fonte *rand.Rand) string {
	var b strings.Builder
	pedacos := 1 + fonte.IntN(6)
	for range pedacos {
		b.WriteString(pedacosRealistas[fonte.IntN(len(pedacosRealistas))])
	}

	// 70% das expressões recebem um `&` garantido: sem ele o oráculo devolve
	// `semfiltro` e o caso não mede o filtro.
	return talvezInserirOperador(fonte, b.String())
}

// pedacosAdversariais acrescentam a sintaxe de expressão regular que o campo do
// banco não impede mas que ninguém deveria digitar.
var pedacosAdversariais = append([]string{
	"\\\\", "\\.", "\\s", "\\d", "\\w", "\\S", "\\#",
	"[ab]", "[a b]", "[^x]", "[", "]",
	"(", ")", "(a)", "*", "+", "?", ".", "|", "{2}", "{2, 3}", "^", "$",
	"(?-x)", "(?i)", "(?x)",
}, pedacosRealistas...)

func expressaoAdversarial(fonte *rand.Rand) string {
	var b strings.Builder
	pedacos := 1 + fonte.IntN(6)
	for range pedacos {
		b.WriteString(pedacosAdversariais[fonte.IntN(len(pedacosAdversariais))])
	}

	return talvezInserirOperador(fonte, b.String())
}

// talvezInserirOperador enfia um ` & ` em posição aleatória, em 70% dos casos.
//
// O corte é feito em RUNAS, nunca em bytes: cortar no meio de uma sequência
// UTF-8 produziria uma cadeia inválida, que o oráculo em Rust recusa em
// `String::from_utf8` antes de olhar para o filtro. A primeira versão deste
// gerador cortava em bytes e produziu 7.272 falsas divergências.
func talvezInserirOperador(fonte *rand.Rand, s string) string {
	if fonte.IntN(100) >= 70 || strings.ContainsRune(s, '&') {
		return s
	}
	runas := []rune(s)
	corte := fonte.IntN(len(runas) + 1)
	return string(runas[:corte]) + " & " + string(runas[corte:])
}

// pedacosDeTexto monta o texto de página contra o qual o filtro é aplicado.
var pedacosDeTexto = []string{
	"ACME", "FILHOS", "acme", "filhos", "LTDA", "JOAO", "SILVA",
	"&", " ", "  ", "\t", "\n", "\v", "\f", "\r",
	"\u00a0", "\u3000", "\u2009", "\u1680", "\u200b", "\u0085",
	".", ",", "-", "#", "[", "]", "(", ")", "\\", "1", "2",
}

func textoAleatorio(fonte *rand.Rand) string {
	var b strings.Builder
	pedacos := 1 + fonte.IntN(12)
	for range pedacos {
		b.WriteString(pedacosDeTexto[fonte.IntN(len(pedacosDeTexto))])
	}
	return b.String()
}
