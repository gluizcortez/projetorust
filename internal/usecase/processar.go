package usecase

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"runtime/debug"
	"sort"
	"strings"

	"github.com/gluizcortez/projetorust/internal/domain"
)

// Pipeline processa uma importação em segundo plano: extrai, indexa, busca cada
// expressão de perfil e grava os recortes.
//
// Porte de reference/main.rs:256-338, a closure disparada com
// `tokio::task::spawn` dentro do manipulador HTTP. A ORDEM das operações é
// contrato observável e não pode ser reagrupada, otimizada nem paralelizada:
// processar chaves em paralelo QUEBRA a deduplicação por perfil (INV-P12).
type Pipeline struct {
	importacoes domain.RepositorioImportacao
	perfis      domain.RepositorioPerfil
	recortes    domain.RepositorioRecorte
	extrator    domain.ExtratorTexto
	indexador   domain.Indexador
	relogio     domain.Relogio
	logger      *slog.Logger
	metricas    Metricas
}

// DependenciasDoPipeline reúne o que o pipeline precisa.
//
// É uma estrutura, e não sete parâmetros posicionais, porque na raiz de
// composição a lista nomeada é conferível de relance e imune a troca de ordem
// entre dois campos do mesmo tipo.
type DependenciasDoPipeline struct {
	Importacoes domain.RepositorioImportacao
	Perfis      domain.RepositorioPerfil
	Recortes    domain.RepositorioRecorte
	Extrator    domain.ExtratorTexto
	Indexador   domain.Indexador
	Relogio     domain.Relogio
	Logger      *slog.Logger

	// Metricas é opcional; nula vira MetricasNulas.
	Metricas Metricas
}

// NovoPipeline valida as dependências e monta o caso de uso.
//
// Falha em vez de aceitar porta nula: uma dependência esquecida na raiz de
// composição deve derrubar o arranque, não produzir pânico na primeira
// importação.
func NovoPipeline(d DependenciasDoPipeline) (*Pipeline, error) {
	faltando := []string{}
	if d.Importacoes == nil {
		faltando = append(faltando, "Importacoes")
	}
	if d.Perfis == nil {
		faltando = append(faltando, "Perfis")
	}
	if d.Recortes == nil {
		faltando = append(faltando, "Recortes")
	}
	if d.Extrator == nil {
		faltando = append(faltando, "Extrator")
	}
	if d.Indexador == nil {
		faltando = append(faltando, "Indexador")
	}
	if d.Relogio == nil {
		faltando = append(faltando, "Relogio")
	}
	if d.Logger == nil {
		faltando = append(faltando, "Logger")
	}
	if len(faltando) > 0 {
		return nil, fmt.Errorf("pipeline: dependências ausentes: %s", strings.Join(faltando, ", "))
	}

	metricas := d.Metricas
	if metricas == nil {
		metricas = MetricasNulas{}
	}

	return &Pipeline{
		importacoes: d.Importacoes,
		perfis:      d.Perfis,
		recortes:    d.Recortes,
		extrator:    d.Extrator,
		indexador:   d.Indexador,
		relogio:     d.Relogio,
		logger:      d.Logger,
		metricas:    metricas,
	}, nil
}

// Processar executa o pipeline inteiro de uma importação.
//
// NÃO devolve erro: no legado a tarefa de fundo não tem para quem reportar
// (reference/main.rs:256). Todo desfecho é observável pelo status gravado, pelo
// registro estruturado e pelas métricas — que é exatamente o que um chamador
// teria a fazer com um erro devolvido.
func (p *Pipeline) Processar(ctx context.Context, id int64, conteudo []byte) {
	inicio := p.relogio.Agora()
	log := p.logger.With(slog.Int64("id_importacao", id))

	// Reproduz o `scopeguard::defer` de main.rs:263-266: o registro de término
	// sai mesmo em caso de pânico. A liberação do contador equivalente é do
	// executor, que a registra antes desta.
	defer func() {
		log.InfoContext(ctx, "processo de recorte finalizado",
			slog.Duration("duracao", p.relogio.Agora().Sub(inicio)))
		p.metricas.ObservarEstagio(EstagioImportacao, p.relogio.Agora().Sub(inicio))
	}()
	defer p.recuperarDePanico(ctx, log, id)

	log.InfoContext(ctx, "processo de recorte iniciado")

	// main.rs:260-261 — data_inicio ANTES do status 1. A ordem importa para
	// quem observa as duas colunas (docs/ESPECIFICACAO.md §3.4).
	p.tentarMarcarInicio(ctx, log, id)
	p.tentarGravarStatus(ctx, log, id, domain.StatusSelecionado)

	// main.rs:268 — status 2 ANTES de criar o índice.
	p.tentarGravarStatus(ctx, log, id, domain.StatusIndexando)

	indice, ok := p.indexar(ctx, log, id, conteudo)
	if !ok {
		return
	}
	defer p.fecharIndice(ctx, log, indice)

	// main.rs:272 — status 3 assim que o índice está pronto.
	p.tentarGravarStatus(ctx, log, id, domain.StatusRecortando)

	chaves, ok := p.carregarChaves(ctx, log, id)
	if !ok {
		return
	}

	total, ok := p.recortar(ctx, log, id, indice, chaves)
	if !ok {
		return
	}

	// main.rs:325-326 — status 5 e depois data_fim com o total.
	p.tentarGravarStatus(ctx, log, id, domain.StatusFinalizado)
	p.tentarMarcarTermino(ctx, log, id, total)

	p.metricas.ObservarRecortes(total)
	p.metricas.ContarImportacao(DesfechoFinalizado)
}

// indexar extrai o texto e constrói o índice.
//
// No legado as duas coisas são uma função só, `criar_indice`
// (main.rs:479-535), e QUALQUER falha dela leva ao mesmo desfecho: status -1 e
// fim da importação (main.rs:332-335). A separação em duas portas é de
// arquitetura; o comportamento observável é o mesmo.
func (p *Pipeline) indexar(
	ctx context.Context, log *slog.Logger, id int64, conteudo []byte,
) (domain.Indice, bool) {
	inicio := p.relogio.Agora()
	paginas, err := p.extrator.ExtrairPaginas(ctx, conteudo)
	p.metricas.ObservarEstagio(EstagioExtracao, p.relogio.Agora().Sub(inicio))
	if err != nil {
		log.ErrorContext(ctx, "falha ao indexar documento",
			slog.String("etapa", "extracao"), slog.Any("erro", err))
		p.encerrarComErro(ctx, log, id)
		return nil, false
	}

	// main.rs:508 — o legado registra a contagem entre a extração e a
	// indexação.
	log.InfoContext(ctx, "documento paginado", slog.Int("paginas", len(paginas)))
	p.metricas.ObservarPaginas(len(paginas))

	inicio = p.relogio.Agora()
	indice, err := p.indexador.Construir(ctx, paginas)
	p.metricas.ObservarEstagio(EstagioIndexacao, p.relogio.Agora().Sub(inicio))
	if err != nil {
		log.ErrorContext(ctx, "falha ao indexar documento",
			slog.String("etapa", "indexacao"), slog.Any("erro", err))
		p.encerrarComErro(ctx, log, id)
		return nil, false
	}

	log.InfoContext(ctx, "documento indexado", slog.Int("paginas", len(paginas)))
	return indice, true
}

// carregarChaves obtém os pares perfil/expressão.
//
// main.rs:327-331 — a falha grava -1 e encerra sem gravar recorte algum.
func (p *Pipeline) carregarChaves(
	ctx context.Context, log *slog.Logger, id int64,
) ([]domain.ChavePesquisa, bool) {
	inicio := p.relogio.Agora()
	chaves, err := p.perfis.ChavesPesquisa(ctx, id)
	p.metricas.ObservarEstagio(EstagioChaves, p.relogio.Agora().Sub(inicio))
	if err != nil {
		log.ErrorContext(ctx, "falha ao carregar chaves de pesquisa", slog.Any("erro", err))
		p.encerrarComErro(ctx, log, id)
		return nil, false
	}

	log.InfoContext(ctx, "chaves de pesquisa carregadas", slog.Int("chaves", len(chaves)))
	return chaves, true
}

// recortar é o laço de main.rs:282-323, com a deduplicação por perfil.
//
// Devolve o total acumulado e se a importação deve continuar.
//
// PROIBIDO PARALELIZAR. O conjunto de páginas atravessa iterações e só é
// reiniciado quando o perfil muda (INV-P12): a ordem sequencial das chaves é
// que determina qual expressão fica registrada numa página disputada.
func (p *Pipeline) recortar(
	ctx context.Context, log *slog.Logger, id int64,
	indice domain.Indice, chaves []domain.ChavePesquisa,
) (int, bool) {
	inicio := p.relogio.Agora()
	defer func() { p.metricas.ObservarEstagio(EstagioRecorte, p.relogio.Agora().Sub(inicio)) }()

	var total int
	deduplicador := novoDeduplicadorPorPerfil()

	for _, chave := range chaves {
		logDaChave := log.With(
			slog.Int64("id_perfil", chave.IDPerfil),
			slog.String("expressao", chave.Expressao),
		)

		recortes, ok := p.buscar(ctx, logDaChave, id, indice, chave)
		if !ok {
			return total, false
		}

		// main.rs:285 — ordenação ESTÁVEL por página. Não é observável hoje,
		// porque há no máximo um recorte por página; usar a estável mesmo assim
		// evita depender dessa coincidência (ESPECIFICACAO §5.2).
		sort.SliceStable(recortes, func(i, j int) bool {
			return recortes[i].Pagina < recortes[j].Pagina
		})

		// main.rs:287-290 — o conjunto só reinicia quando o PERFIL muda.
		deduplicador.aoEntrarNoPerfil(chave.IDPerfil)

		// main.rs:291-293 — o legado registra a contagem BRUTA, antes de
		// filtrar, e só quando há o que filtrar.
		if len(recortes) > 0 {
			logDaChave.InfoContext(ctx, "recortes encontrados", slog.Int("total", len(recortes)))
		}

		filtrados := deduplicador.filtrar(recortes)
		total += len(filtrados)

		// main.rs:308 — a gravação só ocorre se sobrou algo.
		if len(filtrados) == 0 {
			continue
		}
		if !p.gravar(ctx, logDaChave, id, chave, filtrados) {
			return total, false
		}
	}

	return total, true
}

// buscar executa a consulta de uma expressão.
//
// Traduz para o desfecho do legado os DOIS caminhos de falha, que são
// diferentes e não podem ser confundidos:
//
//   - INV-P17: expressão com aspas duplas. No legado o QueryParser recebe
//     `format!(r#""{key}""#)`, a consulta fica desbalanceada e `recortar`
//     devolve Err — status -1 e fim (main.rs:318-322). O índice em Go não tem
//     analisador de consulta e não falharia, então a condição é verificada
//     AQUI, explicitamente.
//   - D-06: expressão com `&` que não compila como expressão regular. No legado
//     é `.unwrap()` e portanto PÂNICO: a tarefa morre sem gravar status e a
//     importação fica presa em 3. NÃO é o mesmo que -1.
func (p *Pipeline) buscar(
	ctx context.Context, log *slog.Logger, id int64,
	indice domain.Indice, chave domain.ChavePesquisa,
) ([]domain.Recorte, bool) {
	if strings.ContainsRune(chave.Expressao, '"') {
		log.ErrorContext(ctx, "falha ao recortar",
			slog.Any("erro", errConsultaDesbalanceada))
		p.encerrarComErro(ctx, log, id)
		return nil, false
	}

	recortes, err := indice.Frase(ctx, chave.Expressao)
	if err == nil {
		return recortes, true
	}

	if errors.Is(err, domain.ErrExpressaoInvalida) {
		p.encerrarPreso(ctx, log, err)
		return nil, false
	}

	log.ErrorContext(ctx, "falha ao recortar", slog.Any("erro", err))
	p.encerrarComErro(ctx, log, id)
	return nil, false
}

// errConsultaDesbalanceada descreve a condição de INV-P17.
//
// A mensagem existe para o registro; a identidade do erro não é observada por
// ninguém, porque no legado a falha equivalente vem de dentro do Tantivy.
var errConsultaDesbalanceada = errors.New(
	`expressão contém aspas duplas: a consulta de frase do legado fica ` +
		`desbalanceada e o analisador a rejeita (INV-P17)`)

// gravar persiste os recortes de uma chave.
//
// main.rs:309-316 — falha grava -1 e ENCERRA a importação; o que já foi gravado
// PERMANECE, sem compensação (INV-P14).
func (p *Pipeline) gravar(
	ctx context.Context, log *slog.Logger, id int64,
	chave domain.ChavePesquisa, filtrados []domain.Recorte,
) bool {
	log.InfoContext(ctx, "recortes a gravar", slog.Int("total", len(filtrados)))

	gravados, err := p.recortes.Salvar(ctx, id, chave, filtrados)
	if err != nil {
		log.ErrorContext(ctx, "falha ao gravar recortes", slog.Any("erro", err))
		p.encerrarComErro(ctx, log, id)
		return false
	}

	log.InfoContext(ctx, "recortes gravados", slog.Int("total", int(gravados)))
	return true
}

// fecharIndice libera o índice. O legado não tem equivalente: o `Index` do
// Tantivy é derrubado junto com a tarefa.
func (p *Pipeline) fecharIndice(ctx context.Context, log *slog.Logger, indice domain.Indice) {
	if err := indice.Fechar(); err != nil {
		log.WarnContext(ctx, "falha ao liberar o índice", slog.Any("erro", err))
	}
}

// encerrarComErro grava o status -1 e contabiliza o desfecho.
func (p *Pipeline) encerrarComErro(ctx context.Context, log *slog.Logger, id int64) {
	p.tentarGravarStatus(ctx, log, id, domain.StatusErro)
	p.metricas.ContarImportacao(DesfechoErro)
}

// encerrarPreso reproduz o efeito observável do pânico do legado: a importação
// fica no ÚLTIMO status gravado, sem ir a -1 nem a 5.
//
// É o padrão provisório de docs/DECISOES-ABERTAS.md, D-06. A normalização para
// -1 é evolução da fase F11, atrás de chave — não pode entrar aqui, porque
// mudaria o estado final de uma importação que hoje trava.
//
// A métrica existe justamente para tornar visível o que no legado é invisível:
// sem ela, a única evidência de uma importação presa é uma linha que nunca sai
// do status 3.
func (p *Pipeline) encerrarPreso(ctx context.Context, log *slog.Logger, err error) {
	log.ErrorContext(ctx, "importação presa: expressão inválida derrubaria a tarefa no legado",
		slog.Any("erro", err),
		slog.String("decisao", "D-06"),
	)
	p.metricas.ContarImportacao(DesfechoPreso)
}

// recuperarDePanico impede que um defeito NOSSO derrube o processo.
//
// Não tem contrapartida no legado, e é por isso que grava -1: o único pânico
// alcançável em Rust é o `.unwrap()` da expressão regular, que aqui já virou
// erro tipado e é tratado em encerrarPreso. Um pânico em Go significa defeito
// de programação, sem comportamento legado a preservar — e deixar a linha
// travada em silêncio seria a pior das opções.
func (p *Pipeline) recuperarDePanico(ctx context.Context, log *slog.Logger, id int64) {
	r := recover()
	if r == nil {
		return
	}
	log.ErrorContext(ctx, "pipeline entrou em pânico",
		slog.Any("panico", r),
		slog.String("pilha", string(debug.Stack())),
	)
	p.encerrarComErro(ctx, log, id)
}

// -------------------------------------------------------------------------
// Gravações cujo resultado o legado descarta
// -------------------------------------------------------------------------
//
// Todas as chamadas de status do legado usam `let _ = ...`
// (docs/ESPECIFICACAO.md §3.6): a falha ao gravar o status é ignorada e o fluxo
// continua. Reproduzimos a decisão de FLUXO — continuar — mas não o silêncio:
// o erro vai para o registro, o que é mudança de observabilidade, não de
// comportamento.

func (p *Pipeline) tentarGravarStatus(
	ctx context.Context, log *slog.Logger, id int64, status domain.StatusImportacao,
) {
	if err := p.importacoes.AtualizarStatus(ctx, id, status); err != nil {
		log.ErrorContext(ctx, "falha ao gravar status; o legado ignoraria em silêncio",
			slog.String("status", status.String()), slog.Any("erro", err))
	}
}

func (p *Pipeline) tentarMarcarInicio(ctx context.Context, log *slog.Logger, id int64) {
	if err := p.importacoes.MarcarInicio(ctx, id); err != nil {
		log.ErrorContext(ctx, "falha ao gravar data_inicio; o legado ignoraria em silêncio",
			slog.Any("erro", err))
	}
}

// tentarMarcarTermino grava data_fim e total_recortes.
//
// INV-P18: total acima de 2.147.483.647 faz a conversão falhar e NENHUMA das
// duas colunas é gravada — o status permanece 5. O legado descarta o erro com
// `let _ =` (main.rs:326).
func (p *Pipeline) tentarMarcarTermino(ctx context.Context, log *slog.Logger, id int64, total int) {
	convertido, err := domain.TamanhoParaInt32(total)
	if err != nil {
		log.ErrorContext(ctx, "total de recortes não cabe em int32; data_fim não será gravada",
			slog.Int("total", total), slog.Any("erro", err))
		return
	}
	if err := p.importacoes.MarcarTermino(ctx, id, convertido); err != nil {
		log.ErrorContext(ctx, "falha ao gravar data_fim; o legado ignoraria em silêncio",
			slog.Any("erro", err))
	}
}

// -------------------------------------------------------------------------
// Deduplicação por perfil — INV-P12
// -------------------------------------------------------------------------

// deduplicadorPorPerfil guarda as páginas já consumidas pelo perfil corrente.
//
// Existe como tipo próprio para que a regra mais sutil da migração fique em um
// lugar só, com nome, e possa ser testada isolada do resto do pipeline.
type deduplicadorPorPerfil struct {
	paginas map[uint64]struct{}

	// perfilCorrente e primeiraChave juntos são o sentinela explícito que
	// substitui o `let mut id_perfil: i64 = 0` de main.rs:281.
	//
	// INV-P13: o legado inicia o contador em ZERO, o que significa que um
	// perfil de id 0 não dispararia o reinício na primeira chave. O efeito
	// prático é nulo — o conjunto já nasce vazio —, mas depender disso seria
	// depender de coincidência. O booleano torna a primeira iteração explícita
	// e o comportamento fica idêntico com ou sem perfil zero.
	// Ver docs/DECISOES-ABERTAS.md, D-01.
	perfilCorrente int64
	primeiraChave  bool
}

func novoDeduplicadorPorPerfil() *deduplicadorPorPerfil {
	return &deduplicadorPorPerfil{
		paginas:       map[uint64]struct{}{},
		primeiraChave: true,
	}
}

// aoEntrarNoPerfil reinicia o conjunto quando o perfil muda. main.rs:287-290.
func (d *deduplicadorPorPerfil) aoEntrarNoPerfil(idPerfil int64) {
	if d.primeiraChave || d.perfilCorrente != idPerfil {
		d.paginas = map[uint64]struct{}{}
		d.perfilCorrente = idPerfil
		d.primeiraChave = false
	}
}

// filtrar descarta os recortes cuja página já foi consumida pelo perfil.
//
// main.rs:296-304. A inserção no conjunto é INCONDICIONAL, fora do `if`: roda
// para todo recorte, inclusive os descartados. Como o descarte só acontece
// quando a página já está lá, a reinserção é idempotente e não muda o
// resultado — está aqui por fidelidade ao original.
func (d *deduplicadorPorPerfil) filtrar(recortes []domain.Recorte) []domain.Recorte {
	filtrados := make([]domain.Recorte, 0, len(recortes))
	for _, recorte := range recortes {
		if _, visto := d.paginas[recorte.Pagina]; !visto {
			filtrados = append(filtrados, recorte)
		}
		d.paginas[recorte.Pagina] = struct{}{}
	}
	return filtrados
}
