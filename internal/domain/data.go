package domain

import (
	"errors"
	"fmt"
	"strconv"
	"unicode"
	"unicode/utf8"
)

// Data é uma data pura: sem hora e sem fuso horário.
//
// Existe porque o legado usa chrono::NaiveDate (reference/main.rs:419-421),
// que não carrega fuso e mapeia para `date` do PostgreSQL. Usar time.Time aqui
// abriria a porta para o deslocamento de um dia descrito em INV-P16.
//
// O valor zero representa "não informada": mes vale 0, que nunca é uma data
// válida.
type Data struct {
	ano int // pode ser negativo — calendário gregoriano proléptico
	mes int // 1..12
	dia int // 1..31
}

// ErrDataInvalida é devolvido por AnalisarData quando a entrada não é aceita.
var ErrDataInvalida = errors.New("data inválida")

// NovaData monta uma data a partir dos componentes, validando o intervalo.
func NovaData(ano, mes, dia int) (Data, error) {
	if mes < 1 || mes > 12 {
		return Data{}, fmt.Errorf("%w: mês %d fora de 1..12", ErrDataInvalida, mes)
	}
	if dia < 1 || dia > diasNoMes(ano, mes) {
		return Data{}, fmt.Errorf("%w: dia %d fora de 1..%d", ErrDataInvalida, dia, diasNoMes(ano, mes))
	}
	return Data{ano: ano, mes: mes, dia: dia}, nil
}

// Zero informa se a data não foi informada.
func (d Data) Zero() bool { return d.mes == 0 }

// Ano devolve o ano. Pode ser negativo.
func (d Data) Ano() int { return d.ano }

// Mes devolve o mês, de 1 a 12.
func (d Data) Mes() int { return d.mes }

// Dia devolve o dia do mês.
func (d Data) Dia() int { return d.dia }

// String formata como AAAA-MM-DD, com o ano em pelo menos quatro dígitos.
func (d Data) String() string {
	if d.Zero() {
		return ""
	}
	if d.ano < 0 {
		return fmt.Sprintf("-%04d-%02d-%02d", -d.ano, d.mes, d.dia)
	}
	return fmt.Sprintf("%04d-%02d-%02d", d.ano, d.mes, d.dia)
}

// AnalisarData replica EXATAMENTE o comportamento de
// chrono::NaiveDate::parse_from_str(s, "%Y-%m-%d"), usado em
// reference/main.rs:152 e 165.
//
// ATENÇÃO — ver docs/INVARIANTES.md, INV-P21.
//
// O analisador do chrono é bem mais permissivo que time.Parse("2006-01-02", s)
// do Go. Trocar um pelo outro muda quais requisições recebem a crítica
// "Data do caderno é inválida" e, portanto, muda o corpo da resposta 400.
// A gramática abaixo foi MEDIDA contra chrono 0.4, não deduzida:
//
//	data  := ws* ano '-' ws* mes '-' ws* dia FIM
//	ano   := sinal? digitos      // com sinal: 1..6 dígitos; sem sinal: 1..4
//	mes   := digitos             // 1..2 dígitos
//	dia   := digitos             // 1..2 dígitos
//
// Consequências que um porte ingênuo perderia:
//   - "2024-3-15" e "2024-03-5" são ACEITOS (dígito único)
//   - "24-03-15" é ACEITO, como o ano 24
//   - espaço em branco à ESQUERDA é ignorado; à direita, não
//   - "+2024-03-15" e "-2024-03-15" são ACEITOS
//   - espaço é ignorado ANTES de cada campo numérico, mas não antes do '-'
//     literal: "2024- 03-15" passa, "2024 -03-15" não
func AnalisarData(s string) (Data, error) {
	p := &analisador{entrada: s}

	ano, err := p.ano()
	if err != nil {
		return Data{}, err
	}
	if err := p.literal('-'); err != nil {
		return Data{}, err
	}
	mes, err := p.numero(2)
	if err != nil {
		return Data{}, err
	}
	if err := p.literal('-'); err != nil {
		return Data{}, err
	}
	dia, err := p.numero(2)
	if err != nil {
		return Data{}, err
	}
	// chrono recusa qualquer sobra, inclusive espaço em branco.
	if p.pos != len(p.entrada) {
		return Data{}, fmt.Errorf("%w: sobra após a data em %q", ErrDataInvalida, s)
	}

	return NovaData(ano, mes, dia)
}

type analisador struct {
	entrada string
	pos     int
}

// pularEspacos consome espaço em branco. O chrono o faz antes de cada campo
// numérico, e apenas antes deles — daí "2024- 03-15" passar e "2024 -03-15"
// não.
func (p *analisador) pularEspacos() {
	for p.pos < len(p.entrada) {
		r, tamanho := utf8.DecodeRuneInString(p.entrada[p.pos:])
		if !unicode.IsSpace(r) {
			return
		}
		p.pos += tamanho
	}
}

func (p *analisador) literal(c byte) error {
	if p.pos >= len(p.entrada) || p.entrada[p.pos] != c {
		return fmt.Errorf("%w: esperava %q na posição %d de %q", ErrDataInvalida, c, p.pos, p.entrada)
	}
	p.pos++
	return nil
}

// ano lê o campo de ano: sinal opcional, depois dígitos.
//
// Sem sinal o chrono consome no máximo 4 dígitos; com sinal explícito, até 6.
// Essa assimetria foi medida, não inferida.
func (p *analisador) ano() (int, error) {
	p.pularEspacos()

	negativo := false
	maximo := 4
	if p.pos < len(p.entrada) && (p.entrada[p.pos] == '+' || p.entrada[p.pos] == '-') {
		negativo = p.entrada[p.pos] == '-'
		p.pos++
		maximo = 6
	}

	v, err := p.digitos(maximo)
	if err != nil {
		return 0, err
	}
	if negativo {
		return -v, nil
	}
	return v, nil
}

// numero lê um campo numérico sem sinal, precedido de espaço opcional.
func (p *analisador) numero(maximo int) (int, error) {
	p.pularEspacos()
	return p.digitos(maximo)
}

func (p *analisador) digitos(maximo int) (int, error) {
	inicio := p.pos
	for p.pos < len(p.entrada) && p.pos-inicio < maximo {
		c := p.entrada[p.pos]
		if c < '0' || c > '9' {
			break
		}
		p.pos++
	}
	if p.pos == inicio {
		return 0, fmt.Errorf("%w: esperava dígito na posição %d de %q", ErrDataInvalida, inicio, p.entrada)
	}
	v, err := strconv.Atoi(p.entrada[inicio:p.pos])
	if err != nil {
		return 0, fmt.Errorf("%w: %q", ErrDataInvalida, p.entrada[inicio:p.pos])
	}
	return v, nil
}

func bissexto(ano int) bool {
	return ano%4 == 0 && (ano%100 != 0 || ano%400 == 0)
}

func diasNoMes(ano, mes int) int {
	switch mes {
	case 1, 3, 5, 7, 8, 10, 12:
		return 31
	case 4, 6, 9, 11:
		return 30
	case 2:
		if bissexto(ano) {
			return 29
		}
		return 28
	default:
		return 0
	}
}

// AnalisarInteiro replica o comportamento de str::parse::<i64>() do Rust,
// usado em reference/main.rs:178 e 191.
//
// Rust e Go concordam aqui — ambos aceitam sinal explícito e rejeitam espaço,
// separador de milhar e parte decimal — mas a equivalência foi MEDIDA, não
// presumida, e esta função existe para que exista um lugar único onde a
// afirmação possa ser testada.
func AnalisarInteiro(s string, bits int) (int64, error) {
	v, err := strconv.ParseInt(s, 10, bits)
	if err != nil {
		return 0, fmt.Errorf("%w: %q não é um inteiro de %d bits", ErrValidacao, s, bits)
	}
	return v, nil
}
