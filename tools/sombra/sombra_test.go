package sombra

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// O comparador de sombra decide se o serviço vai a produção. Um comparador que
// aprova o que deveria reprovar é a pior peça possível do projeto — pior que
// não ter comparador, porque produz confiança sem base.
//
// Os testes abaixo rodam sem banco: as consultas são substituídas por um dublê,
// o que também prova que o comparador não depende de PostgreSQL para ser
// verificado.

// -------------------------------------------------------------------------
// Dublês
// -------------------------------------------------------------------------

func carimbo(s string) *time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err) //nolint:forbidigo // dado literal do próprio teste
	}
	return &t
}

func total(v int32) *int32 { return &v }

func linha(id int64, status int32, recortes ...ChaveDeRecorte) LinhaDeImportacao {
	return LinhaDeImportacao{
		ID: id, Status: status,
		DataInicio:    carimbo("2026-08-07T10:00:00Z"),
		DataFim:       carimbo("2026-08-07T10:02:00Z"),
		TotalRecortes: total(int32(len(recortes))), //nolint:gosec // literal do teste
		Recortes:      recortes,
	}
}

func recorte(pagina, perfil int64, expressao string) ChaveDeRecorte {
	return ChaveDeRecorte{NrPagina: pagina, IDPerfil: perfil, ExpressaoBusca: expressao}
}

// -------------------------------------------------------------------------
// O veredito
// -------------------------------------------------------------------------

// TestAprovadoIgnoraSoODeTempo fixa a regra de severidade.
//
// Diferença de carimbo é ESPERADA: as duas instâncias processam o mesmo
// documento em momentos diferentes. Reprovar por causa dela reprovaria toda
// execução em sombra e tornaria o portão inútil.
func TestAprovadoIgnoraSoODeTempo(t *testing.T) {
	casos := []struct {
		nome         string
		divergencias []Divergencia
		aprovado     bool
	}{
		{"nada", nil, true},
		{"só tempo", []Divergencia{{Severidade: SeveridadeTempo}}, true},
		{"vários de tempo", []Divergencia{
			{Severidade: SeveridadeTempo}, {Severidade: SeveridadeTempo},
		}, true},
		{"recorte", []Divergencia{{Severidade: SeveridadeRecorte}}, false},
		{"estado", []Divergencia{{Severidade: SeveridadeEstado}}, false},
		{"ausência", []Divergencia{{Severidade: SeveridadeAusencia}}, false},
		{"tempo e recorte", []Divergencia{
			{Severidade: SeveridadeTempo}, {Severidade: SeveridadeRecorte},
		}, false},
	}

	for _, caso := range casos {
		t.Run(caso.nome, func(t *testing.T) {
			r := Resultado{Divergencias: caso.divergencias}
			if r.Aprovado() != caso.aprovado {
				t.Errorf("Aprovado() = %t; esperava %t", r.Aprovado(), caso.aprovado)
			}
		})
	}
}

// -------------------------------------------------------------------------
// Comparação de linhas
// -------------------------------------------------------------------------

func TestLinhasIdenticasNaoDivergem(t *testing.T) {
	r := recorte(1, 7, "ALFA CONSTRUCOES")
	p := linha(42, 5, r)
	e := linha(42, 5, r)

	if d := compararLinhas(p, e, ToleranciaDeTempoPadrao); len(d) != 0 {
		t.Errorf("divergências = %+v; esperava nenhuma", d)
	}
}

// TestRecorteFaltandoENomeado: "5 contra 4" não serve para investigar.
func TestRecorteFaltandoENomeado(t *testing.T) {
	a := recorte(1, 7, "ALFA CONSTRUCOES")
	b := recorte(3, 11, "JOAO SILVA")

	p := linha(42, 5, a, b)
	e := linha(42, 5, a)
	e.TotalRecortes = total(2) // o total bate; só o conjunto difere

	divergencias := compararLinhas(p, e, ToleranciaDeTempoPadrao)
	if len(divergencias) != 1 {
		t.Fatalf("divergências = %+v; esperava 1", divergencias)
	}
	d := divergencias[0]
	if d.Severidade != SeveridadeRecorte {
		t.Errorf("severidade = %q; esperava %q", d.Severidade, SeveridadeRecorte)
	}
	for _, trecho := range []string{"FALTANDO", "página 3", "perfil 11", "JOAO SILVA"} {
		if !strings.Contains(d.Detalhe, trecho) {
			t.Errorf("o detalhe não nomeia %q: %s", trecho, d.Detalhe)
		}
	}
}

// TestRecorteSobrandoENomeado é o outro lado: a sombra achou o que produção não
// achou. É tão grave quanto faltar — significa recorte a mais no cliente.
func TestRecorteSobrandoENomeado(t *testing.T) {
	a := recorte(1, 7, "ALFA CONSTRUCOES")
	b := recorte(2, 7, "BETA CONSTRUCOES")

	p := linha(42, 5, a)
	e := linha(42, 5, a, b)

	divergencias := compararLinhas(p, e, ToleranciaDeTempoPadrao)
	var achou bool
	for _, d := range divergencias {
		if d.Severidade == SeveridadeRecorte {
			achou = true
			if !strings.Contains(d.Detalhe, "SOBRANDO") {
				t.Errorf("o detalhe não diz SOBRANDO: %s", d.Detalhe)
			}
			if !strings.Contains(d.Detalhe, "BETA CONSTRUCOES") {
				t.Errorf("o detalhe não nomeia o recorte a mais: %s", d.Detalhe)
			}
		}
	}
	if !achou {
		t.Errorf("nenhuma divergência de recorte em %+v", divergencias)
	}
}

// TestOrdemDosRecortesNaoImporta.
//
// A ORDEM decide o `id_recorte`, que é diferente nos dois bancos por
// construção. O que se compara aqui é o CONJUNTO — a ordem já foi verificada
// contra o oráculo em test/parity.
func TestOrdemDosRecortesNaoImporta(t *testing.T) {
	a := recorte(1, 7, "ALFA")
	b := recorte(2, 8, "BETA")

	p := linha(42, 5, a, b)
	e := linha(42, 5, b, a)

	if d := compararLinhas(p, e, ToleranciaDeTempoPadrao); len(d) != 0 {
		t.Errorf("divergências = %+v; a ordem não pode reprovar", d)
	}
}

// TestStatusEEstadoDivergem.
func TestStatusEEstadoDivergem(t *testing.T) {
	p := linha(42, 5)
	e := linha(42, -1)
	e.TotalRecortes = nil

	divergencias := compararLinhas(p, e, ToleranciaDeTempoPadrao)
	if len(divergencias) < 2 {
		t.Fatalf("divergências = %+v; esperava status e total", divergencias)
	}
	for _, d := range divergencias {
		if d.Severidade != SeveridadeEstado {
			t.Errorf("severidade = %q; esperava %q", d.Severidade, SeveridadeEstado)
		}
	}
}

// TestTotalNuloEDiferenteDeZero é a distinção de INV-P20: nulo é "não
// terminou", zero é "terminou sem ocorrências".
func TestTotalNuloEDiferenteDeZero(t *testing.T) {
	p := linha(42, 5)
	p.TotalRecortes = total(0)
	e := linha(42, 5)
	e.TotalRecortes = nil

	divergencias := compararLinhas(p, e, ToleranciaDeTempoPadrao)
	if len(divergencias) != 1 || divergencias[0].Severidade != SeveridadeEstado {
		t.Fatalf("divergências = %+v; nulo e zero têm de divergir", divergencias)
	}
	if !strings.Contains(divergencias[0].Detalhe, "nulo") {
		t.Errorf("o detalhe deveria dizer qual lado é nulo: %s", divergencias[0].Detalhe)
	}
}

// TestCarimboDentroDaToleranciaNaoDiverge.
func TestCarimboDentroDaToleranciaNaoDiverge(t *testing.T) {
	p := linha(42, 5)
	e := linha(42, 5)
	e.DataInicio = carimbo("2026-08-07T10:04:00Z") // 4 min depois

	if d := compararLinhas(p, e, ToleranciaDeTempoPadrao); len(d) != 0 {
		t.Errorf("divergências = %+v; 4 min cabe na tolerância de 5", d)
	}
}

// TestCarimboForaDaToleranciaDivergeSemBarrar.
func TestCarimboForaDaToleranciaDivergeSemBarrar(t *testing.T) {
	p := linha(42, 5)
	e := linha(42, 5)
	e.DataFim = carimbo("2026-08-07T11:00:00Z") // ~1 h depois

	divergencias := compararLinhas(p, e, ToleranciaDeTempoPadrao)
	if len(divergencias) != 1 {
		t.Fatalf("divergências = %+v; esperava 1", divergencias)
	}
	if divergencias[0].Severidade != SeveridadeTempo {
		t.Errorf("severidade = %q; esperava %q", divergencias[0].Severidade, SeveridadeTempo)
	}
	// E não barra o corte sozinha.
	if !(Resultado{Divergencias: divergencias}).Aprovado() {
		t.Error("divergência de tempo sozinha não pode reprovar")
	}
}

// TestCarimboNuloDeUmLadoSoDiverge: um lado gravou data_fim e o outro não é
// diferença real, ainda que classificada como tempo.
func TestCarimboNuloDeUmLadoSoDiverge(t *testing.T) {
	p := linha(42, 5)
	e := linha(42, 5)
	e.DataFim = nil

	divergencias := compararLinhas(p, e, ToleranciaDeTempoPadrao)
	if len(divergencias) != 1 {
		t.Fatalf("divergências = %+v; esperava 1", divergencias)
	}
	if !strings.Contains(divergencias[0].Detalhe, "nulo") {
		t.Errorf("o detalhe deveria dizer qual lado é nulo: %s", divergencias[0].Detalhe)
	}
}

// -------------------------------------------------------------------------
// Isolamento
// -------------------------------------------------------------------------

// executorFalso simula uma credencial: recusa ou aceita a escrita.
type executorFalso struct {
	escreve    bool
	tentativas []string
}

func (e *executorFalso) Exec(_ context.Context, sql string, _ ...any) (comandoExecutado, error) {
	e.tentativas = append(e.tentativas, sql)
	if e.escreve {
		return nil, nil //nolint:nilnil // o dublê só precisa sinalizar sucesso
	}
	return nil, errors.New("permission denied for table")
}

// TestIsolamentoAprovaCredencialSomenteLeitura.
func TestIsolamentoAprovaCredencialSomenteLeitura(t *testing.T) {
	e := &executorFalso{escreve: false}

	if err := VerificarIsolamento(context.Background(), e); err != nil {
		t.Errorf("VerificarIsolamento reprovou uma credencial de leitura: %v", err)
	}

	// E TENTOU de verdade, nas três tabelas — uma verificação que não tenta
	// nada aprovaria qualquer credencial.
	if len(e.tentativas) != len(tabelasProtegidas) {
		t.Errorf("tentativas = %d; esperava %d", len(e.tentativas), len(tabelasProtegidas))
	}
	for _, tabela := range tabelasProtegidas {
		var achou bool
		for _, sql := range e.tentativas {
			if strings.Contains(sql, tabela) {
				achou = true
			}
		}
		if !achou {
			t.Errorf("a tabela %s não foi testada", tabela)
		}
	}
}

// TestIsolamentoReprovaCredencialDeEscrita é o teste que importa: ele é o único
// que impede a sombra de escrever no banco de produção.
func TestIsolamentoReprovaCredencialDeEscrita(t *testing.T) {
	e := &executorFalso{escreve: true}

	err := VerificarIsolamento(context.Background(), e)
	if err == nil {
		t.Fatal("VerificarIsolamento aprovou uma credencial que ESCREVE em produção")
	}
	if !errors.Is(err, ErrIsolamentoViolado) {
		t.Errorf("erro = %v; esperava ErrIsolamentoViolado", err)
	}
	// A mensagem tem de nomear as tabelas: o operador precisa saber o que
	// revogar.
	for _, tabela := range tabelasProtegidas {
		if !strings.Contains(err.Error(), tabela) {
			t.Errorf("a mensagem não nomeia %s: %v", tabela, err)
		}
	}
}

// TestIsolamentoCobreAsTresTabelasEscritas.
//
// Se o serviço passar a escrever numa quarta tabela, esta lista precisa crescer
// junto — e este teste é o lembrete.
func TestIsolamentoCobreAsTresTabelasEscritas(t *testing.T) {
	esperadas := []string{
		"recorte.tb_importacao",
		"recorte.tb_recorte",
		"recorte.tb_recorte_texto",
	}
	if len(tabelasProtegidas) != len(esperadas) {
		t.Fatalf("tabelas protegidas = %v; esperava %v", tabelasProtegidas, esperadas)
	}
	for i, e := range esperadas {
		if tabelasProtegidas[i] != e {
			t.Errorf("posição %d = %q; esperava %q", i, tabelasProtegidas[i], e)
		}
	}
	if !strings.Contains(ResumoDoIsolamento(), "tb_recorte_texto") {
		t.Error("o resumo não descreve o que foi verificado")
	}
}
