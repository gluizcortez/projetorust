// Package sombra compara o estado final de DOIS bancos — o de produção e o
// espelho da instância em sombra — importação por importação.
//
// # O que a execução em sombra é
//
// O tráfego de produção é duplicado para uma instância Go, que grava em um
// banco ESPELHO. Nada do que ela faz toca o banco real. Depois, este comparador
// confronta as duas linhas de cada importação e responde: o serviço novo teria
// feito a mesma coisa?
//
// # O que este pacote NÃO faz
//
// Não duplica tráfego. A duplicação é infraestrutura — proxy espelho ou
// reprodução de um registro de requisições — e depende de a operação permiti-la,
// que é a decisão aberta **D-14**. Este pacote é a metade que se pode construir
// e testar sem ela: o comparador e a verificação de isolamento.
//
// O procedimento completo, incluindo o que precisa ser montado do outro lado,
// está em docs/RELATORIO-PARIDADE.md.
package sombra

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// Consultador é o mínimo que o comparador precisa de uma conexão.
//
// `*pgxpool.Pool` e `pgx.Conn` o satisfazem. A porta existe para que o teste
// possa injetar um dublê sem banco.
type Consultador interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

// ToleranciaDeTempoPadrao é a diferença aceitável entre os carimbos das duas
// instâncias.
//
// As duas processam o MESMO documento em momentos diferentes: a sombra recebe a
// cópia da requisição depois da original, e o processamento leva o tempo que
// leva. Comparar `data_inicio` e `data_fim` por igualdade exata reprovaria toda
// importação. O que importa é a ORDEM DE GRANDEZA — uma sombra que demore dez
// vezes mais é um achado, cinco segundos de diferença não é.
const ToleranciaDeTempoPadrao = 5 * time.Minute

// LinhaDeImportacao é o estado final de uma importação em um dos bancos.
type LinhaDeImportacao struct {
	ID            int64
	Status        int32
	DataInicio    *time.Time
	DataFim       *time.Time
	TotalRecortes *int32
	// Recortes é o conjunto de (nr_pagina, id_perfil, expressao_busca),
	// ORDENADO — é ele que decide se as duas instâncias acharam a mesma coisa.
	Recortes []ChaveDeRecorte
}

// ChaveDeRecorte identifica um recorte sem depender do identificador gerado.
//
// O `id_recorte` vem de uma sequência e É DIFERENTE nos dois bancos por
// construção; compará-lo reprovaria tudo.
type ChaveDeRecorte struct {
	NrPagina       int64  `json:"nr_pagina"`
	IDPerfil       int64  `json:"id_perfil"`
	ExpressaoBusca string `json:"expressao_busca"`
}

func (c ChaveDeRecorte) String() string {
	return fmt.Sprintf("página %d, perfil %d, %q", c.NrPagina, c.IDPerfil, c.ExpressaoBusca)
}

// Severidade classifica a divergência pelo que ela significa para o corte.
type Severidade string

const (
	// SeveridadeRecorte é a que BARRA o corte: o conteúdo do banco difere.
	SeveridadeRecorte Severidade = "recorte"
	// SeveridadeEstado é divergência de status ou total — também barra, porque
	// muda o que o cliente enxerga.
	SeveridadeEstado Severidade = "estado"
	// SeveridadeTempo é diferença de carimbo além da tolerância. NÃO barra
	// sozinha: as duas instâncias processam em momentos diferentes por
	// construção. É sinal de desempenho, não de correção.
	SeveridadeTempo Severidade = "tempo"
	// SeveridadeAusencia é importação que existe em um banco e não no outro.
	SeveridadeAusencia Severidade = "ausencia"
)

// Divergencia é uma diferença entre os dois bancos.
type Divergencia struct {
	IDImportacao int64      `json:"id_importacao"`
	Severidade   Severidade `json:"severidade"`
	Detalhe      string     `json:"detalhe"`
}

// Resultado é o veredito de uma passagem do comparador.
type Resultado struct {
	Comparadas   int           `json:"comparadas"`
	Identicas    int           `json:"identicas"`
	Divergencias []Divergencia `json:"divergencias"`
}

// Aprovado informa se nada que BARRA o corte apareceu.
//
// Divergência de TEMPO não reprova: ela é esperada por construção e é medida de
// desempenho, não de correção. Recorte, estado e ausência reprovam.
func (r Resultado) Aprovado() bool {
	for _, d := range r.Divergencias {
		if d.Severidade != SeveridadeTempo {
			return false
		}
	}
	return true
}

// Opcoes configura a comparação.
type Opcoes struct {
	// Producao e Espelho são os dois bancos. O de produção é consultado
	// APENAS para leitura — ver VerificarIsolamento.
	Producao, Espelho Consultador

	// Desde limita a janela comparada.
	Desde time.Time

	// ToleranciaDeTempo; zero cai em ToleranciaDeTempoPadrao.
	ToleranciaDeTempo time.Duration
}

// Comparar confronta as importações dos dois bancos.
//
// O pareamento é por `id_importacao`, o que exige que a instância em sombra
// preserve os identificadores — ou seja, que o espelho seja uma CÓPIA do banco
// real no início da janela, e não um banco vazio. É a única forma de parear sem
// heurística; parear por (hash, caderno, data) esbarraria nas duplicatas que o
// legado sempre permitiu.
func Comparar(ctx context.Context, o Opcoes) (Resultado, error) {
	tolerancia := o.ToleranciaDeTempo
	if tolerancia <= 0 {
		tolerancia = ToleranciaDeTempoPadrao
	}

	producao, err := lerImportacoes(ctx, o.Producao, o.Desde)
	if err != nil {
		return Resultado{}, fmt.Errorf("lendo o banco de produção: %w", err)
	}
	espelho, err := lerImportacoes(ctx, o.Espelho, o.Desde)
	if err != nil {
		return Resultado{}, fmt.Errorf("lendo o banco espelho: %w", err)
	}

	var res Resultado

	ids := make([]int64, 0, len(producao))
	for id := range producao {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })

	for _, id := range ids {
		p := producao[id]
		e, existe := espelho[id]
		if !existe {
			res.Divergencias = append(res.Divergencias, Divergencia{
				IDImportacao: id, Severidade: SeveridadeAusencia,
				Detalhe: "existe em produção e não no espelho: a duplicação de tráfego perdeu a requisição",
			})
			continue
		}

		res.Comparadas++
		divergencias := compararLinhas(p, e, tolerancia)
		if len(divergencias) == 0 {
			res.Identicas++
			continue
		}
		res.Divergencias = append(res.Divergencias, divergencias...)
	}

	// O caminho inverso: importação que só existe no espelho é duplicação em
	// excesso — a sombra processou algo que produção não viu.
	for id := range espelho {
		if _, existe := producao[id]; !existe {
			res.Divergencias = append(res.Divergencias, Divergencia{
				IDImportacao: id, Severidade: SeveridadeAusencia,
				Detalhe: "existe no espelho e não em produção: duplicação de tráfego em excesso",
			})
		}
	}

	sort.SliceStable(res.Divergencias, func(i, j int) bool {
		return res.Divergencias[i].IDImportacao < res.Divergencias[j].IDImportacao
	})
	return res, nil
}

// compararLinhas confronta uma importação nos dois bancos.
func compararLinhas(p, e LinhaDeImportacao, tolerancia time.Duration) []Divergencia {
	var saida []Divergencia

	if p.Status != e.Status {
		saida = append(saida, Divergencia{
			IDImportacao: p.ID, Severidade: SeveridadeEstado,
			Detalhe: fmt.Sprintf("status: produção %d, espelho %d", p.Status, e.Status),
		})
	}

	if !mesmoTotal(p.TotalRecortes, e.TotalRecortes) {
		saida = append(saida, Divergencia{
			IDImportacao: p.ID, Severidade: SeveridadeEstado,
			Detalhe: fmt.Sprintf("total_recortes: produção %s, espelho %s",
				descreverTotal(p.TotalRecortes), descreverTotal(e.TotalRecortes)),
		})
	}

	// O CONJUNTO de recortes é o que decide o corte.
	if detalhe := compararRecortes(p.Recortes, e.Recortes); detalhe != "" {
		saida = append(saida, Divergencia{
			IDImportacao: p.ID, Severidade: SeveridadeRecorte, Detalhe: detalhe,
		})
	}

	for _, c := range []struct {
		nome string
		a, b *time.Time
	}{
		{"data_inicio", p.DataInicio, e.DataInicio},
		{"data_fim", p.DataFim, e.DataFim},
	} {
		if d := compararCarimbos(c.nome, c.a, c.b, tolerancia); d != "" {
			saida = append(saida, Divergencia{
				IDImportacao: p.ID, Severidade: SeveridadeTempo, Detalhe: d,
			})
		}
	}

	return saida
}

// compararRecortes confronta os dois CONJUNTOS, nomeando o que falta e o que
// sobra.
//
// "5 recortes contra 4" não diz nada a quem precisa investigar; qual perfil
// deixou de encontrar qual página, sim.
func compararRecortes(producao, espelho []ChaveDeRecorte) string {
	emProducao := map[ChaveDeRecorte]bool{}
	for _, c := range producao {
		emProducao[c] = true
	}
	noEspelho := map[ChaveDeRecorte]bool{}
	for _, c := range espelho {
		noEspelho[c] = true
	}

	var faltando, sobrando []string
	for c := range emProducao {
		if !noEspelho[c] {
			faltando = append(faltando, c.String())
		}
	}
	for c := range noEspelho {
		if !emProducao[c] {
			sobrando = append(sobrando, c.String())
		}
	}
	if len(faltando) == 0 && len(sobrando) == 0 {
		return ""
	}

	sort.Strings(faltando)
	sort.Strings(sobrando)

	var b strings.Builder
	fmt.Fprintf(&b, "recortes: produção %d, espelho %d", len(producao), len(espelho))
	if len(faltando) > 0 {
		fmt.Fprintf(&b, "; FALTANDO no espelho: %s", strings.Join(faltando, " | "))
	}
	if len(sobrando) > 0 {
		fmt.Fprintf(&b, "; SOBRANDO no espelho: %s", strings.Join(sobrando, " | "))
	}
	return b.String()
}

func compararCarimbos(nome string, a, b *time.Time, tolerancia time.Duration) string {
	switch {
	case a == nil && b == nil:
		return ""
	case a == nil || b == nil:
		return fmt.Sprintf("%s: produção %s, espelho %s", nome, descreverCarimbo(a), descreverCarimbo(b))
	}
	diferenca := a.Sub(*b)
	if diferenca < 0 {
		diferenca = -diferenca
	}
	if diferenca <= tolerancia {
		return ""
	}
	return fmt.Sprintf("%s: diferença de %s, acima da tolerância de %s",
		nome, diferenca.Round(time.Second), tolerancia)
}

func descreverCarimbo(t *time.Time) string {
	if t == nil {
		return "nulo"
	}
	return t.Format(time.RFC3339)
}

func mesmoTotal(a, b *int32) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

func descreverTotal(v *int32) string {
	if v == nil {
		return "nulo"
	}
	return fmt.Sprintf("%d", *v)
}

// -------------------------------------------------------------------------
// Leitura
// -------------------------------------------------------------------------

// SQLImportacoes lê o estado final e os recortes em UMA consulta.
//
// A agregação em JSON evita N+1: uma consulta por importação, sobre milhares de
// importações, tornaria a passagem mais lenta que a janela de sombra.
const SQLImportacoes = `
SELECT i.id_importacao, i.status, i.data_inicio, i.data_fim, i.total_recortes,
       coalesce(
           (SELECT json_agg(json_build_object(
                       'nr_pagina', r.nr_pagina,
                       'id_perfil', r.id_perfil,
                       'expressao_busca', r.expressao_busca)
                    ORDER BY r.nr_pagina, r.id_perfil, r.expressao_busca)
            FROM recorte.tb_recorte r
            WHERE r.id_importacao = i.id_importacao),
           '[]'::json)
FROM recorte.tb_importacao i
WHERE i.data_inicio >= $1
ORDER BY i.id_importacao`

// ErrConsultaSemLinhas indica janela vazia — não é erro, mas o chamador precisa
// saber para não relatar "zero divergência" sobre zero importações.
var ErrConsultaSemLinhas = errors.New("nenhuma importação na janela")

func lerImportacoes(
	ctx context.Context, c Consultador, desde time.Time,
) (map[int64]LinhaDeImportacao, error) {
	linhas, err := c.Query(ctx, SQLImportacoes, desde)
	if err != nil {
		return nil, fmt.Errorf("consultando importações: %w", err)
	}
	defer linhas.Close()

	saida := map[int64]LinhaDeImportacao{}
	for linhas.Next() {
		var (
			l        LinhaDeImportacao
			recortes []ChaveDeRecorte
		)
		if err := linhas.Scan(&l.ID, &l.Status, &l.DataInicio, &l.DataFim,
			&l.TotalRecortes, &recortes); err != nil {
			return nil, fmt.Errorf("lendo importação: %w", err)
		}
		l.Recortes = recortes
		saida[l.ID] = l
	}
	if err := linhas.Err(); err != nil {
		return nil, fmt.Errorf("percorrendo importações: %w", err)
	}
	return saida, nil
}
