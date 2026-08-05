package domain_test

import (
	"errors"
	"testing"
	"time"

	"github.com/gluizcortez/projetorust/internal/domain"
)

// TestAnalisarDataReproduzChrono percorre a tabela MEDIDA contra
// chrono 0.4 na fase F3.
//
// Cada linha foi obtida executando
// chrono::NaiveDate::parse_from_str(entrada, "%Y-%m-%d") — não deduzida.
// Ver docs/INVARIANTES.md, INV-P21.
func TestAnalisarDataReproduzChrono(t *testing.T) {
	casos := []struct {
		entrada  string
		aceita   bool
		esperada string // formato AAAA-MM-DD; vazio quando rejeitada
		nota     string
	}{
		// --- forma canônica ---
		{"2024-03-15", true, "2024-03-15", "canônica"},
		{"2024-01-01", true, "2024-01-01", "limite inferior do ano"},
		{"2024-12-31", true, "2024-12-31", "limite superior do ano"},

		// --- dígito único: ACEITO, e um porte com time.Parse rejeitaria ---
		{"2024-3-15", true, "2024-03-15", "mês com um dígito"},
		{"2024-03-5", true, "2024-03-05", "dia com um dígito"},
		{"2024-3-5", true, "2024-03-05", "mês e dia com um dígito"},

		// --- ano curto: ACEITO como o ano literal ---
		{"1-03-15", true, "0001-03-15", "ano de um dígito"},
		{"12-03-15", true, "0012-03-15", "ano de dois dígitos"},
		{"123-03-15", true, "0123-03-15", "ano de três dígitos"},
		{"24-03-15", true, "0024-03-15", "NÃO é 2024"},
		{"0000-01-01", true, "0000-01-01", "ano zero é válido"},

		// --- ano longo: só com sinal explícito ---
		{"12345-03-15", false, "", "cinco dígitos sem sinal"},
		{"262143-12-31", false, "", "seis dígitos sem sinal"},
		{"+12345-03-15", true, "12345-03-15", "cinco dígitos COM sinal"},

		// --- sinais ---
		{"+2024-03-15", true, "2024-03-15", "sinal positivo explícito"},
		{"-2024-03-15", true, "-2024-03-15", "ano negativo"},
		{"+0-01-01", true, "0000-01-01", "zero com sinal"},

		// --- espaço em branco ---
		{"  2024-03-15", true, "2024-03-15", "espaços à esquerda: ignorados"},
		{"\t2024-03-15", true, "2024-03-15", "tabulação à esquerda"},
		{"\n2024-03-15", true, "2024-03-15", "nova linha à esquerda"},
		{"2024- 03-15", true, "2024-03-15", "espaço ANTES do campo numérico"},
		{"2024-03- 15", true, "2024-03-15", "espaço ANTES do dia"},
		{"2024 -03-15", false, "", "espaço antes do '-' literal: rejeitado"},
		{"2024-03 -15", false, "", "espaço antes do segundo '-'"},
		{"2024-03-15 ", false, "", "espaço à DIREITA: rejeitado"},
		{"2024-03-15\n", false, "", "nova linha à direita"},
		{"2024-03-15\t", false, "", "tabulação à direita"},

		// --- sobra ---
		{"2024-03-15T00:00:00", false, "", "sufixo de hora"},
		{"2024-03-15 extra", false, "", "sufixo qualquer"},
		{"2024-03-015", false, "", "dia com três dígitos: sobra"},

		// --- separador e formato ---
		{"20240315", false, "", "sem separador"},
		{"2024/03/15", false, "", "separador errado"},
		{"", false, "", "vazio"},
		{"2024-+3-15", false, "", "sinal em campo sem sinal"},
		{"2024-03--5", false, "", "sinal no dia"},

		// --- intervalo ---
		{"2024-13-01", false, "", "mês 13"},
		{"2024-00-15", false, "", "mês 0"},
		{"2024-0-15", false, "", "mês 0 com um dígito"},
		{"2024-003-15", false, "", "mês com três dígitos vira 00"},
		{"2024-03-00", false, "", "dia 0"},
		{"2024-02-30", false, "", "30 de fevereiro"},
		{"2024-02-29", true, "2024-02-29", "bissexto"},
		{"2023-02-29", false, "", "não bissexto"},
		{"2000-02-29", true, "2000-02-29", "divisível por 400"},
		{"1900-02-29", false, "", "divisível por 100, não por 400"},
	}

	for _, c := range casos {
		t.Run(c.entrada+" · "+c.nota, func(t *testing.T) {
			d, err := domain.AnalisarData(c.entrada)

			if !c.aceita {
				if err == nil {
					t.Fatalf("chrono REJEITA %q (%s), mas o analisador aceitou como %s",
						c.entrada, c.nota, d)
				}
				if !errors.Is(err, domain.ErrDataInvalida) {
					t.Errorf("erro deveria envolver ErrDataInvalida: %v", err)
				}
				return
			}

			if err != nil {
				t.Fatalf("chrono ACEITA %q (%s), mas o analisador rejeitou: %v",
					c.entrada, c.nota, err)
			}
			if d.String() != c.esperada {
				t.Errorf("%q -> %s, esperado %s (%s)", c.entrada, d, c.esperada, c.nota)
			}
		})
	}
}

// TestAnalisarDataDivergeDeTimeParse documenta, de forma executável, por que
// time.Parse não pode ser usado aqui.
//
// Se algum dia alguém "simplificar" AnalisarData para time.Parse, este teste
// falha e explica o motivo.
func TestAnalisarDataDivergeDeTimeParse(t *testing.T) {
	// Entradas que o legado ACEITA e que time.Parse("2006-01-02") REJEITA.
	divergentes := []string{
		"2024-3-15", "2024-03-5", "24-03-15",
		"  2024-03-15", "+2024-03-15", "-2024-03-15",
	}

	for _, entrada := range divergentes {
		t.Run(entrada, func(t *testing.T) {
			if _, err := domain.AnalisarData(entrada); err != nil {
				t.Errorf("o legado aceita %q; AnalisarData rejeitou: %v", entrada, err)
			}
			if _, err := time.Parse("2006-01-02", entrada); err == nil {
				t.Errorf("premissa do teste quebrou: time.Parse passou a aceitar %q — "+
					"reveja INV-P21", entrada)
			}
		})
	}
}

func TestDataZero(t *testing.T) {
	var d domain.Data
	if !d.Zero() {
		t.Error("o valor zero de Data deveria ser 'não informada'")
	}
	if d.String() != "" {
		t.Errorf("Data zero deveria formatar como vazio, obtive %q", d.String())
	}

	preenchida, err := domain.NovaData(2024, 3, 15)
	if err != nil {
		t.Fatalf("NovaData: %v", err)
	}
	if preenchida.Zero() {
		t.Error("data preenchida não pode ser Zero")
	}
	if preenchida.Ano() != 2024 || preenchida.Mes() != 3 || preenchida.Dia() != 15 {
		t.Errorf("componentes = %d-%d-%d", preenchida.Ano(), preenchida.Mes(), preenchida.Dia())
	}
}

func TestNovaDataValidaIntervalo(t *testing.T) {
	casos := []struct {
		ano, mes, dia int
		valida        bool
	}{
		{2024, 1, 1, true},
		{2024, 12, 31, true},
		{2024, 2, 29, true},
		{2023, 2, 29, false},
		{2024, 0, 1, false},
		{2024, 13, 1, false},
		{2024, 1, 0, false},
		{2024, 1, 32, false},
		{2024, 4, 31, false},
		{0, 1, 1, true},
		{-44, 3, 15, true},
	}
	for _, c := range casos {
		_, err := domain.NovaData(c.ano, c.mes, c.dia)
		if c.valida && err != nil {
			t.Errorf("NovaData(%d,%d,%d) deveria ser válida: %v", c.ano, c.mes, c.dia, err)
		}
		if !c.valida && err == nil {
			t.Errorf("NovaData(%d,%d,%d) deveria ser inválida", c.ano, c.mes, c.dia)
		}
	}
}

// TestAnalisarInteiroReproduzRust percorre a tabela medida contra
// str::parse::<i64>() e ::<i32>() na fase F3.
func TestAnalisarInteiroReproduzRust(t *testing.T) {
	casos := []struct {
		entrada string
		bits    int
		aceita  bool
		valor   int64
	}{
		{"5", 64, true, 5},
		{"+5", 64, true, 5},
		{"-5", 64, true, -5},
		{"0", 64, true, 0},
		{" 5", 64, false, 0},
		{"5 ", 64, false, 0},
		{"5.0", 64, false, 0},
		{"1_000", 64, false, 0},
		{"", 64, false, 0},
		{"9223372036854775807", 64, true, 9223372036854775807},
		{"9223372036854775808", 64, false, 0},
		{"2147483647", 32, true, 2147483647},
		{"2147483648", 32, false, 0},
		{"-2147483648", 32, true, -2147483648},
		{"-2147483649", 32, false, 0},
	}

	for _, c := range casos {
		t.Run(c.entrada, func(t *testing.T) {
			v, err := domain.AnalisarInteiro(c.entrada, c.bits)
			if c.aceita {
				if err != nil {
					t.Fatalf("Rust aceita %q como i%d; rejeitado: %v", c.entrada, c.bits, err)
				}
				if v != c.valor {
					t.Errorf("%q -> %d, esperado %d", c.entrada, v, c.valor)
				}
				return
			}
			if err == nil {
				t.Errorf("Rust rejeita %q como i%d; aceito como %d", c.entrada, c.bits, v)
			}
			if !errors.Is(err, domain.ErrValidacao) {
				t.Errorf("erro deveria envolver ErrValidacao: %v", err)
			}
		})
	}
}
