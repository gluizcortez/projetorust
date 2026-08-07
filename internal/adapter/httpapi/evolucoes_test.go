package httpapi_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gluizcortez/projetorust/internal/adapter/httpapi"
	"github.com/gluizcortez/projetorust/internal/adapter/httpapi/middleware"
	"github.com/gluizcortez/projetorust/internal/adapter/httpapi/resposta"
	"github.com/gluizcortez/projetorust/internal/domain"
	"github.com/gluizcortez/projetorust/internal/domain/domaintest"
	"github.com/gluizcortez/projetorust/internal/usecase"
)

// Este arquivo exercita as evoluções da fase F11 pela camada HTTP.
//
// Cada uma tem teste LIGADA e DESLIGADA. O teste desligado é o que importa:
// ele é a prova de que a chave, no padrão, não muda nada — e é o que a suíte de
// paridade da fase F12 vai depender.

// -------------------------------------------------------------------------
// VALIDAR_ASSINATURA_PDF
// -------------------------------------------------------------------------

// conteudoNaoPDF é um corpo plausível que NÃO é PDF: uma planilha CSV enviada
// por engano, que é o erro real que a chave existe para barrar.
const conteudoNaoPDF = "nome;valor\nfulano;10\n"

func submissaoComArquivo(conteudo string) string {
	return corpoMultipart(
		parteMultipart{nome: "data-caderno", valor: "2024-03-15"},
		parteMultipart{nome: "data-disponibilizacao", valor: "2024-03-16"},
		parteMultipart{nome: "id-usuario", valor: "7"},
		parteMultipart{nome: "id-caderno", valor: "9"},
		parteMultipart{
			nome:       "pdf",
			disposicao: `form-data; name="pdf"; filename="diario.pdf"`,
			valor:      conteudo,
		},
	)
}

// TestAssinaturaDesligadaAceitaQualquerArquivo é a paridade: o legado aceita
// qualquer coisa e só descobre no processamento assíncrono.
func TestAssinaturaDesligadaAceitaQualquerArquivo(t *testing.T) {
	p := montarPilhaCom(t, nil, nil)

	w := httptest.NewRecorder()
	p.roteador.ServeHTTP(w, requisicaoPDF(submissaoComArquivo(conteudoNaoPDF), texto(chaveDeTeste)))

	if w.Code != http.StatusOK {
		t.Errorf("status = %d; sem a chave, arquivo que não é PDF é ACEITO", w.Code)
	}
	if corpo := w.Body.String(); corpo != httpapi.TextoSucesso {
		t.Errorf("corpo = %q; esperava %q", corpo, httpapi.TextoSucesso)
	}
	if len(p.importacoes.Registradas) != 1 {
		t.Errorf("registradas = %d; esperava 1", len(p.importacoes.Registradas))
	}
}

// TestAssinaturaLigadaRecusaSemTextoNovo cobre a exigência literal da fase: a
// resposta é a MESMA 422 do legado, sem texto novo.
func TestAssinaturaLigadaRecusaSemTextoNovo(t *testing.T) {
	p := montarPilhaCom(t, func(d *usecase.DependenciasDaIngestao) {
		d.ValidarAssinaturaPDF = true
	}, nil)

	w := httptest.NewRecorder()
	p.roteador.ServeHTTP(w, requisicaoPDF(submissaoComArquivo(conteudoNaoPDF), texto(chaveDeTeste)))

	if w.Code != http.StatusUnprocessableEntity {
		t.Errorf("status = %d; esperava 422", w.Code)
	}
	// O ponto do teste: NENHUM texto novo. Se alguém introduzir uma crítica
	// própria para "não é PDF", esta comparação reprova.
	if corpo := w.Body.String(); corpo != httpapi.TextoErroAoProcessar {
		t.Errorf("corpo = %q; a fase exige o texto existente %q", corpo, httpapi.TextoErroAoProcessar)
	}
	if len(p.importacoes.Registradas) != 0 {
		t.Error("nada podia ser registrado: a recusa é anterior ao banco")
	}
	if len(p.processadas) != 0 {
		t.Error("nada podia ser processado")
	}
}

// TestAssinaturaLigadaAceitaPDFDeVerdade: a chave não pode recusar o caso bom.
func TestAssinaturaLigadaAceitaPDFDeVerdade(t *testing.T) {
	p := montarPilhaCom(t, func(d *usecase.DependenciasDaIngestao) {
		d.ValidarAssinaturaPDF = true
	}, nil)

	w := httptest.NewRecorder()
	p.roteador.ServeHTTP(w, requisicaoPDF(submissaoCompleta(), texto(chaveDeTeste)))

	if w.Code != http.StatusOK {
		t.Errorf("status = %d; %q começa com a assinatura e deve passar", w.Code, "%PDF-1.4")
	}
}

// TestAssinaturaLigadaNaoPrecedeAsCriticas fixa a ORDEM da decisão.
//
// Uma submissão que erra a data E manda arquivo que não é PDF continua
// recebendo o 400 com as críticas do legado. Trocá-lo pelo 422 seria fazer a
// chave alterar a resposta de uma requisição que o legado JÁ rejeitava — efeito
// além do que ela existe para ter.
func TestAssinaturaLigadaNaoPrecedeAsCriticas(t *testing.T) {
	corpo := corpoMultipart(
		parteMultipart{nome: "data-caderno", valor: "nao-e-data"},
		parteMultipart{nome: "data-disponibilizacao", valor: "2024-03-16"},
		parteMultipart{nome: "id-usuario", valor: "7"},
		parteMultipart{nome: "id-caderno", valor: "9"},
		parteMultipart{
			nome:       "pdf",
			disposicao: `form-data; name="pdf"; filename="d.pdf"`,
			valor:      conteudoNaoPDF,
		},
	)

	p := montarPilhaCom(t, func(d *usecase.DependenciasDaIngestao) {
		d.ValidarAssinaturaPDF = true
	}, nil)

	w := httptest.NewRecorder()
	p.roteador.ServeHTTP(w, requisicaoPDF(corpo, texto(chaveDeTeste)))

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d; esperava 400 com as críticas", w.Code)
	}
	if corpo := w.Body.String(); corpo != domain.CriticaDataCadernoInvalida {
		t.Errorf("corpo = %q; esperava a crítica do legado %q",
			corpo, domain.CriticaDataCadernoInvalida)
	}
}

// -------------------------------------------------------------------------
// IDEMPOTENCIA_POR_HASH
// -------------------------------------------------------------------------

// consultorFalso é o dublê da porta de idempotência.
type consultorFalso struct {
	resumo    domain.ResumoDaImportacao
	err       error
	consultas []domain.ChaveDeIdempotencia
}

func (c *consultorFalso) ImportacaoEquivalente(
	_ context.Context, chave domain.ChaveDeIdempotencia,
) (domain.ResumoDaImportacao, error) {
	c.consultas = append(c.consultas, chave)
	if c.err != nil {
		return domain.ResumoDaImportacao{}, c.err
	}
	return c.resumo, nil
}

// TestIdempotenciaDesligadaRegistraOReenvio é a paridade.
//
// Duas submissões IDÊNTICAS — mesmo conteúdo, mesmo caderno, mesma data —
// produzem DUAS importações e DOIS processamentos, que é o que o legado faz:
// ele grava o hash e nunca o consulta.
//
// O teste manda o mesmo corpo duas vezes em vez de afirmar que a porta não foi
// chamada; afirmar sobre um dublê que ninguém injetou seria verdade por
// construção, e passaria mesmo se a evolução vazasse.
func TestIdempotenciaDesligadaRegistraOReenvio(t *testing.T) {
	p := montarPilhaCom(t, func(d *usecase.DependenciasDaIngestao) {
		d.Equivalentes = nil // é o que a raiz de composição passa com a chave no padrão
	}, nil)

	for i := range 2 {
		w := httptest.NewRecorder()
		p.roteador.ServeHTTP(w, requisicaoPDF(submissaoCompleta(), texto(chaveDeTeste)))
		if w.Code != http.StatusOK {
			t.Fatalf("submissão %d: status = %d", i, w.Code)
		}
	}

	if len(p.importacoes.Registradas) != 2 {
		t.Errorf("registradas = %d; esperava 2 — sem a chave, o reenvio registra de novo",
			len(p.importacoes.Registradas))
	}
	if len(p.processadas) != 2 {
		t.Errorf("processadas = %d; esperava 2", len(p.processadas))
	}
}

// TestIdempotenciaLigadaEvitaOSegundoRegistro é o espelho do teste acima: o
// mesmo corpo, duas vezes, com a chave ligada, registra UMA só.
func TestIdempotenciaLigadaEvitaOSegundoRegistro(t *testing.T) {
	consultor := &consultorFalso{err: domain.ErrImportacaoNaoEncontrada}
	p := montarPilhaCom(t, func(d *usecase.DependenciasDaIngestao) {
		d.Equivalentes = consultor
	}, nil)

	// Primeira: nada equivalente ainda.
	w := httptest.NewRecorder()
	p.roteador.ServeHTTP(w, requisicaoPDF(submissaoCompleta(), texto(chaveDeTeste)))
	if w.Code != http.StatusOK {
		t.Fatalf("primeira submissão: status = %d", w.Code)
	}

	// Segunda: agora existe uma finalizada com o mesmo hash.
	consultor.err = nil
	consultor.resumo = domain.ResumoDaImportacao{
		ID: p.importacoes.IDGerado, Status: domain.StatusFinalizado,
	}
	w = httptest.NewRecorder()
	p.roteador.ServeHTTP(w, requisicaoPDF(submissaoCompleta(), texto(chaveDeTeste)))
	if w.Code != http.StatusOK {
		t.Fatalf("segunda submissão: status = %d", w.Code)
	}

	if len(p.importacoes.Registradas) != 1 {
		t.Errorf("registradas = %d; esperava 1 — o reenvio reaproveita",
			len(p.importacoes.Registradas))
	}
	if len(p.processadas) != 1 {
		t.Errorf("processadas = %d; esperava 1", len(p.processadas))
	}
	// As duas chaves consultadas têm de ser iguais: mesmo conteúdo, mesmo hash.
	if len(consultor.consultas) != 2 {
		t.Fatalf("consultas = %d; esperava 2", len(consultor.consultas))
	}
	if consultor.consultas[0] != consultor.consultas[1] {
		t.Errorf("as duas consultas divergiram: %+v e %+v",
			consultor.consultas[0], consultor.consultas[1])
	}
}

// TestIdempotenciaLigadaReaproveitaFinalizada é o caminho feliz da chave.
func TestIdempotenciaLigadaReaproveitaFinalizada(t *testing.T) {
	consultor := &consultorFalso{
		resumo: domain.ResumoDaImportacao{ID: 999, Status: domain.StatusFinalizado},
	}
	p := montarPilhaCom(t, func(d *usecase.DependenciasDaIngestao) {
		d.Equivalentes = consultor
	}, nil)

	w := httptest.NewRecorder()
	p.roteador.ServeHTTP(w, requisicaoPDF(submissaoCompleta(), texto(chaveDeTeste)))

	// A resposta é INDISTINGUÍVEL da de uma submissão nova — é o ponto da
	// idempotência: o cliente não precisa saber que houve reaproveitamento.
	if w.Code != http.StatusOK {
		t.Errorf("status = %d; esperava 200", w.Code)
	}
	if corpo := w.Body.String(); corpo != httpapi.TextoSucesso {
		t.Errorf("corpo = %q; esperava %q", corpo, httpapi.TextoSucesso)
	}
	if len(p.importacoes.Registradas) != 0 {
		t.Error("o reenvio não podia registrar importação nova")
	}
	if len(p.processadas) != 0 {
		t.Error("o reenvio não podia disparar processamento")
	}

	if len(consultor.consultas) != 1 {
		t.Fatalf("consultas = %d; esperava 1", len(consultor.consultas))
	}
	// Os três campos da chave, conferidos: só o hash levaria a deduplicar
	// documentos legitimamente reenviados para outro caderno ou outra data.
	c := consultor.consultas[0]
	if c.IDCaderno != 9 {
		t.Errorf("id_caderno = %d; esperava 9", c.IDCaderno)
	}
	if c.DataCaderno.String() != "2024-03-15" {
		t.Errorf("data_caderno = %q; esperava 2024-03-15", c.DataCaderno)
	}
	if len(c.HashSHA256) != 64 {
		t.Errorf("hash = %q; esperava 64 caracteres hexadecimais", c.HashSHA256)
	}
}

// TestIdempotenciaNaoReaproveitaInacabada percorre TODOS os status não
// terminais mais o de erro.
//
// É o núcleo da regra: só o 5 é resultado utilizável. Reaproveitar uma em curso
// faria o cliente herdar uma tarefa que ainda pode falhar; reaproveitar uma em
// -1 impediria justamente o reenvio que corrige o erro.
func TestIdempotenciaNaoReaproveitaInacabada(t *testing.T) {
	for _, status := range domain.TodosOsStatus {
		if status == domain.StatusFinalizado {
			continue
		}
		t.Run(status.String(), func(t *testing.T) {
			consultor := &consultorFalso{
				resumo: domain.ResumoDaImportacao{ID: 999, Status: status},
			}
			p := montarPilhaCom(t, func(d *usecase.DependenciasDaIngestao) {
				d.Equivalentes = consultor
			}, nil)

			w := httptest.NewRecorder()
			p.roteador.ServeHTTP(w, requisicaoPDF(submissaoCompleta(), texto(chaveDeTeste)))

			if w.Code != http.StatusOK {
				t.Fatalf("status = %d", w.Code)
			}
			if len(p.importacoes.Registradas) != 1 {
				t.Errorf("registradas = %d; status %s não pode ser reaproveitado",
					len(p.importacoes.Registradas), status)
			}
		})
	}
}

// TestIdempotenciaFalhandoNaoDerrubaASubmissao: a evolução é otimização, não
// regra de negócio. Banco fora do ar para o SELECT segue o caminho do legado.
func TestIdempotenciaFalhandoNaoDerrubaASubmissao(t *testing.T) {
	consultor := &consultorFalso{err: errors.New("conexão recusada")}
	p := montarPilhaCom(t, func(d *usecase.DependenciasDaIngestao) {
		d.Equivalentes = consultor
	}, nil)

	w := httptest.NewRecorder()
	p.roteador.ServeHTTP(w, requisicaoPDF(submissaoCompleta(), texto(chaveDeTeste)))

	if w.Code != http.StatusOK {
		t.Errorf("status = %d; a falha da consulta não pode virar erro para o cliente", w.Code)
	}
	if len(p.importacoes.Registradas) != 1 {
		t.Errorf("registradas = %d; esperava 1 — segue o caminho do legado",
			len(p.importacoes.Registradas))
	}
}

// -------------------------------------------------------------------------
// STATUS_ENDPOINT
// -------------------------------------------------------------------------

// consultorDeImportacaoFalso é o dublê da porta de GET /importacao/{id}.
type consultorDeImportacaoFalso struct {
	porID map[int64]domain.ResumoDaImportacao
	err   error
}

func (c *consultorDeImportacaoFalso) Consultar(
	_ context.Context, id int64,
) (domain.ResumoDaImportacao, error) {
	if c.err != nil {
		return domain.ResumoDaImportacao{}, c.err
	}
	resumo, existe := c.porID[id]
	if !existe {
		return domain.ResumoDaImportacao{}, fmt.Errorf("importação %d: %w",
			id, domain.ErrImportacaoNaoEncontrada)
	}
	return resumo, nil
}

func resumoDeExemplo() domain.ResumoDaImportacao {
	caderno, _ := domain.NovaData(2024, 3, 15)
	disponibilizacao, _ := domain.NovaData(2024, 3, 16)
	inicio := time.Date(2024, 3, 16, 10, 0, 0, 0, time.UTC)
	fim := time.Date(2024, 3, 16, 10, 2, 30, 0, time.UTC)
	total := int32(17)
	return domain.ResumoDaImportacao{
		ID:                   42,
		Status:               domain.StatusFinalizado,
		DataCaderno:          caderno,
		DataDisponibilizacao: disponibilizacao,
		DataInicio:           &inicio,
		DataFim:              &fim,
		TotalRecortes:        &total,
	}
}

func pilhaComStatus(t *testing.T, consultor httpapi.ConsultorDeImportacao) *pilha {
	t.Helper()
	return montarPilha(t, func(d *httpapi.Dependencias, _ *domaintest.RepositorioImportacaoFalso) {
		d.StatusEndpoint = true
		d.Importacoes = consultor
	})
}

// TestStatusEndpointDesligadoNaoRegistraARota: desligado, /importacao/42 é uma
// rota inexistente e recebe o catcher do Salvo, como qualquer outra.
func TestStatusEndpointDesligadoNaoRegistraARota(t *testing.T) {
	for _, caminho := range []string{"/importacao", "/importacao/42"} {
		t.Run(caminho, func(t *testing.T) {
			w := httptest.NewRecorder()
			r := httptest.NewRequest(http.MethodGet, caminho, nil)
			r.Header.Set("X-API-KEY", chaveDeTeste)
			roteador(t).ServeHTTP(w, r)

			if w.Code != http.StatusNotFound {
				t.Errorf("status = %d; esperava 404", w.Code)
			}
			// O corpo tem de ser o do catcher, não uma resposta própria.
			if tipo := w.Header().Get("Content-Type"); tipo != "text/html" {
				t.Errorf("Content-Type = %q; esperava a página do catcher", tipo)
			}
		})
	}
}

// TestStatusEndpointLigadoDevolveOResumo.
func TestStatusEndpointLigadoDevolveOResumo(t *testing.T) {
	p := pilhaComStatus(t, &consultorDeImportacaoFalso{
		porID: map[int64]domain.ResumoDaImportacao{42: resumoDeExemplo()},
	})

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/importacao/42", nil)
	r.Header.Set("X-API-KEY", chaveDeTeste)
	p.roteador.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d; corpo %q", w.Code, w.Body.String())
	}
	if tipo := w.Header().Get("Content-Type"); !strings.HasPrefix(tipo, "application/json") {
		t.Errorf("Content-Type = %q; esperava application/json", tipo)
	}

	var corpo map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &corpo); err != nil {
		t.Fatalf("corpo não é JSON: %v — %q", err, w.Body.String())
	}

	esperados := map[string]any{
		"id_importacao":         float64(42),
		"status":                float64(5),
		"status_rotulo":         domain.StatusFinalizado.String(),
		"data_caderno":          "2024-03-15",
		"data_disponibilizacao": "2024-03-16",
		"total_recortes":        float64(17),
		"concluida":             true,
	}
	for campo, valor := range esperados {
		if corpo[campo] != valor {
			t.Errorf("%s = %v; esperava %v", campo, corpo[campo], valor)
		}
	}

	// O hash e o nome do arquivo NÃO podem sair: ver domain.ResumoDaImportacao.
	for _, proibido := range []string{"hash", "nome_original_pdf", "arquivo_pdf"} {
		if _, presente := corpo[proibido]; presente {
			t.Errorf("o campo %q não pode aparecer na resposta", proibido)
		}
	}
}

// TestStatusEndpointOmiteNulos separa "não terminou" de "terminou sem
// ocorrências" — a distinção que INV-P20 torna importante.
func TestStatusEndpointOmiteNulos(t *testing.T) {
	casos := []struct {
		nome     string
		total    *int32
		presente bool
		valor    float64
	}{
		{"em curso, total nulo", nil, false, 0},
		{"finalizada sem recortes", pontuar(0), true, 0},
		{"finalizada com recortes", pontuar(3), true, 3},
	}

	for _, caso := range casos {
		t.Run(caso.nome, func(t *testing.T) {
			resumo := resumoDeExemplo()
			resumo.TotalRecortes = caso.total
			p := pilhaComStatus(t, &consultorDeImportacaoFalso{
				porID: map[int64]domain.ResumoDaImportacao{42: resumo},
			})

			w := httptest.NewRecorder()
			r := httptest.NewRequest(http.MethodGet, "/importacao/42", nil)
			r.Header.Set("X-API-KEY", chaveDeTeste)
			p.roteador.ServeHTTP(w, r)

			var corpo map[string]any
			if err := json.Unmarshal(w.Body.Bytes(), &corpo); err != nil {
				t.Fatalf("corpo não é JSON: %v", err)
			}
			valor, presente := corpo["total_recortes"]
			if presente != caso.presente {
				t.Fatalf("total_recortes presente = %t; esperava %t", presente, caso.presente)
			}
			if presente && valor != caso.valor {
				t.Errorf("total_recortes = %v; esperava %v", valor, caso.valor)
			}
		})
	}
}

func pontuar(v int32) *int32 { return &v }

// TestStatusEndpointRespostasDeErro cobre id inválido, ausente e falha de banco.
func TestStatusEndpointRespostasDeErro(t *testing.T) {
	casos := []struct {
		nome     string
		caminho  string
		falha    error
		esperado int
		corpo    string
	}{
		{"id não numérico", "/importacao/abc", nil, http.StatusBadRequest, httpapi.TextoIdentificadorInvalido},
		{"id negativo", "/importacao/-1", nil, http.StatusBadRequest, httpapi.TextoIdentificadorInvalido},
		{"id ausente", "/importacao", nil, http.StatusBadRequest, httpapi.TextoIdentificadorInvalido},
		{"id com sobra", "/importacao/42/extra", nil, http.StatusBadRequest, httpapi.TextoIdentificadorInvalido},
		{"não encontrada", "/importacao/7", nil, http.StatusNotFound, httpapi.TextoImportacaoNaoEncontrada},
		{"falha de banco", "/importacao/42", errors.New("conexão perdida"),
			http.StatusInternalServerError, httpapi.TextoFalhaAoConsultar},
	}

	for _, caso := range casos {
		t.Run(caso.nome, func(t *testing.T) {
			p := pilhaComStatus(t, &consultorDeImportacaoFalso{
				porID: map[int64]domain.ResumoDaImportacao{42: resumoDeExemplo()},
				err:   caso.falha,
			})

			w := httptest.NewRecorder()
			r := httptest.NewRequest(http.MethodGet, caso.caminho, nil)
			r.Header.Set("X-API-KEY", chaveDeTeste)
			p.roteador.ServeHTTP(w, r)

			if w.Code != caso.esperado {
				t.Errorf("status = %d; esperava %d (corpo %q)", w.Code, caso.esperado, w.Body.String())
			}
			if corpo := w.Body.String(); corpo != caso.corpo {
				t.Errorf("corpo = %q; esperava %q", corpo, caso.corpo)
			}
		})
	}
}

// TestStatusEndpointExigeCredencial: o estado de uma importação é informação do
// cliente, não pública.
func TestStatusEndpointExigeCredencial(t *testing.T) {
	p := pilhaComStatus(t, &consultorDeImportacaoFalso{
		porID: map[int64]domain.ResumoDaImportacao{42: resumoDeExemplo()},
	})

	w := httptest.NewRecorder()
	p.roteador.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/importacao/42", nil))

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d; esperava 401", w.Code)
	}
	if corpo := w.Body.String(); corpo != middleware.TextoChaveAusente {
		t.Errorf("corpo = %q; esperava %q", corpo, middleware.TextoChaveAusente)
	}
}

// TestStatusEndpointRecusaMetodoErrado: o método perde ANTES da autenticação,
// como em /pdf.
func TestStatusEndpointRecusaMetodoErrado(t *testing.T) {
	p := pilhaComStatus(t, &consultorDeImportacaoFalso{})

	w := httptest.NewRecorder()
	p.roteador.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/importacao/42", nil))

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d; esperava 405", w.Code)
	}
}

// TestStatusEndpointExigeAPorta: montagem incoerente falha no arranque, não na
// primeira requisição.
func TestStatusEndpointExigeAPorta(t *testing.T) {
	_, err := httpapi.NovoRouter(httpapi.Dependencias{
		Ingestor:       ingestorQueNuncaEChamado{},
		Logger:         loggerMudo(),
		APIKey:         chaveDeTeste,
		StatusEndpoint: true,
	})
	if err == nil {
		t.Fatal("NovoRouter aceitou STATUS_ENDPOINT sem Importacoes")
	}
	if !strings.Contains(err.Error(), "Importacoes") {
		t.Errorf("erro = %v; deveria nomear a dependência ausente", err)
	}
}

type ingestorQueNuncaEChamado struct{}

func (ingestorQueNuncaEChamado) Executar(context.Context, usecase.ComandoIngerir) (int64, error) {
	return 0, errors.New("não deveria ser chamado")
}

// -------------------------------------------------------------------------
// HEALTH_ENDPOINTS
// -------------------------------------------------------------------------

// TestHealthDesligadoNaoRegistraAsRotas.
func TestHealthDesligadoNaoRegistraAsRotas(t *testing.T) {
	for _, caminho := range []string{"/health/live", "/health/ready"} {
		t.Run(caminho, func(t *testing.T) {
			w := httptest.NewRecorder()
			roteador(t).ServeHTTP(w, httptest.NewRequest(http.MethodGet, caminho, nil))

			if w.Code != http.StatusNotFound {
				t.Errorf("status = %d; esperava 404", w.Code)
			}
		})
	}
}

// TestPingNaoMudaComHealthLigado é a exigência literal da fase: `/ping`
// PERMANECE EXATAMENTE COMO ESTÁ.
//
// A verificação de prontidão devolve erro de propósito: mesmo com a dependência
// reprovando, /ping tem de responder 200 e "pong" — porque ele não verifica
// nada, e é contrato existente.
func TestPingNaoMudaComHealthLigado(t *testing.T) {
	p := montarPilha(t, func(d *httpapi.Dependencias, _ *domaintest.RepositorioImportacaoFalso) {
		d.HealthEndpoints = true
		d.Pronto = func(context.Context) error { return errors.New("banco fora do ar") }
	})

	w := httptest.NewRecorder()
	p.roteador.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/ping", nil))

	if w.Code != http.StatusOK {
		t.Errorf("status = %d; /ping não verifica nada", w.Code)
	}
	if corpo := w.Body.String(); corpo != httpapi.TextoPong {
		t.Errorf("corpo = %q; esperava %q", corpo, httpapi.TextoPong)
	}
	if tipo := w.Header().Get("Content-Type"); tipo != resposta.TipoTexto {
		t.Errorf("Content-Type = %q; esperava %q", tipo, resposta.TipoTexto)
	}
}

// TestHealthLigado cobre as duas sondas, aprovando e reprovando.
func TestHealthLigado(t *testing.T) {
	casos := []struct {
		nome     string
		falha    error
		caminho  string
		esperado int
		corpo    string
	}{
		{"live com tudo bem", nil, "/health/live", http.StatusOK, httpapi.TextoVivo},
		{"live com banco fora", errors.New("fora"), "/health/live", http.StatusOK, httpapi.TextoVivo},
		{"ready com tudo bem", nil, "/health/ready", http.StatusOK, httpapi.TextoPronto},
		{"ready com banco fora", errors.New("fora"), "/health/ready",
			http.StatusServiceUnavailable, httpapi.TextoIndisponivel},
	}

	for _, caso := range casos {
		t.Run(caso.nome, func(t *testing.T) {
			p := montarPilha(t, func(d *httpapi.Dependencias, _ *domaintest.RepositorioImportacaoFalso) {
				d.HealthEndpoints = true
				d.Pronto = func(context.Context) error { return caso.falha }
			})

			w := httptest.NewRecorder()
			p.roteador.ServeHTTP(w, httptest.NewRequest(http.MethodGet, caso.caminho, nil))

			if w.Code != caso.esperado {
				t.Errorf("status = %d; esperava %d", w.Code, caso.esperado)
			}
			if corpo := w.Body.String(); corpo != caso.corpo {
				t.Errorf("corpo = %q; esperava %q", corpo, caso.corpo)
			}
		})
	}
}

// TestHealthLiveNaoConsultaNada é o motivo de as duas sondas serem separadas:
// uma sonda de vivacidade que dependa do banco faz o orquestrador REINICIAR o
// serviço quando o banco cai.
func TestHealthLiveNaoConsultaNada(t *testing.T) {
	var chamadas int
	p := montarPilha(t, func(d *httpapi.Dependencias, _ *domaintest.RepositorioImportacaoFalso) {
		d.HealthEndpoints = true
		d.Pronto = func(context.Context) error { chamadas++; return nil }
	})

	w := httptest.NewRecorder()
	p.roteador.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/health/live", nil))

	if chamadas != 0 {
		t.Errorf("a verificação foi chamada %d vez(es); /health/live não pode consultar nada", chamadas)
	}
	if w.Code != http.StatusOK {
		t.Errorf("status = %d", w.Code)
	}
}

// TestHealthNaoExigeCredencial: a sonda é chamada pelo orquestrador, que não
// tem a chave de API — e não deve ter.
func TestHealthNaoExigeCredencial(t *testing.T) {
	p := montarPilha(t, func(d *httpapi.Dependencias, _ *domaintest.RepositorioImportacaoFalso) {
		d.HealthEndpoints = true
		d.Pronto = func(context.Context) error { return nil }
	})

	for _, caminho := range []string{"/health/live", "/health/ready"} {
		w := httptest.NewRecorder()
		p.roteador.ServeHTTP(w, httptest.NewRequest(http.MethodGet, caminho, nil))
		if w.Code != http.StatusOK {
			t.Errorf("%s sem credencial devolveu %d", caminho, w.Code)
		}
	}
}

// TestHealthExigeAVerificacao: montagem incoerente falha no arranque.
func TestHealthExigeAVerificacao(t *testing.T) {
	_, err := httpapi.NovoRouter(httpapi.Dependencias{
		Ingestor:        ingestorQueNuncaEChamado{},
		Logger:          loggerMudo(),
		APIKey:          chaveDeTeste,
		HealthEndpoints: true,
	})
	if err == nil {
		t.Fatal("NovoRouter aceitou HEALTH_ENDPOINTS sem Pronto")
	}
	if !strings.Contains(err.Error(), "Pronto") {
		t.Errorf("erro = %v; deveria nomear a dependência ausente", err)
	}
}

// -------------------------------------------------------------------------
// RATE_LIMIT_RPS
// -------------------------------------------------------------------------

// TestTaxaDesligadaNaoRecusa dispara MUITO acima de qualquer limite razoável.
func TestTaxaDesligadaNaoRecusa(t *testing.T) {
	h := roteador(t)
	for i := range 200 {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/ping", nil))
		if w.Code != http.StatusOK {
			t.Fatalf("requisição %d devolveu %d; sem RATE_LIMIT_RPS nada é recusado", i, w.Code)
		}
	}
}

// TestTaxaLigadaRecusaAcimaDoLimite.
func TestTaxaLigadaRecusaAcimaDoLimite(t *testing.T) {
	const limite = 5
	p := montarPilha(t, func(d *httpapi.Dependencias, _ *domaintest.RepositorioImportacaoFalso) {
		d.RateLimitRPS = limite
	})

	var recusadas int
	var primeiroRetryAfter string
	for range limite + 3 {
		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodGet, "/ping", nil)
		r.Header.Set("X-API-KEY", chaveDeTeste)
		p.roteador.ServeHTTP(w, r)

		if w.Code == http.StatusTooManyRequests {
			recusadas++
			if primeiroRetryAfter == "" {
				primeiroRetryAfter = w.Header().Get(middleware.CabecalhoRetryAfter)
			}
			if corpo := w.Body.String(); corpo != middleware.TextoTaxaExcedida {
				t.Errorf("corpo = %q; esperava %q", corpo, middleware.TextoTaxaExcedida)
			}
		}
	}

	if recusadas != 3 {
		t.Errorf("recusadas = %d; esperava 3 (as %d primeiras passam)", recusadas, limite)
	}

	// Retry-After precisa ser um inteiro POSITIVO de segundos: um "0" convida o
	// cliente a tentar de novo na hora e levar outro 429.
	segundos, err := strconv.Atoi(primeiroRetryAfter)
	if err != nil || segundos < 1 {
		t.Errorf("Retry-After = %q; esperava um inteiro de segundos ≥ 1", primeiroRetryAfter)
	}
}

// TestTaxaSeparaPorChaveDeAPI: um cliente barulhento não pode consumir o balde
// de outro.
func TestTaxaSeparaPorChaveDeAPI(t *testing.T) {
	const limite = 3
	p := montarPilha(t, func(d *httpapi.Dependencias, _ *domaintest.RepositorioImportacaoFalso) {
		d.RateLimitRPS = limite
	})

	// Esgota o balde de uma chave qualquer — inválida, o que basta: o
	// limitador roda ANTES da autenticação.
	for range limite + 5 {
		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodGet, "/ping", nil)
		r.Header.Set("X-API-KEY", "outro-cliente")
		p.roteador.ServeHTTP(w, r)
	}

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/ping", nil)
	r.Header.Set("X-API-KEY", chaveDeTeste)
	p.roteador.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d; o balde de outra chave não pode afetar esta", w.Code)
	}
}

// -------------------------------------------------------------------------
// Matriz de combinações
// -------------------------------------------------------------------------

// TestMatrizProblemJSONComCriticas é o par (problem+json × críticas de
// validação) exigido nos critérios de aceite.
//
// A ORDEM das críticas dentro do array é contrato tanto quanto a ordem dentro da
// cadeia concatenada — trocar o formato não pode reordenar nada.
func TestMatrizProblemJSONComCriticas(t *testing.T) {
	// Corpo com QUATRO críticas, em ordem: data do caderno, data de
	// disponibilização, id do usuário, id do caderno.
	corpo := corpoMultipart(
		parteMultipart{nome: "data-caderno", valor: "nao-e-data"},
		parteMultipart{nome: "data-disponibilizacao", valor: "tambem-nao"},
		parteMultipart{nome: "id-usuario", valor: "abc"},
		parteMultipart{nome: "id-caderno", valor: "xyz"},
		parteMultipart{
			nome:       "pdf",
			disposicao: `form-data; name="pdf"; filename="d.pdf"`,
			valor:      "%PDF-1.4",
		},
	)

	esperadas := []string{
		domain.CriticaDataCadernoInvalida,
		domain.CriticaDataDisponibilizacaoInvalida,
		domain.CriticaIDUsuarioInvalido,
		domain.CriticaIDCadernoInvalido,
	}

	t.Run("texto, o padrão", func(t *testing.T) {
		w := httptest.NewRecorder()
		roteador(t).ServeHTTP(w, requisicaoPDF(corpo, texto(chaveDeTeste)))

		if w.Code != http.StatusBadRequest {
			t.Fatalf("status = %d", w.Code)
		}
		if got := w.Body.String(); got != strings.Join(esperadas, domain.SeparadorDeCriticas) {
			t.Errorf("corpo = %q", got)
		}
	})

	t.Run("problem+json", func(t *testing.T) {
		p := montarPilha(t, func(d *httpapi.Dependencias, _ *domaintest.RepositorioImportacaoFalso) {
			d.RespostaProblemJSON = true
		})

		w := httptest.NewRecorder()
		p.roteador.ServeHTTP(w, requisicaoPDF(corpo, texto(chaveDeTeste)))

		if w.Code != http.StatusBadRequest {
			t.Fatalf("status = %d", w.Code)
		}
		if tipo := w.Header().Get("Content-Type"); tipo != resposta.TipoProblemJSON {
			t.Errorf("Content-Type = %q; esperava %q", tipo, resposta.TipoProblemJSON)
		}

		var doc struct {
			Type     string   `json:"type"`
			Title    string   `json:"title"`
			Status   int      `json:"status"`
			Detail   string   `json:"detail"`
			Instance string   `json:"instance"`
			Criticas []string `json:"criticas"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &doc); err != nil {
			t.Fatalf("corpo não é JSON: %v — %q", err, w.Body.String())
		}

		if doc.Status != http.StatusBadRequest {
			t.Errorf("status = %d", doc.Status)
		}
		if doc.Type != "about:blank" {
			t.Errorf("type = %q", doc.Type)
		}
		// `instance` é a exigência da especificação da fase.
		if doc.Instance != "/pdf" {
			t.Errorf("instance = %q; esperava /pdf", doc.Instance)
		}
		// O `detail` preserva o texto do legado, byte a byte.
		if doc.Detail != strings.Join(esperadas, domain.SeparadorDeCriticas) {
			t.Errorf("detail = %q", doc.Detail)
		}
		if len(doc.Criticas) != len(esperadas) {
			t.Fatalf("críticas = %v; esperava %d itens", doc.Criticas, len(esperadas))
		}
		for i, esperada := range esperadas {
			if doc.Criticas[i] != esperada {
				t.Errorf("crítica %d = %q; esperava %q", i, doc.Criticas[i], esperada)
			}
		}
	})
}

// TestMatrizTetoComLimiteDeCorpo é o par (teto × limite de corpo).
//
// O teto de concorrência vive no executor e o limite de corpo no middleware;
// juntos, o risco é o corpo grande ser lido ANTES de a submissão pegar vaga,
// segurando memória na fila. A recusa por tamanho tem de vir primeiro.
func TestMatrizTetoComLimiteDeCorpo(t *testing.T) {
	grande := strings.Repeat("A", 64<<10)
	corpo := corpoMultipart(
		parteMultipart{nome: "data-caderno", valor: "2024-03-15"},
		parteMultipart{nome: "data-disponibilizacao", valor: "2024-03-16"},
		parteMultipart{nome: "id-usuario", valor: "7"},
		parteMultipart{nome: "id-caderno", valor: "9"},
		parteMultipart{nome: "pdf", disposicao: `form-data; name="pdf"; filename="d.pdf"`, valor: grande},
	)

	// O executor recusa TUDO: se a submissão chegasse a ele, a resposta seria
	// 422 e não o 400 do limite de corpo.
	p := montarPilhaCom(t, func(d *usecase.DependenciasDaIngestao) {
		d.Executor = executorQueRecusa{}
	}, func(d *httpapi.Dependencias, _ *domaintest.RepositorioImportacaoFalso) {
		d.MaxUploadBytes = 1024
	})

	w := httptest.NewRecorder()
	p.roteador.ServeHTTP(w, requisicaoPDF(corpo, texto(chaveDeTeste)))

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d; o limite de corpo tem de decidir antes do executor", w.Code)
	}
	if corpo := w.Body.String(); corpo != domain.CriticaPDFAcimaDoLimite {
		t.Errorf("corpo = %q; esperava %q", corpo, domain.CriticaPDFAcimaDoLimite)
	}
	if len(p.importacoes.Registradas) != 0 {
		t.Error("nada podia ser registrado")
	}
}

// executorQueRecusa reprova toda submissão, para provar que ela não chegou lá.
type executorQueRecusa struct{}

func (executorQueRecusa) Submeter(context.Context, context.Context, func(context.Context)) error {
	return errors.New("executor não deveria ter sido alcançado")
}

// TestMatrizAssinaturaComProblemJSON: a recusa por assinatura também muda de
// formato com a outra chave ligada, e continua sem texto novo.
func TestMatrizAssinaturaComProblemJSON(t *testing.T) {
	p := montarPilhaCom(t, func(d *usecase.DependenciasDaIngestao) {
		d.ValidarAssinaturaPDF = true
	}, func(d *httpapi.Dependencias, _ *domaintest.RepositorioImportacaoFalso) {
		d.RespostaProblemJSON = true
	})

	w := httptest.NewRecorder()
	p.roteador.ServeHTTP(w, requisicaoPDF(submissaoComArquivo(conteudoNaoPDF), texto(chaveDeTeste)))

	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d; esperava 422", w.Code)
	}

	var doc struct {
		Detail   string   `json:"detail"`
		Instance string   `json:"instance"`
		Criticas []string `json:"criticas"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &doc); err != nil {
		t.Fatalf("corpo não é JSON: %v", err)
	}
	if doc.Detail != httpapi.TextoErroAoProcessar {
		t.Errorf("detail = %q; esperava o texto existente %q", doc.Detail, httpapi.TextoErroAoProcessar)
	}
	if doc.Instance != "/pdf" {
		t.Errorf("instance = %q", doc.Instance)
	}
	if len(doc.Criticas) != 0 {
		t.Errorf("críticas = %v; o 422 não carrega lista", doc.Criticas)
	}
}

// TestTodasAsChavesNoPadraoNaoMudamNada é a asserção que a fase F12 herda.
//
// Monta o roteador com a estrutura ZERADA — que é o que a configuração no
// padrão produz — e confere as respostas de contrato uma a uma.
func TestTodasAsChavesNoPadraoNaoMudamNada(t *testing.T) {
	h := roteador(t)

	casos := []struct {
		metodo, caminho string
		comChave        bool
		status          int
		corpo           string
	}{
		{http.MethodGet, "/ping", false, http.StatusOK, httpapi.TextoPong},
		{http.MethodGet, "/importacao/1", true, http.StatusNotFound, ""},
		{http.MethodGet, "/health/live", false, http.StatusNotFound, ""},
		{http.MethodGet, "/health/ready", false, http.StatusNotFound, ""},
		{http.MethodGet, "/metrics", false, http.StatusNotFound, ""},
	}

	for _, caso := range casos {
		t.Run(caso.metodo+" "+caso.caminho, func(t *testing.T) {
			w := httptest.NewRecorder()
			r := httptest.NewRequest(caso.metodo, caso.caminho, nil)
			if caso.comChave {
				r.Header.Set("X-API-KEY", chaveDeTeste)
			}
			h.ServeHTTP(w, r)

			if w.Code != caso.status {
				t.Errorf("status = %d; esperava %d", w.Code, caso.status)
			}
			if caso.corpo != "" && w.Body.String() != caso.corpo {
				t.Errorf("corpo = %q; esperava %q", w.Body.String(), caso.corpo)
			}
			// Nenhuma resposta pode trazer cabeçalho de evolução.
			if v := w.Header().Get(middleware.CabecalhoRetryAfter); v != "" {
				t.Errorf("Retry-After = %q; nenhuma chave está ligada", v)
			}
		})
	}
}
