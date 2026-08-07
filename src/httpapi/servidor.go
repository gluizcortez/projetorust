package httpapi

import (
	"net/http"
	"time"
)

// Tempos limite do servidor.
//
// O legado NÃO define nenhum: `Server::new(acceptor).serve(service)` usa os
// padrões do Salvo/hyper. Definir os do Go explicitamente é necessário porque o
// `http.Server` sem tempos limite fica exposto a conexão lenta — mas os valores
// precisam ser generosos o bastante para NÃO alterar o comportamento com PDFs
// grandes, que é o caso de uso real.
//
// A escolha, e o porquê de cada um:
//
//	ReadHeaderTimeout  10s   cabeçalho é pequeno; 10s cobre qualquer rede ruim e
//	                         fecha a porta para o ataque de cabeçalho lento
//	ReadTimeout        0     SEM LIMITE. Um diário oficial de centenas de
//	                         megabytes por um enlace lento leva minutos, e
//	                         cortar o envio no meio mudaria o comportamento
//	                         observável. É a única forma de preservar paridade
//	                         sem adivinhar o tamanho máximo — que é a decisão
//	                         aberta D-04.
//	WriteTimeout       0     SEM LIMITE, pelo mesmo motivo: o relógio de escrita
//	                         do Go começa a contar na LEITURA do cabeçalho, então
//	                         qualquer valor finito limitaria também o envio.
//	IdleTimeout        120s  conexão persistente ociosa; não afeta requisição em
//	                         andamento
//	MaxHeaderBytes     1 MiB  padrão do Go
//
// Os dois zeros são deliberados e ficam configuráveis: quem souber o tamanho
// máximo real dos documentos (D-04) pode fechá-los sem risco.
const (
	TempoLimiteDeCabecalhoPadrao = 10 * time.Second
	TempoLimiteOciosoPadrao      = 120 * time.Second
	MaxHeaderBytesPadrao         = 1 << 20
)

// OpcoesDoServidor são os tempos limite e o tamanho máximo de cabeçalho.
//
// O valor zero de cada campo significa "sem limite", como em http.Server.
type OpcoesDoServidor struct {
	Endereco string

	TempoLimiteDeCabecalho time.Duration
	TempoLimiteDeLeitura   time.Duration
	TempoLimiteDeEscrita   time.Duration
	TempoLimiteOcioso      time.Duration
	MaxHeaderBytes         int
}

// PadroesDoServidor devolve as opções com os valores documentados acima.
func PadroesDoServidor(endereco string) OpcoesDoServidor {
	return OpcoesDoServidor{
		Endereco:               endereco,
		TempoLimiteDeCabecalho: TempoLimiteDeCabecalhoPadrao,
		TempoLimiteDeLeitura:   0,
		TempoLimiteDeEscrita:   0,
		TempoLimiteOcioso:      TempoLimiteOciosoPadrao,
		MaxHeaderBytes:         MaxHeaderBytesPadrao,
	}
}

// NovoServidor monta o http.Server com os tempos limite explícitos.
//
// Não escuta nem serve: quem conduz o ciclo de vida é a raiz de composição, na
// fase F10.
func NovoServidor(manipulador http.Handler, op OpcoesDoServidor) *http.Server {
	return &http.Server{
		Addr:              op.Endereco,
		Handler:           manipulador,
		ReadHeaderTimeout: op.TempoLimiteDeCabecalho,
		ReadTimeout:       op.TempoLimiteDeLeitura,
		WriteTimeout:      op.TempoLimiteDeEscrita,
		IdleTimeout:       op.TempoLimiteOcioso,
		MaxHeaderBytes:    op.MaxHeaderBytes,
	}
}
