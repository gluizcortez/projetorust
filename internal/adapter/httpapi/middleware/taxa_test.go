package middleware

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

// O limitador é testado com relógio INJETADO. Um teste que dormisse de verdade
// seria lento e instável, e um que só verificasse a recusa não distinguiria
// "limitou" de "travou para sempre" — a recarga é a metade do comportamento que
// importa.

// relogioFalso avança só quando mandado.
type relogioFalso struct {
	mu    sync.Mutex
	agora time.Time
}

func novoRelogioFalso() *relogioFalso {
	return &relogioFalso{agora: time.Date(2024, 3, 15, 12, 0, 0, 0, time.UTC)}
}

func (r *relogioFalso) Agora() time.Time {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.agora
}

func (r *relogioFalso) Avancar(d time.Duration) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.agora = r.agora.Add(d)
}

// pilhaLimitada monta o limitador sobre um manipulador que sempre responde 200.
func pilhaLimitada(rps int, relogio *relogioFalso) http.Handler {
	negar := func(w http.ResponseWriter, _ *http.Request, codigo int, texto string) {
		w.WriteHeader(codigo)
		_, _ = w.Write([]byte(texto))
	}
	ok := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	return limitarTaxaCom(rps, negar, relogio.Agora)(ok)
}

// bater dispara n requisições com a chave dada e devolve quantas passaram.
func bater(h http.Handler, chave string, n int) (aprovadas, recusadas int) {
	for range n {
		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodGet, "/ping", nil)
		if chave != "" {
			r.Header.Set(CabecalhoAPIKey, chave)
		}
		h.ServeHTTP(w, r)
		if w.Code == http.StatusTooManyRequests {
			recusadas++
		} else {
			aprovadas++
		}
	}
	return aprovadas, recusadas
}

// TestTaxaZeroEhTransparente: o padrão não recusa nada, nem instala estado.
func TestTaxaZeroEhTransparente(t *testing.T) {
	for _, rps := range []int{0, -1} {
		relogio := novoRelogioFalso()
		aprovadas, recusadas := bater(pilhaLimitada(rps, relogio), "cliente", 1000)
		if recusadas != 0 {
			t.Errorf("rps=%d recusou %d de 1000", rps, recusadas)
		}
		if aprovadas != 1000 {
			t.Errorf("rps=%d aprovou %d de 1000", rps, aprovadas)
		}
	}
}

// TestBaldeComecaCheio: o primeiro pedido de um cliente novo nunca é recusado.
func TestBaldeComecaCheio(t *testing.T) {
	relogio := novoRelogioFalso()
	h := pilhaLimitada(10, relogio)

	aprovadas, recusadas := bater(h, "novo", 10)
	if aprovadas != 10 || recusadas != 0 {
		t.Errorf("aprovadas=%d recusadas=%d; o balde nasce cheio", aprovadas, recusadas)
	}
	if _, recusadas := bater(h, "novo", 1); recusadas != 1 {
		t.Error("a 11ª deveria ser recusada sem o relógio avançar")
	}
}

// TestRecargaLiberaNaProporcao é o teste que o relógio injetado torna possível.
//
// Meio segundo a 10 rps repõe exatamente 5 fichas — nem 4, nem 6.
func TestRecargaLiberaNaProporcao(t *testing.T) {
	relogio := novoRelogioFalso()
	h := pilhaLimitada(10, relogio)

	bater(h, "c", 10) // esgota

	relogio.Avancar(500 * time.Millisecond)

	aprovadas, _ := bater(h, "c", 10)
	if aprovadas != 5 {
		t.Errorf("aprovadas = %d; meio segundo a 10 rps repõe 5 fichas", aprovadas)
	}
}

// TestFichasNaoAcumulamAlemDaCapacidade: ócio longo não vira rajada.
//
// Sem o teto, um cliente parado por uma hora ganharia 36 000 fichas a 10 rps e
// derrubaria o serviço na volta — o oposto do que o limitador existe para fazer.
func TestFichasNaoAcumulamAlemDaCapacidade(t *testing.T) {
	relogio := novoRelogioFalso()
	h := pilhaLimitada(10, relogio)

	bater(h, "c", 10)
	relogio.Avancar(time.Hour)

	aprovadas, recusadas := bater(h, "c", 50)
	if aprovadas != 10 {
		t.Errorf("aprovadas = %d; a capacidade é 10, por mais longo que seja o ócio", aprovadas)
	}
	if recusadas != 40 {
		t.Errorf("recusadas = %d; esperava 40", recusadas)
	}
}

// TestBaldesSaoIndependentesPorChave.
func TestBaldesSaoIndependentesPorChave(t *testing.T) {
	relogio := novoRelogioFalso()
	h := pilhaLimitada(3, relogio)

	bater(h, "barulhento", 20)

	if aprovadas, _ := bater(h, "quieto", 3); aprovadas != 3 {
		t.Errorf("aprovadas = %d; o balde de outro cliente não pode afetar este", aprovadas)
	}
}

// TestRequisicoesSemChaveDividemUmBalde.
//
// Elas vão receber 401 de qualquer forma; o que importa é que uma enxurrada de
// anônimos não consuma o balde de ninguém legítimo — e não que cada uma ganhe
// balde próprio, o que faria o limitador não limitar nada.
func TestRequisicoesSemChaveDividemUmBalde(t *testing.T) {
	relogio := novoRelogioFalso()
	h := pilhaLimitada(3, relogio)

	aprovadas, recusadas := bater(h, "", 10)
	if aprovadas != 3 {
		t.Errorf("aprovadas = %d; esperava 3", aprovadas)
	}
	if recusadas != 7 {
		t.Errorf("recusadas = %d; esperava 7", recusadas)
	}

	if aprovadas, _ := bater(h, "legitimo", 3); aprovadas != 3 {
		t.Error("a enxurrada anônima não pode consumir o balde de quem tem chave")
	}
}

// TestRetryAfterArredondaParaCima.
//
// Dizer "1s" quando faltam 1,4s convida o cliente a tentar cedo e levar outro
// 429. E "0" é pior ainda: é um convite a girar em laço.
func TestRetryAfterArredondaParaCima(t *testing.T) {
	relogio := novoRelogioFalso()
	// 1 rps: esgotada a única ficha, falta 1 segundo inteiro para a próxima.
	h := pilhaLimitada(1, relogio)

	bater(h, "c", 1)

	// Avança 200 ms: faltam 800 ms, que precisam virar "1", não "0".
	relogio.Avancar(200 * time.Millisecond)

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/ping", nil)
	r.Header.Set(CabecalhoAPIKey, "c")
	h.ServeHTTP(w, r)

	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d; esperava 429", w.Code)
	}
	if v := w.Header().Get(CabecalhoRetryAfter); v != "1" {
		t.Errorf("Retry-After = %q; 800 ms restantes têm de virar \"1\"", v)
	}
}

// TestColetaDescartaBaldesOciosos evita que chaves distintas façam o mapa
// crescer sem limite — o limitador viraria o vetor de exaustão de memória que
// ele deveria evitar.
func TestColetaDescartaBaldesOciosos(t *testing.T) {
	relogio := novoRelogioFalso()
	// O limitador é exercitado DIRETO, sem passar por HTTP: o que se observa
	// aqui é o tamanho do mapa, que não é visível pela resposta.
	l := &limitador{
		rps:      10,
		capacid:  10,
		agora:    relogio.Agora,
		baldes:   make(map[string]*balde),
		ultimaCo: relogio.Agora(),
	}

	for i := range 500 {
		l.permitir("chave-" + string(rune('a'+i%26)) + string(rune('a'+i/26)))
	}
	if len(l.baldes) == 0 {
		t.Fatal("nenhum balde foi criado")
	}
	antes := len(l.baldes)

	// Passado o intervalo, todos já se recarregaram por completo e são
	// indistinguíveis de clientes novos.
	relogio.Avancar(2 * intervaloDeColeta)
	l.permitir("mais-uma")

	if len(l.baldes) >= antes {
		t.Errorf("baldes = %d antes, %d depois da coleta; a coleta não descartou nada",
			antes, len(l.baldes))
	}
	// A que acabou de ser usada precisa sobreviver.
	if _, existe := l.baldes["mais-uma"]; !existe {
		t.Error("a coleta descartou o balde em uso")
	}
}

// TestLimitadorSuportaUsoConcorrente roda sob -race.
func TestLimitadorSuportaUsoConcorrente(t *testing.T) {
	relogio := novoRelogioFalso()
	h := pilhaLimitada(50, relogio)

	var grupo sync.WaitGroup
	for i := range 20 {
		grupo.Add(1)
		go func(i int) {
			defer grupo.Done()
			bater(h, "chave-"+string(rune('a'+i)), 20)
		}(i)
	}
	grupo.Wait()
}
