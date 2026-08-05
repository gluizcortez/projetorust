package domain

import "strings"

// Textos de crítica de validação.
//
// SÃO CONTRATO OBSERVÁVEL, byte a byte. Conferidos linha a linha contra
// reference/main.rs na fase F3; a coluna indica a linha de origem.
// Alterar qualquer um destes literais quebra clientes que analisam a mensagem
// da resposta 400. Ver docs/ESPECIFICACAO.md §1.4.2.
const (
	CriticaDataCadernoInvalida          = "Data do caderno é inválida"             // main.rs:155
	CriticaDataCadernoAusente           = "Data do caderno não informada"          // main.rs:159
	CriticaDataDisponibilizacaoInvalida = "Data de disponibilização é inválida"    // main.rs:168
	CriticaDataDisponibilizacaoAusente  = "Data de disponibilização não informada" // main.rs:172
	CriticaIDUsuarioInvalido            = "Id do usuário é inválido"               // main.rs:181
	CriticaIDUsuarioAusente             = "Id do usuário não informado"            // main.rs:185
	CriticaIDCadernoInvalido            = "Id do caderno é inválido"               // main.rs:194
	CriticaIDCadernoAusente             = "Id do caderno não informado"            // main.rs:198
	CriticaPDFSemNome                   = "PDF não possui nome"                    // main.rs:207
	CriticaPDFAusente                   = "PDF não enviado"                        // main.rs:211
)

// SeparadorDeCriticas é a vírgula SEM espaço, reproduzindo
// criticas.join(",") de reference/main.rs:220.
const SeparadorDeCriticas = ","

// Criticas acumula os erros de validação de uma requisição.
//
// A ORDEM DE INSERÇÃO É CONTRATO: a mensagem da resposta 400 é a concatenação
// nesta ordem. O legado a fixa pela sequência dos blocos de validação
// (reference/main.rs:151-214): data do caderno, data de disponibilização, id
// do usuário, id do caderno, PDF.
type Criticas struct {
	itens []string
}

// Adicionar acrescenta uma crítica ao final, preservando a ordem.
func (c *Criticas) Adicionar(texto string) {
	c.itens = append(c.itens, texto)
}

// Vazio informa se nenhuma crítica foi acumulada.
func (c *Criticas) Vazio() bool { return len(c.itens) == 0 }

// Total devolve a quantidade acumulada.
func (c *Criticas) Total() int { return len(c.itens) }

// Itens devolve uma cópia das críticas, na ordem de inserção.
//
// A cópia evita que quem consome altere o acumulador por engano — em
// particular a camada HTTP, que precisa da lista para o corpo problem+json da
// fase F11.
func (c *Criticas) Itens() []string {
	saida := make([]string, len(c.itens))
	copy(saida, c.itens)
	return saida
}

// Mensagem devolve o corpo exato da resposta 400.
//
// Reproduz criticas.join(",") — vírgula sem espaço. Com o acumulador vazio
// devolve a cadeia vazia, que é o que join produz sobre um vetor vazio.
func (c *Criticas) Mensagem() string {
	return strings.Join(c.itens, SeparadorDeCriticas)
}

// Erro devolve ErrValidacao quando há críticas, e nil quando não há.
//
// Permite que o caso de uso propague a falha por errors.Is sem perder a lista.
func (c *Criticas) Erro() error {
	if c.Vazio() {
		return nil
	}
	return ErrValidacao
}
