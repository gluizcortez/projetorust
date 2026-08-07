package middleware

import (
	"net/http"
	"strconv"
	"sync"
	"time"
)

// -------------------------------------------------------------------------
// Limitação de taxa — RATE_LIMIT_RPS
// -------------------------------------------------------------------------
//
// EVOLUÇÃO da fase F11. O legado não tem limitação alguma: qualquer cliente com
// a chave certa pode submeter na velocidade que quiser. A chave nasce em ZERO,
// que DESLIGA o middleware — ele nem entra na cadeia (ver httpapi.NovoRouter),
// então com o padrão não existe nem o custo do `if`.

// TextoTaxaExcedida é o corpo da resposta 429.
//
// NÃO é literal do legado — não existe 429 no legado. O texto segue o estilo
// das mensagens existentes: frase curta, em português, sem ponto final.
const TextoTaxaExcedida = "Limite de requisições excedido"

// CabecalhoRetryAfter informa em quantos segundos vale a pena tentar de novo.
//
// A RFC 9110 §10.2.3 admite data ou número de segundos; o número é o que
// clientes de HTTP tratam sem ambiguidade de fuso.
const CabecalhoRetryAfter = "Retry-After"

// intervaloDeColeta é de quanto em quanto tempo os baldes ociosos são
// descartados.
//
// Sem coleta, um atacante que rode chaves inválidas distintas faria o mapa
// crescer sem limite — o limitador viraria o vetor de exaustão de memória que
// ele deveria evitar. Um minuto é folgado o bastante para não descartar o balde
// de um cliente ativo e curto o bastante para o mapa não acumular.
const intervaloDeColeta = time.Minute

// balde é o estado de uma chave: fichas disponíveis e o instante da última
// recarga.
//
// O algoritmo é o de balde de fichas com recarga PREGUIÇOSA: em vez de uma
// goroteina por chave repondo fichas no relógio, a reposição é calculada no
// momento do uso, a partir do tempo decorrido. O resultado observável é o
// mesmo, sem nenhuma goroutine por cliente.
type balde struct {
	fichas    float64
	atualizad time.Time
}

// limitador guarda um balde por chave de API.
type limitador struct {
	rps      float64
	capacid  float64
	agora    func() time.Time
	mu       sync.Mutex
	baldes   map[string]*balde
	ultimaCo time.Time
}

// permitir consome uma ficha da chave, se houver.
//
// Devolve também quanto tempo falta para a próxima ficha, que vira Retry-After.
func (l *limitador) permitir(chave string) (bool, time.Duration) {
	agora := l.agora()

	l.mu.Lock()
	defer l.mu.Unlock()

	l.coletar(agora)

	b, existe := l.baldes[chave]
	if !existe {
		// Um cliente novo começa com o balde CHEIO. Começar vazio faria a
		// primeira requisição de cada cliente ser recusada, que é pior que
		// inútil: nenhum limite razoável rejeita quem ainda não pediu nada.
		b = &balde{fichas: l.capacid, atualizad: agora}
		l.baldes[chave] = b
	}

	// Recarga preguiçosa, limitada à capacidade: fichas não acumulam durante o
	// ócio, senão um cliente parado por uma hora ganharia uma rajada de 3600.
	decorrido := agora.Sub(b.atualizad)
	if decorrido > 0 {
		b.fichas = min(l.capacid, b.fichas+decorrido.Seconds()*l.rps)
		b.atualizad = agora
	}

	if b.fichas >= 1 {
		b.fichas--
		return true, 0
	}

	// Quanto falta para completar UMA ficha.
	faltam := (1 - b.fichas) / l.rps
	return false, time.Duration(faltam * float64(time.Second))
}

// coletar descarta baldes que já voltaram ao cheio e não são usados há um
// intervalo. Roda com a trava tomada.
func (l *limitador) coletar(agora time.Time) {
	if agora.Sub(l.ultimaCo) < intervaloDeColeta {
		return
	}
	l.ultimaCo = agora

	for chave, b := range l.baldes {
		// Um balde que já se recarregou por completo é indistinguível de um
		// cliente novo: descartá-lo não altera decisão alguma.
		if agora.Sub(b.atualizad).Seconds()*l.rps >= l.capacid {
			delete(l.baldes, chave)
		}
	}
}

// LimitarTaxa recusa requisições acima de `rps` por chave de API.
//
// `rps` menor ou igual a zero devolve um middleware TRANSPARENTE — mas quem
// monta a cadeia nem chega a incluí-lo nesse caso; a guarda existe para que
// chamar esta função com zero em um teste não invente comportamento.
//
// # Por que a chave de API, e não o IP
//
// Porque é ela que identifica o cliente do serviço. O IP de origem, atrás de
// balanceador, é o do balanceador — limitaria todo mundo junto. Requisição SEM
// chave cai num balde comum, sob a etiqueta abaixo: ela vai receber 401 de
// qualquer forma, e o que importa é que uma enxurrada de anônimos não consuma o
// balde de ninguém legítimo.
//
// # A chave não vira etiqueta de métrica nem sai em registro
//
// Ela é credencial. O que existe aqui é o mapa em memória, cuja chave nunca é
// exposta — a resposta 429 não diz qual balde estourou.
//
// # Ordem na cadeia
//
// DEPOIS da recuperação de pânico e ANTES do roteamento (ver
// httpapi.NovoRouter): a recusa não deve custar análise de rota nem leitura de
// corpo, e um defeito aqui não pode derrubar o processo.
func LimitarTaxa(rps int, negar Negar) Middleware {
	return limitarTaxaCom(rps, negar, time.Now)
}

// limitarTaxaCom é LimitarTaxa com relógio injetável.
//
// O relógio é parâmetro para que o teste comprove a RECARGA sem dormir: um
// teste que espera de verdade ou é lento ou é instável, e um que só verifica a
// recusa não distingue "limitou" de "travou para sempre".
func limitarTaxaCom(rps int, negar Negar, agora func() time.Time) Middleware {
	if rps <= 0 {
		return func(proximo http.Handler) http.Handler { return proximo }
	}

	l := &limitador{
		rps: float64(rps),
		// A capacidade é o próprio limite por segundo: tolera uma rajada de um
		// segundo de tráfego, que é o que qualquer cliente normal produz ao
		// disparar requisições em paralelo.
		capacid:  float64(rps),
		agora:    agora,
		baldes:   make(map[string]*balde),
		ultimaCo: agora(),
	}

	return func(proximo http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ok, espera := l.permitir(chaveDoBalde(r))
			if ok {
				proximo.ServeHTTP(w, r)
				return
			}

			// Arredonda para CIMA: dizer "1s" quando faltam 1,4s convida o
			// cliente a tentar cedo e levar outro 429.
			segundos := int(espera.Seconds())
			if espera > time.Duration(segundos)*time.Second {
				segundos++
			}
			if segundos < 1 {
				segundos = 1
			}
			w.Header().Set(CabecalhoRetryAfter, strconv.Itoa(segundos))

			negar(w, r, http.StatusTooManyRequests, TextoTaxaExcedida)
		})
	}
}

// baldeAnonimo é a etiqueta interna das requisições sem credencial.
//
// Nunca aparece em resposta nem em registro: é só a chave do mapa.
const baldeAnonimo = "\x00sem-chave"

func chaveDoBalde(r *http.Request) string {
	if v := r.Header.Get(CabecalhoAPIKey); v != "" {
		return v
	}
	return baldeAnonimo
}
