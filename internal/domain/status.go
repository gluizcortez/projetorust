package domain

import "fmt"

// StatusImportacao é o estado de uma importação na coluna
// recorte.tb_importacao.status.
//
// Substitui os inteiros mágicos do legado por um tipo com transições
// declaradas. Ver docs/ESPECIFICACAO.md §3.
type StatusImportacao int32

// Valores de status. Correspondem exatamente aos do legado
// (reference/main.rs:652-663).
const (
	// StatusErro é terminal e não distingue a causa da falha.
	StatusErro StatusImportacao = -1
	// StatusRecebido é gravado pelo INSERT e novamente logo em seguida — duas
	// escritas do mesmo valor, preservadas (achado A16).
	StatusRecebido StatusImportacao = 0
	// StatusSelecionado marca o início da tarefa de segundo plano.
	StatusSelecionado StatusImportacao = 1
	// StatusIndexando precede a extração do documento.
	StatusIndexando StatusImportacao = 2
	// StatusRecortando indica índice pronto e busca em andamento.
	StatusRecortando StatusImportacao = 3
	// StatusReservado NUNCA é gravado nem lido pelo serviço.
	//
	// O legado o documenta como "não usado" (reference/main.rs:661) e nenhum
	// caminho de código o produz. Está declarado para que o valor permaneça
	// reservado: se alguém precisar de um estado novo, deve escolher outro
	// número em vez de reaproveitar este, que pode existir em linhas antigas
	// do banco.
	StatusReservado StatusImportacao = 4
	// StatusFinalizado é terminal e indica o laço concluído sem erro.
	StatusFinalizado StatusImportacao = 5
)

// TodosOsStatus lista os valores declarados, na ordem numérica.
//
// O teste de exaustividade percorre esta lista: acrescentar uma constante sem
// acrescentá-la aqui e a TransicoesValidas faz o teste falhar.
var TodosOsStatus = []StatusImportacao{
	StatusErro, StatusRecebido, StatusSelecionado,
	StatusIndexando, StatusRecortando, StatusReservado, StatusFinalizado,
}

// TransicoesValidas mapeia cada estado aos que podem sucedê-lo.
//
// Derivado de docs/ESPECIFICACAO.md §3.3, que por sua vez foi extraído das
// gravações em reference/main.rs:249, 261, 268, 272, 314, 320, 325, 329, 334.
var TransicoesValidas = map[StatusImportacao][]StatusImportacao{
	// O UPDATE redundante de main.rs:249 grava 0 sobre 0.
	StatusRecebido:    {StatusRecebido, StatusSelecionado},
	StatusSelecionado: {StatusIndexando, StatusErro},
	StatusIndexando:   {StatusRecortando, StatusErro},
	StatusRecortando:  {StatusFinalizado, StatusErro},

	// Terminais.
	StatusFinalizado: {},
	StatusErro:       {},

	// Reservado: nenhum caminho de código entra ou sai dele.
	StatusReservado: {},
}

// String devolve o nome legível do estado.
func (s StatusImportacao) String() string {
	switch s {
	case StatusErro:
		return "erro"
	case StatusRecebido:
		return "recebido"
	case StatusSelecionado:
		return "selecionado"
	case StatusIndexando:
		return "indexando"
	case StatusRecortando:
		return "recortando"
	case StatusReservado:
		return "reservado"
	case StatusFinalizado:
		return "finalizado"
	default:
		return fmt.Sprintf("desconhecido(%d)", int32(s))
	}
}

// Conhecido informa se o valor é um dos declarados.
func (s StatusImportacao) Conhecido() bool {
	_, ok := TransicoesValidas[s]
	return ok
}

// Terminal informa se nenhuma transição sai deste estado.
func (s StatusImportacao) Terminal() bool {
	return s == StatusErro || s == StatusFinalizado
}

// PodeTransicionarPara informa se a transição consta do mapa declarado.
//
// A verificação é uma rede de segurança para desenvolvimento e teste: o legado
// não valida transições, e o serviço em Go não deve recusar uma gravação por
// causa dela — apenas registrar a anomalia. Recusar seria comportamento novo.
func (s StatusImportacao) PodeTransicionarPara(destino StatusImportacao) bool {
	for _, permitido := range TransicoesValidas[s] {
		if permitido == destino {
			return true
		}
	}
	return false
}
