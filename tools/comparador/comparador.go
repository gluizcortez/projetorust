// Package comparador confronta a implementação Go com o corpus dourado
// capturado do legado, nas CINCO camadas da fase F12.
//
// # Por que cinco camadas, e não uma
//
// Porque as camadas LOCALIZAM A CAUSA. Uma divergência de recorte pode vir de
// um caractere extraído errado, de uma normalização diferente, de um termo
// tokenizado de outro jeito ou do laço de deduplicação. Comparar só o resultado
// final diz que há um problema; comparar as cinco diz ONDE ele está.
//
//	1  texto extraído por página          antes da normalização
//	2  texto normalizado por página       junção de hífens + diacríticos
//	3  termos do índice por página        tokenização
//	4  conjunto e ORDEM dos recortes      laço, deduplicação, gravação
//	5  desfecho e sequência de status     máquina de estados
//
// # A costura é o ponto cego
//
// Cada camada é alimentada pelo ORÁCULO da anterior, não pela saída da anterior
// em Go — é o que isola a medição. O preço é que a COSTURA entre camadas não é
// medida por camada nenhuma, e foi exatamente ali que a fase F12 encontrou o
// defeito mais caro do projeto: a normalização não era aplicada em produção, e
// as camadas 1, 2 e 3 continuavam verdes.
//
// Por isso as camadas 4 e 5 são executadas de forma DIFERENTE: elas rodam o
// pipeline REAL, de ponta a ponta, a partir do PDF. É a única parte do
// comparador que mede a montagem, e não as peças.
package comparador

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gluizcortez/projetorust/internal/adapter/pdftext"
	"github.com/gluizcortez/projetorust/internal/adapter/searchidx"
	"github.com/gluizcortez/projetorust/internal/domain"
	"github.com/gluizcortez/projetorust/internal/usecase"
)

// Camada identifica em que estágio a divergência apareceu.
type Camada string

// As cinco camadas, na ordem do pipeline.
const (
	CamadaExtracao     Camada = "1-extracao"
	CamadaNormalizacao Camada = "2-normalizacao"
	CamadaTermos       Camada = "3-termos"
	CamadaRecortes     Camada = "4-recortes"
	CamadaEstados      Camada = "5-estados"
)

// TodasAsCamadas na ordem em que são executadas.
var TodasAsCamadas = []Camada{
	CamadaExtracao, CamadaNormalizacao, CamadaTermos, CamadaRecortes, CamadaEstados,
}

// Classe agrupa divergências que têm a mesma causa provável.
//
// Existe para que o relatório não liste mil linhas iguais: mil páginas com o
// mesmo hífen não tratado são UMA classe, com um caso reprodutor.
type Classe string

// Classes conhecidas. `ClasseDesconhecida` é o balde do que não se encaixa —
// e uma divergência que caia nele merece uma classe nova.
const (
	ClasseHifen        Classe = "hifen-fim-de-linha"
	ClasseDiacritico   Classe = "diacritico"
	ClasseEspaco       Classe = "espaco"
	ClasseQuantidade   Classe = "quantidade"
	ClasseOrdem        Classe = "ordem"
	ClasseTexto        Classe = "texto"
	ClasseDesfecho     Classe = "desfecho"
	ClasseFalhaDeExec  Classe = "falha-de-execucao"
	ClasseDesconhecida Classe = "desconhecida"
)

// Divergencia é uma diferença entre o obtido e o oráculo.
type Divergencia struct {
	Camada    Camada `json:"camada"`
	Classe    Classe `json:"classe"`
	Documento string `json:"documento"`
	// Pagina vale zero quando a divergência não é de página — a de desfecho,
	// por exemplo.
	Pagina int `json:"pagina,omitempty"`
	// Detalhe é a descrição legível.
	Detalhe string `json:"detalhe"`
	// MenorCaso é o resultado da redução automática: o menor trecho que ainda
	// reproduz a diferença. Vazio quando a redução não se aplica.
	MenorCaso *MenorCaso `json:"menor_caso,omitempty"`
}

// MenorCaso é o reprodutor reduzido de uma divergência de texto.
type MenorCaso struct {
	// Linha é o índice, a partir de 1, dentro da página.
	Linha int `json:"linha"`
	// Esperado e Obtido são a LINHA inteira, não a página.
	Esperado string `json:"esperado"`
	Obtido   string `json:"obtido"`
	// Runa é a posição da primeira diferença dentro da linha.
	Runa int `json:"runa"`
	// PontoEsperado e PontoObtido nomeiam os pontos de código. Nomeá-los é o
	// que distingue um caractere invisível de outro: espaço estreito e espaço
	// comum imprimem igual e não são o mesmo.
	PontoEsperado string `json:"ponto_esperado"`
	PontoObtido   string `json:"ponto_obtido"`
}

// ResumoDeCamada conta o que foi comparado em uma camada.
type ResumoDeCamada struct {
	Camada Camada `json:"camada"`
	// Comparados é o denominador: páginas, termos ou recortes, conforme a
	// camada.
	Comparados int `json:"comparados"`
	Iguais     int `json:"iguais"`
	// Divergentes é o numerador da taxa.
	Divergentes int `json:"divergentes"`
	// PorClasse conta as divergências por classe.
	PorClasse map[Classe]int `json:"por_classe,omitempty"`
	// Unidade nomeia o que foi contado, para o relatório em texto.
	Unidade string `json:"unidade"`
}

// Taxa é a fração divergente, de 0 a 1.
func (r ResumoDeCamada) Taxa() float64 {
	if r.Comparados == 0 {
		return 0
	}
	return float64(r.Divergentes) / float64(r.Comparados)
}

// Relatorio é a saída do comparador.
type Relatorio struct {
	GeradoEm   time.Time        `json:"gerado_em"`
	DirCorpus  string           `json:"dir_corpus"`
	Documentos int              `json:"documentos"`
	Camadas    []ResumoDeCamada `json:"camadas"`
	// Divergencias traz UMA por classe e por camada — o representante com o
	// menor caso reprodutor. O total está em ResumoDeCamada.
	Divergencias []Divergencia `json:"divergencias"`
	// PortaoDeCorte é o critério de saída da fase: zero divergência de
	// recortes. As demais camadas informam, mas não barram — uma diferença de
	// termo que não muda recorte algum não altera o banco.
	PortaoDeCorte bool `json:"portao_de_corte"`
}

// Aprovado informa se o critério de corte foi atendido.
//
// O critério é ZERO DIVERGÊNCIA DE RECORTES, camadas 4 e 5. As camadas 1 a 3
// são diagnósticas: elas localizam a causa, e uma divergência ali que não
// chegue a mudar recorte não altera o conteúdo do banco.
func (r Relatorio) Aprovado() bool {
	for _, c := range r.Camadas {
		if (c.Camada == CamadaRecortes || c.Camada == CamadaEstados) && c.Divergentes > 0 {
			return false
		}
	}
	return true
}

// -------------------------------------------------------------------------
// Execução
// -------------------------------------------------------------------------

// Opcoes configura a execução.
type Opcoes struct {
	DirCorpus   string
	DirEsperado string
}

// Executar roda as cinco camadas sobre o corpus inteiro.
func Executar(ctx context.Context, o Opcoes) (Relatorio, error) {
	rel := Relatorio{GeradoEm: time.Now().UTC(), DirCorpus: o.DirCorpus}

	brutos, err := carregarPaginas(o.DirEsperado, ".paginas-brutas.json")
	if err != nil {
		return rel, err
	}
	normalizados, err := carregarPaginas(o.DirEsperado, ".paginas.json")
	if err != nil {
		return rel, err
	}
	termos, err := carregarTermos(o.DirEsperado)
	if err != nil {
		return rel, err
	}
	recortes, err := carregarRecortes(o.DirEsperado)
	if err != nil {
		return rel, err
	}
	chaves, err := carregarChaves(o.DirCorpus)
	if err != nil {
		return rel, err
	}

	// O corpus INTEIRO é o que tem oráculo de recorte: dois documentos —
	// `21-vazio` e `22-nao-e-pdf` — falham na extração de propósito e por isso
	// não têm oráculo de páginas. Contar por `brutos` esconderia justamente os
	// dois casos de entrada inválida.
	rel.Documentos = len(recortes)

	acumulador := novoAcumulador()

	compararExtracao(ctx, o.DirCorpus, brutos, acumulador)
	compararNormalizacao(brutos, normalizados, acumulador)
	compararTermos(ctx, normalizados, termos, acumulador)
	compararPipeline(ctx, o.DirCorpus, recortes, chaves, acumulador)

	rel.Camadas = acumulador.resumos()
	rel.Divergencias = acumulador.representantes()
	rel.PortaoDeCorte = rel.Aprovado()
	return rel, nil
}

// acumulador junta os resultados das camadas.
//
// Guarda UM representante por (camada, classe): o relatório precisa do menor
// caso reprodutor de cada classe, não de mil linhas com a mesma causa.
type acumulador struct {
	mu       sync.Mutex
	resumo   map[Camada]*ResumoDeCamada
	primeiro map[string]Divergencia
	ordem    []string
}

func novoAcumulador() *acumulador {
	return &acumulador{
		resumo:   map[Camada]*ResumoDeCamada{},
		primeiro: map[string]Divergencia{},
	}
}

func (a *acumulador) contar(c Camada, unidade string, iguais, divergentes int) {
	a.mu.Lock()
	defer a.mu.Unlock()
	r, existe := a.resumo[c]
	if !existe {
		r = &ResumoDeCamada{Camada: c, Unidade: unidade, PorClasse: map[Classe]int{}}
		a.resumo[c] = r
	}
	r.Iguais += iguais
	r.Divergentes += divergentes
	r.Comparados += iguais + divergentes
}

func (a *acumulador) anotar(d Divergencia) {
	a.mu.Lock()
	defer a.mu.Unlock()

	r, existe := a.resumo[d.Camada]
	if !existe {
		r = &ResumoDeCamada{Camada: d.Camada, PorClasse: map[Classe]int{}}
		a.resumo[d.Camada] = r
	}
	r.PorClasse[d.Classe]++

	chave := string(d.Camada) + "/" + string(d.Classe)
	if _, jaTem := a.primeiro[chave]; !jaTem {
		a.primeiro[chave] = d
		a.ordem = append(a.ordem, chave)
	}
}

func (a *acumulador) resumos() []ResumoDeCamada {
	a.mu.Lock()
	defer a.mu.Unlock()

	saida := make([]ResumoDeCamada, 0, len(TodasAsCamadas))
	for _, c := range TodasAsCamadas {
		if r, existe := a.resumo[c]; existe {
			saida = append(saida, *r)
		}
	}
	return saida
}

func (a *acumulador) representantes() []Divergencia {
	a.mu.Lock()
	defer a.mu.Unlock()

	saida := make([]Divergencia, 0, len(a.ordem))
	for _, chave := range a.ordem {
		saida = append(saida, a.primeiro[chave])
	}
	return saida
}

// -------------------------------------------------------------------------
// Camadas 1 a 3
// -------------------------------------------------------------------------

func compararExtracao(ctx context.Context, dirCorpus string, brutos []paginasOraculo, ac *acumulador) {
	extrator := pdftext.NovoExtrator()

	for _, esp := range brutos {
		conteudo, err := os.ReadFile(filepath.Join(dirCorpus, esp.Documento+".pdf")) //nolint:gosec // caminho derivado do oráculo
		if err != nil {
			ac.anotar(Divergencia{
				Camada: CamadaExtracao, Classe: ClasseFalhaDeExec,
				Documento: esp.Documento, Detalhe: "lendo o PDF: " + err.Error(),
			})
			continue
		}

		obtidas, err := extrator.ExtrairPaginas(ctx, conteudo)
		if err != nil {
			// Documentos inválidos do corpus falham DE PROPÓSITO, e o oráculo
			// registra zero páginas. Só é divergência se o oráculo esperava
			// páginas.
			if len(esp.Paginas) > 0 {
				ac.anotar(Divergencia{
					Camada: CamadaExtracao, Classe: ClasseFalhaDeExec,
					Documento: esp.Documento, Detalhe: "ExtrairPaginas: " + err.Error(),
				})
			}
			continue
		}
		compararPaginas(CamadaExtracao, esp, obtidas, ac)
	}
}

func compararNormalizacao(brutos, normalizados []paginasOraculo, ac *acumulador) {
	porDocumento := map[string]paginasOraculo{}
	for _, b := range brutos {
		porDocumento[b.Documento] = b
	}

	for _, esp := range normalizados {
		bruto, existe := porDocumento[esp.Documento]
		if !existe {
			continue
		}
		obtidas := make([]string, len(bruto.Paginas))
		for i, p := range bruto.Paginas {
			obtidas[i] = pdftext.Normalizar(p)
		}
		compararPaginas(CamadaNormalizacao, esp, obtidas, ac)
	}
}

func compararTermos(ctx context.Context, normalizados []paginasOraculo, termos []termosOraculo, ac *acumulador) {
	porDocumento := map[string]paginasOraculo{}
	for _, n := range normalizados {
		porDocumento[n.Documento] = n
	}

	for _, esp := range termos {
		pagina, existe := porDocumento[esp.Documento]
		if !existe {
			continue
		}
		for i, esperados := range esp.TermosPorPagina {
			if i >= len(pagina.Paginas) {
				break
			}
			obtidos := searchidx.Tokenizar(pagina.Paginas[i])
			if igualSequencia(esperados, obtidos) {
				ac.contar(CamadaTermos, "páginas", 1, 0)
				continue
			}
			ac.contar(CamadaTermos, "páginas", 0, 1)
			ac.anotar(Divergencia{
				Camada: CamadaTermos, Classe: classeDeTermos(esperados, obtidos),
				Documento: esp.Documento, Pagina: i + 1,
				Detalhe: fmt.Sprintf("termos: esperados %d, obtidos %d; primeira diferença em %s",
					len(esperados), len(obtidos), primeiroTermoDivergente(esperados, obtidos)),
			})
		}
	}
}

// compararPaginas é o corpo comum das camadas 1 e 2.
func compararPaginas(camada Camada, esp paginasOraculo, obtidas []string, ac *acumulador) {
	if len(esp.Paginas) != len(obtidas) {
		ac.contar(camada, "páginas", 0, 1)
		ac.anotar(Divergencia{
			Camada: camada, Classe: ClasseQuantidade, Documento: esp.Documento,
			Detalhe: fmt.Sprintf("páginas: esperadas %d, obtidas %d", len(esp.Paginas), len(obtidas)),
		})
		return
	}

	for i := range esp.Paginas {
		if esp.Paginas[i] == obtidas[i] {
			ac.contar(camada, "páginas", 1, 0)
			continue
		}
		ac.contar(camada, "páginas", 0, 1)
		menor := reduzir(esp.Paginas[i], obtidas[i])
		ac.anotar(Divergencia{
			Camada: camada, Classe: classeDeTexto(menor), Documento: esp.Documento,
			Pagina: i + 1, Detalhe: "texto da página divergente", MenorCaso: menor,
		})
	}
}

// -------------------------------------------------------------------------
// Camadas 4 e 5 — o pipeline REAL, a partir do PDF
// -------------------------------------------------------------------------

func compararPipeline(
	ctx context.Context, dirCorpus string,
	esperados []recortesOraculo, chaves []domain.ChavePesquisa, ac *acumulador,
) {
	for _, esp := range esperados {
		conteudo, err := os.ReadFile(filepath.Join(dirCorpus, esp.Documento+".pdf")) //nolint:gosec // caminho derivado do oráculo
		if err != nil {
			ac.anotar(Divergencia{
				Camada: CamadaRecortes, Classe: ClasseFalhaDeExec,
				Documento: esp.Documento, Detalhe: "lendo o PDF: " + err.Error(),
			})
			continue
		}

		obtido := ExecutarPipeline(ctx, conteudo, chaves)
		compararCamada4(esp, obtido, ac)
		compararCamada5(esp, obtido, ac)
	}
}

func compararCamada4(esp recortesOraculo, obtido ResultadoDoPipeline, ac *acumulador) {
	if len(esp.Recortes) != len(obtido.Recortes) {
		ac.contar(CamadaRecortes, "recortes", 0, 1)
		ac.anotar(Divergencia{
			Camada: CamadaRecortes, Classe: ClasseQuantidade, Documento: esp.Documento,
			Detalhe: fmt.Sprintf("recortes: esperados %d, obtidos %d",
				len(esp.Recortes), len(obtido.Recortes)),
		})
		return
	}

	for i := range esp.Recortes {
		e, o := esp.Recortes[i], obtido.Recortes[i]
		switch {
		case e.IDPerfil != o.IDPerfil || e.ExpressaoBusca != o.ExpressaoBusca || e.NrPagina != o.NrPagina:
			ac.contar(CamadaRecortes, "recortes", 0, 1)
			ac.anotar(Divergencia{
				Camada: CamadaRecortes, Classe: ClasseOrdem, Documento: esp.Documento,
				Pagina: int(e.NrPagina),
				Detalhe: fmt.Sprintf(
					"recorte %d: esperado (perfil %d, %q, página %d), obtido (perfil %d, %q, página %d)",
					i, e.IDPerfil, e.ExpressaoBusca, e.NrPagina,
					o.IDPerfil, o.ExpressaoBusca, o.NrPagina),
			})
		case e.TextoSHA256 != o.TextoSHA256:
			ac.contar(CamadaRecortes, "recortes", 0, 1)
			menor := reduzir(e.Texto, o.Texto)
			ac.anotar(Divergencia{
				Camada: CamadaRecortes, Classe: classeDeTexto(menor), Documento: esp.Documento,
				Pagina: int(e.NrPagina),
				Detalhe: fmt.Sprintf("recorte %d (perfil %d, %q): texto divergente",
					i, e.IDPerfil, e.ExpressaoBusca),
				MenorCaso: menor,
			})
		default:
			ac.contar(CamadaRecortes, "recortes", 1, 0)
		}
	}
}

func compararCamada5(esp recortesOraculo, obtido ResultadoDoPipeline, ac *acumulador) {
	esperada := SequenciaEsperada(esp.Desfecho)
	if !igualStatus(esperada, obtido.Status) {
		ac.contar(CamadaEstados, "documentos", 0, 1)
		ac.anotar(Divergencia{
			Camada: CamadaEstados, Classe: ClasseDesfecho, Documento: esp.Documento,
			Detalhe: fmt.Sprintf("sequência de status: esperada %v, obtida %v (desfecho %q)",
				esperada, obtido.Status, esp.Desfecho),
		})
		return
	}

	if esp.Desfecho == "finalizado" {
		if len(obtido.Terminos) != 1 || int(obtido.Terminos[0]) != esp.TotalRecortes {
			ac.contar(CamadaEstados, "documentos", 0, 1)
			ac.anotar(Divergencia{
				Camada: CamadaEstados, Classe: ClasseDesfecho, Documento: esp.Documento,
				Detalhe: fmt.Sprintf("total_recortes: esperado %d, obtido %v",
					esp.TotalRecortes, obtido.Terminos),
			})
			return
		}
	} else if len(obtido.Terminos) != 0 {
		ac.contar(CamadaEstados, "documentos", 0, 1)
		ac.anotar(Divergencia{
			Camada: CamadaEstados, Classe: ClasseDesfecho, Documento: esp.Documento,
			Detalhe: fmt.Sprintf("MarcarTermino chamado no desfecho %q", esp.Desfecho),
		})
		return
	}

	ac.contar(CamadaEstados, "documentos", 1, 0)
}

// SequenciaEsperada deriva a sequência de status do desfecho capturado.
//
// Vem de docs/ESPECIFICACAO.md §3.3 e das gravações de reference/main.rs:261,
// 268, 272, 320, 325 e 334. NÃO inclui o status 0 do caminho síncrono: ele é
// gravado pela ingestão, não pelo pipeline.
func SequenciaEsperada(desfecho string) []domain.StatusImportacao {
	base := []domain.StatusImportacao{domain.StatusSelecionado, domain.StatusIndexando}
	switch desfecho {
	case "erro_indexacao":
		return append(base, domain.StatusErro)
	case "erro_recorte":
		return append(base, domain.StatusRecortando, domain.StatusErro)
	default:
		return append(base, domain.StatusRecortando, domain.StatusFinalizado)
	}
}

// -------------------------------------------------------------------------
// Execução do pipeline real
// -------------------------------------------------------------------------

// RecorteGravado é uma linha na ordem exata em que seria gravada.
type RecorteGravado struct {
	Ordem          int    `json:"ordem"`
	IDPerfil       int64  `json:"id_perfil"`
	ExpressaoBusca string `json:"expressao_busca"`
	NrPagina       int64  `json:"nr_pagina"`
	TextoSHA256    string `json:"texto_sha256"`
	Texto          string `json:"texto"`
}

// ResultadoDoPipeline é o que a execução em Go produziu.
type ResultadoDoPipeline struct {
	Recortes []RecorteGravado
	Status   []domain.StatusImportacao
	Terminos []int32
	Inicios  int
}

// ExecutarPipeline roda o pipeline REAL sobre um documento.
//
// Usa `pdftext.NovoExtratorNormalizado` — o extrator de PRODUÇÃO. Usar o cru
// aqui reproduziria o defeito que este comparador existe para pegar.
func ExecutarPipeline(
	ctx context.Context, conteudo []byte, chaves []domain.ChavePesquisa,
) ResultadoDoPipeline {
	registro := &registroDeGravacao{}

	pipeline, err := usecase.NovoPipeline(usecase.DependenciasDoPipeline{
		Importacoes: registro,
		Perfis:      chavesFixas{chaves: chaves},
		Recortes:    registro,
		Extrator:    pdftext.NovoExtratorNormalizado(),
		Indexador:   searchidx.NovoIndexador(),
		Relogio:     relogioParado{},
		Logger:      slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		// Inalcançável: todas as portas acima são não nulas.
		return ResultadoDoPipeline{}
	}

	pipeline.Processar(ctx, 1, conteudo)

	registro.mu.Lock()
	defer registro.mu.Unlock()
	return ResultadoDoPipeline{
		Recortes: append([]RecorteGravado(nil), registro.recortes...),
		Status:   append([]domain.StatusImportacao(nil), registro.status...),
		Terminos: append([]int32(nil), registro.terminos...),
		Inicios:  registro.inicios,
	}
}

type registroDeGravacao struct {
	mu       sync.Mutex
	recortes []RecorteGravado
	status   []domain.StatusImportacao
	inicios  int
	terminos []int32
}

func (r *registroDeGravacao) Registrar(context.Context, domain.Importacao) (int64, error) {
	return 1, nil
}

func (r *registroDeGravacao) AtualizarStatus(_ context.Context, _ int64, s domain.StatusImportacao) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.status = append(r.status, s)
	return nil
}

func (r *registroDeGravacao) MarcarInicio(context.Context, int64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.inicios++
	return nil
}

func (r *registroDeGravacao) MarcarTermino(_ context.Context, _ int64, total int32) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.terminos = append(r.terminos, total)
	return nil
}

func (r *registroDeGravacao) Salvar(
	_ context.Context, _ int64, chave domain.ChavePesquisa, recortes []domain.Recorte,
) (int32, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, recorte := range recortes {
		nrPagina, err := domain.ParaInt64(recorte.Pagina)
		if err != nil {
			return 0, fmt.Errorf("página %d: %w", recorte.Pagina, err)
		}
		soma := sha256.Sum256([]byte(recorte.Destaque))
		r.recortes = append(r.recortes, RecorteGravado{
			Ordem:          len(r.recortes),
			IDPerfil:       chave.IDPerfil,
			ExpressaoBusca: chave.Expressao,
			NrPagina:       nrPagina,
			TextoSHA256:    hex.EncodeToString(soma[:]),
			Texto:          recorte.Destaque,
		})
	}
	return int32(len(recortes)), nil //nolint:gosec // limitado pelo número de páginas
}

type chavesFixas struct{ chaves []domain.ChavePesquisa }

func (c chavesFixas) ChavesPesquisa(context.Context, int64) ([]domain.ChavePesquisa, error) {
	return c.chaves, nil
}

type relogioParado struct{}

func (relogioParado) Agora() time.Time { return time.Unix(0, 0) }

// -------------------------------------------------------------------------
// Redução automática — o menor caso reprodutor
// -------------------------------------------------------------------------

// reduzir encontra o menor trecho que ainda distingue as duas cadeias.
//
// Bissecção por LINHA primeiro — que é a unidade em que o texto é produzido —,
// depois por RUNA dentro da linha. Sem isso, uma divergência imprime duas
// páginas inteiras e quem lê procura a diferença no olho.
func reduzir(esperado, obtido string) *MenorCaso {
	if esperado == obtido {
		return nil
	}

	linhasE := strings.Split(esperado, "\n")
	linhasO := strings.Split(obtido, "\n")

	for i := range max(len(linhasE), len(linhasO)) {
		e, o := linhaOuVazio(linhasE, i), linhaOuVazio(linhasO, i)
		if e == o {
			continue
		}
		caso := &MenorCaso{Linha: i + 1, Esperado: e, Obtido: o, Runa: -1}
		preencherRuna(caso, e, o)
		return caso
	}
	return &MenorCaso{Linha: 0, Esperado: esperado, Obtido: obtido, Runa: -1}
}

func preencherRuna(caso *MenorCaso, esperado, obtido string) {
	e, o := []rune(esperado), []rune(obtido)
	for i := range min(len(e), len(o)) {
		if e[i] != o[i] {
			caso.Runa = i
			caso.PontoEsperado = nomearRuna(e[i])
			caso.PontoObtido = nomearRuna(o[i])
			return
		}
	}
	// Um é prefixo do outro: a diferença é o excedente.
	caso.Runa = min(len(e), len(o))
	if len(e) > len(o) {
		caso.PontoEsperado = nomearRuna(e[len(o)])
		caso.PontoObtido = "(fim)"
		return
	}
	caso.PontoEsperado = "(fim)"
	caso.PontoObtido = nomearRuna(o[len(e)])
}

func nomearRuna(r rune) string {
	return fmt.Sprintf("U+%04X %q", r, string(r))
}

func linhaOuVazio(linhas []string, i int) string {
	if i < len(linhas) {
		return linhas[i]
	}
	return ""
}

// classeDeTexto infere a causa provável a partir do menor caso.
//
// A classificação é uma HEURÍSTICA de triagem, não um diagnóstico: ela agrupa o
// relatório para que classes distintas não se escondam umas atrás das outras.
// Quem decide a causa é quem lê o caso reprodutor.
func classeDeTexto(menor *MenorCaso) Classe {
	if menor == nil || menor.Runa < 0 {
		return ClasseDesconhecida
	}
	e := []rune(menor.Esperado)
	o := []rune(menor.Obtido)

	if menor.Runa < len(o) && o[menor.Runa] == '-' {
		return ClasseHifen
	}
	if menor.Runa < len(e) && menor.Runa < len(o) {
		if temDiacritico(o[menor.Runa]) != temDiacritico(e[menor.Runa]) {
			return ClasseDiacritico
		}
		if ehEspaco(e[menor.Runa]) || ehEspaco(o[menor.Runa]) {
			return ClasseEspaco
		}
	}
	return ClasseTexto
}

// temDiacritico é aproximado de propósito: cobre o Latim-1 e o Latim Estendido
// A, que é onde estão os acentos do português.
func temDiacritico(r rune) bool {
	return (r >= 0x00C0 && r <= 0x024F) || (r >= 0x0300 && r <= 0x036F)
}

func ehEspaco(r rune) bool {
	switch r {
	case ' ', '\t', '\n', '\r', ' ', ' ', ' ', ' ', '　':
		return true
	default:
		return r >= ' ' && r <= ' '
	}
}

func classeDeTermos(esperados, obtidos []string) Classe {
	if len(esperados) != len(obtidos) {
		return ClasseQuantidade
	}
	return ClasseTexto
}

func primeiroTermoDivergente(esperados, obtidos []string) string {
	for i := range min(len(esperados), len(obtidos)) {
		if esperados[i] != obtidos[i] {
			return fmt.Sprintf("%d: esperado %q, obtido %q", i, esperados[i], obtidos[i])
		}
	}
	if len(esperados) > len(obtidos) {
		return fmt.Sprintf("%d: faltou %q", len(obtidos), esperados[len(obtidos)])
	}
	if len(obtidos) > len(esperados) {
		return fmt.Sprintf("%d: sobrou %q", len(esperados), obtidos[len(esperados)])
	}
	return "(nenhuma)"
}

func igualSequencia(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func igualStatus(a, b []domain.StatusImportacao) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// -------------------------------------------------------------------------
// Leitura dos oráculos
// -------------------------------------------------------------------------

type paginasOraculo struct {
	Documento string   `json:"documento"`
	Paginas   []string `json:"paginas"`
}

type termosOraculo struct {
	Documento       string     `json:"documento"`
	TermosPorPagina [][]string `json:"termos_por_pagina"`
}

type recortesOraculo struct {
	Documento     string           `json:"documento"`
	Desfecho      string           `json:"desfecho"`
	TotalRecortes int              `json:"total_recortes"`
	Recortes      []RecorteGravado `json:"recortes"`
}

type chaveOraculo struct {
	IDPerfil    int64  `json:"id_perfil"`
	ExpressaoNM string `json:"expressao_nm"`
}

func carregarPaginas(dir, sufixo string) ([]paginasOraculo, error) {
	var saida []paginasOraculo
	if err := lerGlob(dir, "*"+sufixo, &saida); err != nil {
		return nil, err
	}
	return saida, nil
}

func carregarTermos(dir string) ([]termosOraculo, error) {
	var saida []termosOraculo
	if err := lerGlob(dir, "*.tokens.json", &saida); err != nil {
		return nil, err
	}
	return saida, nil
}

func carregarRecortes(dir string) ([]recortesOraculo, error) {
	var saida []recortesOraculo
	if err := lerGlob(dir, "*.recortes.json", &saida); err != nil {
		return nil, err
	}
	return saida, nil
}

// carregarChaves lê o dump de perfis NA ORDEM DO ARQUIVO.
//
// A ordem é normativa: vem do `ORDER BY tp.id_perfil, tpv.expressao_nm` do
// legado e governa a deduplicação por perfil (INV-P12).
func carregarChaves(dirCorpus string) ([]domain.ChavePesquisa, error) {
	bruto, err := os.ReadFile(filepath.Join(dirCorpus, "chaves.json")) //nolint:gosec // caminho do corpus
	if err != nil {
		return nil, fmt.Errorf("lendo chaves.json: %w", err)
	}
	var linhas []chaveOraculo
	if err := json.Unmarshal(bruto, &linhas); err != nil {
		return nil, fmt.Errorf("analisando chaves.json: %w", err)
	}

	chaves := make([]domain.ChavePesquisa, 0, len(linhas))
	for _, l := range linhas {
		chaves = append(chaves, domain.ChavePesquisa{IDPerfil: l.IDPerfil, Expressao: l.ExpressaoNM})
	}
	return chaves, nil
}

// lerGlob decodifica todos os arquivos que casam o padrão, em ordem estável.
func lerGlob[T any](dir, padrao string, alvo *[]T) error {
	arquivos, err := filepath.Glob(filepath.Join(dir, padrao))
	if err != nil {
		return fmt.Errorf("procurando %s: %w", padrao, err)
	}
	sort.Strings(arquivos)

	for _, a := range arquivos {
		bruto, err := os.ReadFile(a) //nolint:gosec // caminho vem do glob sobre o diretório de oráculos
		if err != nil {
			return fmt.Errorf("lendo %s: %w", a, err)
		}
		var v T
		if err := json.Unmarshal(bruto, &v); err != nil {
			return fmt.Errorf("analisando %s: %w", a, err)
		}
		*alvo = append(*alvo, v)
	}
	return nil
}
