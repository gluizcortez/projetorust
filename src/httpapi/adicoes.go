package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/gluizcortez/projetorust/src/domain"
	"github.com/gluizcortez/projetorust/src/usecase"
)

// Este arquivo tem as rotas que NÃO existem no legado: o endpoint de status e as
// duas sondas de saúde. São ADIÇÃO PURA — só são registradas com a chave
// correspondente ligada, e nenhuma delas toca em `/ping` ou `/pdf`.
//
// Ficam separadas de router.go justamente para que a fronteira seja visível:
// tudo aqui é comportamento novo, e nada aqui pode ser invocado com as chaves no
// padrão.

// Textos das respostas de erro das adições.
//
// NÃO são literais do legado — nenhuma destas rotas existe lá. Seguem o estilo
// das mensagens existentes: frase curta, em português, sem ponto final.
const (
	// TextoImportacaoNaoEncontrada acompanha o 404 de GET /importacao/{id}.
	TextoImportacaoNaoEncontrada = "Importação não encontrada"
	// TextoIdentificadorInvalido acompanha o 400 de um id que não é inteiro.
	TextoIdentificadorInvalido = "Identificador inválido"
	// TextoFalhaAoConsultar acompanha o 500 de falha de banco na consulta.
	TextoFalhaAoConsultar = "Erro ao consultar a importação"
	// TextoVivo é o corpo de /health/live.
	TextoVivo = "vivo"
	// TextoPronto é o corpo de /health/ready quando tudo responde.
	TextoPronto = "pronto"
	// TextoIndisponivel é o corpo de /health/ready quando algo não responde.
	TextoIndisponivel = "indisponível"
)

// ConsultorDeImportacao é a porta de leitura de GET /importacao/{id}.
//
// Definida aqui, pelo consumidor, como todas as outras deste pacote.
type ConsultorDeImportacao interface {
	// Consultar devolve o resumo da importação.
	//
	// Identificador sem correspondência devolve domain.ErrImportacaoNaoEncontrada,
	// não um resumo zerado — a diferença entre "não existe" e "existe e está em
	// status 0" é exatamente o que o endpoint serve para dizer.
	Consultar(ctx context.Context, id int64) (domain.ResumoDaImportacao, error)
}

// -------------------------------------------------------------------------
// STATUS_ENDPOINT
// -------------------------------------------------------------------------

// respostaDeImportacao é o corpo JSON de GET /importacao/{id}.
//
// Os nomes dos campos usam sublinhado, como as colunas do banco e como as
// variáveis de ambiente — é a convenção já presente no projeto. As datas saem
// no formato AAAA-MM-DD do domínio; os carimbos, em RFC 3339.
//
// Campos nulos são OMITIDOS. Um cliente que veja `total_recortes` ausente sabe
// que a importação não terminou; um que veja `0` sabe que terminou sem
// ocorrências. Emitir `null` para os dois casos apagaria a distinção que INV-P20
// torna importante.
type respostaDeImportacao struct {
	ID     int64  `json:"id_importacao"`
	Status int32  `json:"status"`
	Rotulo string `json:"status_rotulo"`

	DataCaderno          string `json:"data_caderno,omitempty"`
	DataDisponibilizacao string `json:"data_disponibilizacao,omitempty"`

	DataInicio *string `json:"data_inicio,omitempty"`
	DataFim    *string `json:"data_fim,omitempty"`

	TotalRecortes *int32 `json:"total_recortes,omitempty"`

	Concluida bool `json:"concluida"`
}

// consultarImportacao atende GET /importacao/{id}.
//
// Passa pela autenticação, como /pdf: o estado de uma importação diz quando um
// diário foi submetido e quantas ocorrências ele gerou, o que é informação do
// cliente, não pública.
func (s *servico) consultarImportacao(w http.ResponseWriter, r *http.Request) {
	id, ok := identificadorDaRota(r.URL.EscapedPath())
	if !ok {
		s.escritor.Escrever(w, r, http.StatusBadRequest, TextoIdentificadorInvalido, nil)
		return
	}

	resumo, err := s.importacoes.Consultar(r.Context(), id)
	switch {
	case err == nil:
	case errors.Is(err, domain.ErrImportacaoNaoEncontrada):
		s.escritor.Escrever(w, r, http.StatusNotFound, TextoImportacaoNaoEncontrada, nil)
		return
	default:
		s.logger.ErrorContext(r.Context(), "falha ao consultar importação",
			slog.Int64("id_importacao", id), slog.Any("erro", err))
		s.escritor.Escrever(w, r, http.StatusInternalServerError, TextoFalhaAoConsultar, nil)
		return
	}

	corpo, err := json.Marshal(paraResposta(resumo))
	if err != nil {
		// Inalcançável: o documento só tem tipos primitivos.
		s.logger.ErrorContext(r.Context(), "falha ao serializar o resumo da importação",
			slog.Int64("id_importacao", id), slog.Any("erro", err))
		s.escritor.Escrever(w, r, http.StatusInternalServerError, TextoFalhaAoConsultar, nil)
		return
	}

	// O SUCESSO é sempre JSON, independentemente de RESPOSTA_PROBLEM_JSON: a
	// chave governa o formato dos ERROS, e um endpoint de dados que devolvesse
	// texto puro não teria como transmitir os campos. Os erros acima passam pelo
	// escritor e, portanto, respeitam a chave.
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(corpo)
}

// paraResposta converte o resumo do domínio no documento da resposta.
func paraResposta(resumo domain.ResumoDaImportacao) respostaDeImportacao {
	out := respostaDeImportacao{
		ID:                   resumo.ID,
		Status:               int32(resumo.Status),
		Rotulo:               resumo.Status.String(),
		DataCaderno:          resumo.DataCaderno.String(),
		DataDisponibilizacao: resumo.DataDisponibilizacao.String(),
		TotalRecortes:        resumo.TotalRecortes,
		Concluida:            resumo.Concluida(),
	}
	if resumo.DataInicio != nil {
		t := resumo.DataInicio.Format(formatoDeCarimbo)
		out.DataInicio = &t
	}
	if resumo.DataFim != nil {
		t := resumo.DataFim.Format(formatoDeCarimbo)
		out.DataFim = &t
	}
	return out
}

// formatoDeCarimbo é RFC 3339 com nanossegundos, o padrão de interoperação.
const formatoDeCarimbo = "2006-01-02T15:04:05.999999999Z07:00"

// identificadorDaRota extrai o `{id}` de `/importacao/{id}`.
//
// O caminho já chegou NORMALIZADO (ver normalizarCaminho), então a comparação é
// direta: exatamente dois segmentos, o segundo inteiro.
//
// Recusa negativo. Identificador de importação vem de uma sequência do banco e
// é sempre positivo; aceitar `-1` só levaria a um 404 mais caro.
func identificadorDaRota(caminho string) (int64, bool) {
	resto, achou := strings.CutPrefix(normalizarCaminho(caminho), RotaImportacao+"/")
	if !achou || resto == "" || strings.Contains(resto, "/") {
		return 0, false
	}
	id, err := strconv.ParseInt(resto, 10, 64)
	if err != nil || id <= 0 {
		return 0, false
	}
	return id, true
}

// -------------------------------------------------------------------------
// HEALTH_ENDPOINTS
// -------------------------------------------------------------------------

// healthLive responde enquanto o processo estiver de pé.
//
// NÃO consulta nada, de propósito: uma sonda de vivacidade que dependa do banco
// faz o orquestrador REINICIAR o serviço quando o banco cai — o que não conserta
// o banco e derruba as importações em andamento. Quem verifica dependência é
// /health/ready.
func (s *servico) healthLive(w http.ResponseWriter, r *http.Request) {
	s.escritor.Escrever(w, r, http.StatusOK, TextoVivo, nil)
}

// healthReady informa se o serviço pode receber tráfego.
//
// A verificação em si é injetada: este pacote não conhece o pool de conexões
// nem o executor. Ver app.Novo, que compõe "banco alcançável" com "fila abaixo
// do teto".
//
// Falha devolve 503 — o código que balanceadores e orquestradores entendem como
// "tire este pod da rotação, mas não o mate".
func (s *servico) healthReady(w http.ResponseWriter, r *http.Request) {
	if err := s.pronto(r.Context()); err != nil {
		s.logger.WarnContext(r.Context(), "sonda de prontidão reprovou", slog.Any("erro", err))
		// O MOTIVO não vai no corpo: ele descreve a topologia interna — qual
		// dependência caiu, qual fila encheu — e a sonda é alcançável por quem
		// não tem credencial. Quem precisa do motivo lê o registro.
		s.escritor.Escrever(w, r, http.StatusServiceUnavailable, TextoIndisponivel, nil)
		return
	}
	s.escritor.Escrever(w, r, http.StatusOK, TextoPronto, nil)
}

// -------------------------------------------------------------------------
// POST /pdf-verificacao — VERIFICACAO_ENDPOINT
// -------------------------------------------------------------------------
//
// Busca manual de uma expressão num PDF, para diagnóstico do time técnico.
//
// Recebe os MESMOS campos de `/pdf` mais `expressao`, roda o MESMO caminho de
// extração, normalização, indexação e busca, e devolve o que encontrou em JSON.
// Não registra importação, não grava recorte, não muda status — não tem
// repositório algum entre as suas dependências.
//
// # Por que exige a chave de API
//
// A resposta traz TRECHOS DO DOCUMENTO submetido. Ainda que o documento venha
// do próprio cliente, deixar a rota aberta faria dela um extrator de texto de
// PDF anônimo, hospedado por nós. Autenticada, como `/pdf`.

// Verificador é a porta do caso de uso de verificação manual.
//
// Declarada aqui, pelo consumidor, para que o pacote HTTP não dependa do tipo
// concreto de usecase.Verificacao.
type Verificador interface {
	Executar(ctx context.Context, cmd usecase.ComandoVerificar) (usecase.ResultadoVerificacao, error)
}

// Textos das respostas de erro da verificação.
const (
	// TextoExpressaoAusente acompanha o 400 de `expressao` vazia ou ausente.
	TextoExpressaoAusente = "Expressão de verificação não informada"
	// TextoFalhaAoVerificar acompanha o 422 de documento que não pôde ser lido.
	TextoFalhaAoVerificar = "Erro ao verificar o PDF"
)

// ocorrenciaJSON é uma página em que a expressão foi encontrada.
type ocorrenciaJSON struct {
	Pagina uint64 `json:"pagina"`
	// Trecho é omitido quando a expressão casou por frase mas não aparece como
	// subcadeia contígua — ver usecase.recortarTrecho.
	Trecho string `json:"trecho,omitempty"`
}

// respostaDeVerificacao é o documento devolvido.
//
// `encontrado` e `total_ocorrencias` são redundantes com `ocorrencias` de
// propósito: quem consome isto com `jq` ou pelo Postman quer a resposta no
// primeiro campo, não contando um array.
type respostaDeVerificacao struct {
	Expressao        string           `json:"expressao"`
	TermosBuscados   []string         `json:"termos_buscados"`
	Encontrado       bool             `json:"encontrado"`
	TotalOcorrencias int              `json:"total_ocorrencias"`
	TotalPaginas     int              `json:"total_paginas"`
	Ocorrencias      []ocorrenciaJSON `json:"ocorrencias"`
	Diagnostico      []string         `json:"diagnostico,omitempty"`
}

// verificarPDF atende POST /pdf-verificacao.
func (s *servico) verificarPDF(w http.ResponseWriter, r *http.Request) {
	lida, err := lerSubmissao(r, s.maxUploadBytes)
	if err != nil {
		// Mesmo tratamento de /pdf: o corpo acima do teto tem resposta própria,
		// o resto é falha de leitura.
		if errors.Is(err, ErrCorpoAcimaDoLimite) {
			var criticas domain.Criticas
			criticas.Adicionar(domain.CriticaPDFAcimaDoLimite)
			s.escritor.Escrever(w, r, http.StatusBadRequest,
				criticas.Mensagem(), criticas.Itens())
			return
		}
		s.logger.ErrorContext(r.Context(), "falha ao ler o corpo da verificação",
			slog.Any("erro", err))
		s.escritor.Escrever(w, r, http.StatusUnprocessableEntity, TextoFalhaAoVerificar, nil)
		return
	}

	resultado, err := s.verificador.Executar(r.Context(), usecase.ComandoVerificar{
		Submissao: lida.submissao,
		Conteudo:  lida.conteudo,
		Expressao: lida.expressao,
	})

	switch {
	case err == nil:

	case errors.Is(err, usecase.ErrExpressaoVazia):
		s.escritor.Escrever(w, r, http.StatusBadRequest, TextoExpressaoAusente, nil)
		return

	case errors.Is(err, usecase.ErrExpressaoInvalida):
		// A expressão não compila como filtro — mesma classe de recusa que o
		// laço de recorte encontra numa expressão cadastrada malformada.
		s.escritor.Escrever(w, r, http.StatusBadRequest, err.Error(), nil)
		return

	case errors.Is(err, domain.ErrValidacao):
		// As MESMAS críticas de /pdf, no mesmo formato.
		var validacao *domain.ErroDeValidacao
		if !errors.As(err, &validacao) {
			s.escritor.Escrever(w, r, http.StatusUnprocessableEntity, TextoFalhaAoVerificar, nil)
			return
		}
		s.escritor.Escrever(w, r, http.StatusBadRequest,
			validacao.Criticas.Mensagem(), validacao.Criticas.Itens())
		return

	default:
		s.logger.ErrorContext(r.Context(), "falha na verificação manual", slog.Any("erro", err))
		s.escritor.Escrever(w, r, http.StatusUnprocessableEntity, TextoFalhaAoVerificar, nil)
		return
	}

	// A rota responde 200 mesmo quando NÃO encontra: a pergunta foi respondida.
	// "Não achei" é resultado, não erro — e devolver 404 faria um cliente
	// automatizado tratar como falha o caso mais comum do diagnóstico.
	doc := respostaDeVerificacao{
		Expressao:        resultado.Expressao,
		TermosBuscados:   resultado.TermosBuscados,
		Encontrado:       resultado.Encontrou(),
		TotalOcorrencias: len(resultado.Ocorrencias),
		TotalPaginas:     resultado.TotalPaginas,
		Ocorrencias:      make([]ocorrenciaJSON, 0, len(resultado.Ocorrencias)),
		Diagnostico:      resultado.Diagnostico,
	}
	for _, o := range resultado.Ocorrencias {
		doc.Ocorrencias = append(doc.Ocorrencias, ocorrenciaJSON{Pagina: o.Pagina, Trecho: o.Trecho})
	}
	if doc.TermosBuscados == nil {
		doc.TermosBuscados = []string{}
	}

	corpo, err := json.Marshal(doc)
	if err != nil {
		// Inalcançável: o documento só tem tipos primitivos.
		s.logger.ErrorContext(r.Context(), "falha ao serializar a verificação", slog.Any("erro", err))
		s.escritor.Escrever(w, r, http.StatusInternalServerError, TextoFalhaAoVerificar, nil)
		return
	}

	// Como em /importacao/{id}: o SUCESSO é sempre JSON, independentemente de
	// RESPOSTA_PROBLEM_JSON. A chave governa o formato dos ERROS.
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(corpo)
}
