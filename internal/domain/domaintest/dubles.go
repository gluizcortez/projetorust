// Package domaintest fornece dublês das portas do domínio.
//
// Todos registram as chamadas em um Diario compartilhado, na ordem em que
// ocorrem. Isso é o que permite à fase F8 asserir a SEQUÊNCIA de operações do
// pipeline — que é contrato — e não apenas o estado final.
//
// Este pacote não é usado em produção.
package domaintest

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/gluizcortez/projetorust/internal/domain"
)

// Diario registra as chamadas observadas, em ordem.
type Diario struct {
	mu       sync.Mutex
	entradas []string
}

// Anotar acrescenta uma chamada ao diário.
func (d *Diario) Anotar(formato string, args ...any) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.entradas = append(d.entradas, fmt.Sprintf(formato, args...))
}

// Entradas devolve uma cópia das chamadas registradas, em ordem.
func (d *Diario) Entradas() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	saida := make([]string, len(d.entradas))
	copy(saida, d.entradas)
	return saida
}

// Limpar descarta o histórico.
func (d *Diario) Limpar() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.entradas = nil
}

// -------------------------------------------------------------------------

// RepositorioImportacaoFalso implementa domain.RepositorioImportacao.
//
// Seguro para uso concorrente: a fase F8 exercita o executor de tarefas de
// fundo com várias importações simultâneas.
type RepositorioImportacaoFalso struct {
	mu     sync.Mutex
	Diario *Diario

	// IDGerado é devolvido por Registrar.
	IDGerado int64

	// Erros injetáveis, por método.
	ErroRegistrar       error
	ErroAtualizarStatus error
	ErroMarcarInicio    error
	ErroMarcarTermino   error

	// Estado observável.
	Registradas []domain.Importacao
	Status      []domain.StatusImportacao
	TotalFinal  int32
}

var _ domain.RepositorioImportacao = (*RepositorioImportacaoFalso)(nil)

// Registrar simula a inserção da importação.
func (r *RepositorioImportacaoFalso) Registrar(_ context.Context, imp domain.Importacao) (int64, error) {
	r.Diario.Anotar("Importacao.Registrar(caderno=%d, hash=%s)", imp.IDCaderno, imp.HashSHA256)
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.ErroRegistrar != nil {
		return 0, r.ErroRegistrar
	}
	r.Registradas = append(r.Registradas, imp)
	return r.IDGerado, nil
}

// AtualizarStatus simula a gravação do estado.
func (r *RepositorioImportacaoFalso) AtualizarStatus(_ context.Context, id int64, s domain.StatusImportacao) error {
	r.Diario.Anotar("Importacao.AtualizarStatus(%d, %s)", id, s)
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.ErroAtualizarStatus != nil {
		return r.ErroAtualizarStatus
	}
	r.Status = append(r.Status, s)
	return nil
}

// MarcarInicio simula a gravação de data_inicio.
func (r *RepositorioImportacaoFalso) MarcarInicio(_ context.Context, id int64) error {
	r.Diario.Anotar("Importacao.MarcarInicio(%d)", id)
	return r.ErroMarcarInicio
}

// MarcarTermino simula a gravação de data_fim e total_recortes.
func (r *RepositorioImportacaoFalso) MarcarTermino(_ context.Context, id int64, total int32) error {
	r.Diario.Anotar("Importacao.MarcarTermino(%d, %d)", id, total)
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.ErroMarcarTermino != nil {
		return r.ErroMarcarTermino
	}
	r.TotalFinal = total
	return nil
}

// StatusGravados devolve os estados observados, em ordem, de forma segura.
func (r *RepositorioImportacaoFalso) StatusGravados() []domain.StatusImportacao {
	r.mu.Lock()
	defer r.mu.Unlock()
	saida := make([]domain.StatusImportacao, len(r.Status))
	copy(saida, r.Status)
	return saida
}

// -------------------------------------------------------------------------

// RepositorioPerfilFalso implementa domain.RepositorioPerfil.
type RepositorioPerfilFalso struct {
	Diario *Diario

	// Chaves é devolvido por ChavesPesquisa, NA ORDEM em que for declarado —
	// que precisa ser a de (id_perfil, expressao_nm).
	Chaves []domain.ChavePesquisa
	Erro   error
}

var _ domain.RepositorioPerfil = (*RepositorioPerfilFalso)(nil)

// ChavesPesquisa devolve as chaves configuradas.
func (r *RepositorioPerfilFalso) ChavesPesquisa(_ context.Context, id int64) ([]domain.ChavePesquisa, error) {
	r.Diario.Anotar("Perfil.ChavesPesquisa(%d)", id)
	if r.Erro != nil {
		return nil, r.Erro
	}
	return r.Chaves, nil
}

// -------------------------------------------------------------------------

// GravacaoObservada é um lote de recortes entregue ao repositório.
type GravacaoObservada struct {
	IDImportacao int64
	Chave        domain.ChavePesquisa
	Recortes     []domain.Recorte
}

// RepositorioRecorteFalso implementa domain.RepositorioRecorte.
type RepositorioRecorteFalso struct {
	mu     sync.Mutex
	Diario *Diario

	// ErroNaChamada faz a n-ésima chamada (base 1) falhar. Zero desliga.
	ErroNaChamada int
	Erro          error

	// EntrarEmPanico faz Salvar entrar em pânico. Existe para exercitar a
	// recuperação do pipeline e do executor: um defeito de programação não
	// pode derrubar o processo.
	EntrarEmPanico bool

	Gravacoes []GravacaoObservada
	chamadas  int
}

var _ domain.RepositorioRecorte = (*RepositorioRecorteFalso)(nil)

// Salvar simula a gravação de um lote de recortes.
func (r *RepositorioRecorteFalso) Salvar(
	_ context.Context, idImportacao int64, chave domain.ChavePesquisa, recortes []domain.Recorte,
) (int32, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.chamadas++
	r.Diario.Anotar("Recorte.Salvar(imp=%d, perfil=%d, expressao=%q, n=%d)",
		idImportacao, chave.IDPerfil, chave.Expressao, len(recortes))

	if r.EntrarEmPanico {
		panic("dublê de RepositorioRecorte: pânico solicitado pelo teste")
	}

	if r.ErroNaChamada != 0 && r.chamadas == r.ErroNaChamada {
		return 0, r.Erro
	}
	r.Gravacoes = append(r.Gravacoes, GravacaoObservada{
		IDImportacao: idImportacao, Chave: chave, Recortes: recortes,
	})
	total, err := domain.TamanhoParaInt32(len(recortes))
	if err != nil {
		return 0, fmt.Errorf("dublê de RepositorioRecorte: %w", err)
	}
	return total, nil
}

// GravacoesObservadas devolve os lotes gravados, em ordem, de forma segura.
func (r *RepositorioRecorteFalso) GravacoesObservadas() []GravacaoObservada {
	r.mu.Lock()
	defer r.mu.Unlock()
	saida := make([]GravacaoObservada, len(r.Gravacoes))
	copy(saida, r.Gravacoes)
	return saida
}

// -------------------------------------------------------------------------

// ExtratorTextoFalso implementa domain.ExtratorTexto.
type ExtratorTextoFalso struct {
	Diario  *Diario
	Paginas []string
	Erro    error
}

var _ domain.ExtratorTexto = (*ExtratorTextoFalso)(nil)

// ExtrairPaginas devolve as páginas configuradas.
func (e *ExtratorTextoFalso) ExtrairPaginas(_ context.Context, conteudo []byte) ([]string, error) {
	e.Diario.Anotar("Extrator.ExtrairPaginas(bytes=%d)", len(conteudo))
	if e.Erro != nil {
		return nil, e.Erro
	}
	return e.Paginas, nil
}

// -------------------------------------------------------------------------

// IndiceFalso implementa domain.Indice.
//
// Acertos mapeia expressão para as páginas em que ela é encontrada. Isso
// permite montar cenários de deduplicação (INV-P12) sem índice real.
type IndiceFalso struct {
	mu      sync.Mutex
	Diario  *Diario
	Acertos map[string][]uint64
	Textos  map[uint64]string
	Erro    error
	Fechado bool

	// ErroFechar é devolvido por Fechar. O legado não tem equivalente — o
	// Index do Tantivy é derrubado com a tarefa —, então a falha aqui não pode
	// alterar o desfecho da importação.
	ErroFechar error
}

var _ domain.Indice = (*IndiceFalso)(nil)

// Frase devolve os recortes configurados para a expressão.
func (i *IndiceFalso) Frase(_ context.Context, expressao string) ([]domain.Recorte, error) {
	i.Diario.Anotar("Indice.Frase(%q)", expressao)
	if i.Erro != nil {
		return nil, i.Erro
	}
	var saida []domain.Recorte
	for _, pagina := range i.Acertos[expressao] {
		texto := i.Textos[pagina]
		if texto == "" {
			texto = fmt.Sprintf("texto sintético da página %d", pagina)
		}
		saida = append(saida, domain.NovoRecorte(pagina, texto))
	}
	return saida, nil
}

// Fechar marca o índice como liberado.
func (i *IndiceFalso) Fechar() error {
	i.Diario.Anotar("Indice.Fechar()")
	i.mu.Lock()
	defer i.mu.Unlock()
	i.Fechado = true
	return i.ErroFechar
}

// IndexadorFalso implementa domain.Indexador.
type IndexadorFalso struct {
	Diario *Diario
	Indice *IndiceFalso
	Erro   error
}

var _ domain.Indexador = (*IndexadorFalso)(nil)

// Construir devolve o índice configurado.
func (x *IndexadorFalso) Construir(_ context.Context, paginas []string) (domain.Indice, error) {
	x.Diario.Anotar("Indexador.Construir(paginas=%d)", len(paginas))
	if x.Erro != nil {
		return nil, x.Erro
	}
	return x.Indice, nil
}

// -------------------------------------------------------------------------

// UnidadeDeTrabalhoFalsa implementa domain.UnidadeDeTrabalho executando a
// função diretamente, sem transação.
type UnidadeDeTrabalhoFalsa struct {
	Diario *Diario
}

var _ domain.UnidadeDeTrabalho = (*UnidadeDeTrabalhoFalsa)(nil)

// EmTransacao executa a função recebida.
func (u *UnidadeDeTrabalhoFalsa) EmTransacao(ctx context.Context, fn func(context.Context) error) error {
	u.Diario.Anotar("UnidadeDeTrabalho.EmTransacao(início)")
	err := fn(ctx)
	u.Diario.Anotar("UnidadeDeTrabalho.EmTransacao(fim, erro=%v)", err)
	return err
}

// -------------------------------------------------------------------------

// RelogioFixo implementa domain.Relogio devolvendo sempre o mesmo instante.
type RelogioFixo struct {
	Instante time.Time
}

var _ domain.Relogio = (*RelogioFixo)(nil)

// Agora devolve o instante configurado.
func (r RelogioFixo) Agora() time.Time { return r.Instante }
