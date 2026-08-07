package httpapi

import (
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"strings"

	"github.com/gluizcortez/projetorust/internal/domain"
)

// NomeDoCampoArquivo é a parte multipart que carrega o documento.
const NomeDoCampoArquivo = "pdf"

// Nomes dos campos de texto. Usam HÍFEN, não sublinhado
// (reference/main.rs:138-142).
const (
	CampoDataCaderno          = "data-caderno"
	CampoDataDisponibilizacao = "data-disponibilizacao"
	CampoIDUsuario            = "id-usuario"
	CampoIDCaderno            = "id-caderno"
)

// submissaoLida é o resultado da análise do corpo multipart.
type submissaoLida struct {
	submissao domain.SubmissaoPDF
	conteudo  []byte
}

// ErrCorpoInvalido indica corpo multipart que não pôde ser analisado.
var ErrCorpoInvalido = errors.New("corpo multipart inválido")

// ErrCorpoAcimaDoLimite indica corpo que estourou MAX_UPLOAD_BYTES.
//
// É SEPARADO de ErrCorpoInvalido porque as duas falhas têm respostas
// diferentes: corpo malformado é o 422 do legado, e corpo grande demais é o 400
// com crítica da opção B de D-16. Com a chave no padrão (zero) este erro é
// inalcançável — não há limite a estourar.
var ErrCorpoAcimaDoLimite = errors.New("corpo acima do limite configurado")

// erroDeLeitura classifica uma falha de leitura do corpo.
//
// `http.MaxBytesError` é o que `http.MaxBytesReader` produz quando o LIMITE DA
// REQUISIÇÃO INTEIRA estoura, instalado pelo middleware LimiteDeCorpo. Ele pode
// aparecer em qualquer leitura — ao avançar para a próxima parte, ao ler um
// campo de texto ou ao copiar o arquivo —, então a classificação fica num só
// lugar em vez de repetida em cada ponto de erro.
func erroDeLeitura(err error) error {
	var excedido *http.MaxBytesError
	if errors.As(err, &excedido) {
		return fmt.Errorf("%w: %w", ErrCorpoAcimaDoLimite, err)
	}
	return fmt.Errorf("%w: %w", ErrCorpoInvalido, err)
}

// lerSubmissao analisa o corpo multipart reproduzindo as decisões do Salvo.
//
// # Por que não `r.ParseMultipartForm`
//
// Porque ele diverge do legado num caso MEDIDO. O Salvo classifica uma parte
// como ARQUIVO quando a `Content-Disposition` traz o parâmetro `filename`,
// mesmo VAZIO. O `ParseMultipartForm` do Go usa `Part.FileName()`, que devolve
// a cadeia vazia tanto para `filename=""` quanto para `filename` ausente, e
// portanto joga os dois em `Value`:
//
//	Content-Disposition               Salvo (legado)          Go ParseMultipartForm
//	filename="diario.pdf"             arquivo "diario.pdf"    arquivo "diario.pdf"
//	filename=""                       ARQUIVO, nome ""        VALOR — não é arquivo
//	(sem filename)                    valor                   valor
//	filename=" "                      arquivo " "             arquivo " "
//
// A linha do meio muda a resposta: no legado a requisição é aceita e grava nome
// vazio em `nome_original_pdf`; com `ParseMultipartForm` viraria 400 com
// "PDF não enviado". Analisar as partes à mão é o que preserva isso.
//
// # A crítica que não tem como acontecer
//
// `PDF não possui nome` (reference/main.rs:207) exige `req.file("pdf")`
// devolvendo `Some` com `name()` a `None`. A sonda tentou as três formas de
// chegar lá — `filename` ausente, vazio e com espaço — e NENHUMA produz esse
// estado: sem `filename` não há arquivo, e com `filename` sempre há nome. A
// crítica é **inalcançável**, como o ramo de main.rs:226-230.
//
// Ela continua implementada no domínio, porque o domínio não sabe de onde vem a
// submissão e um chamador futuro pode alcançá-la. O que esta função garante é
// que o caminho HTTP se comporta como o legado.
//
// # Primeiro valor prevalece
//
// Campo repetido usa o PRIMEIRO valor, medido na sonda contra multipart real.
// A leitura sequencial das partes preserva isso naturalmente: uma vez definido,
// o campo não é sobrescrito.
func lerSubmissao(r *http.Request, maxMemoria int64) (submissaoLida, error) {
	partes, err := r.MultipartReader()
	if err != nil {
		return submissaoLida{}, erroDeLeitura(err)
	}

	var (
		lida      submissaoLida
		conteudo  strings.Builder
		temPDF    bool
		nomePDF   *string
		definidos = map[string]bool{}
	)

	for {
		parte, err := partes.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return submissaoLida{}, erroDeLeitura(err)
		}

		nome := parte.FormName()

		if nome == NomeDoCampoArquivo && parteEhArquivo(parte) {
			if temPDF {
				// Primeiro prevalece, como nos campos de texto.
				_ = parte.Close()
				continue
			}
			temPDF = true
			arquivo := nomeDoArquivo(parte)
			nomePDF = &arquivo

			// O legado lê o arquivo INTEIRO em memória antes de calcular o
			// resumo (reference/main.rs:233). O teto existe para que um corpo
			// gigante não derrube o processo; zero desliga, que é o legado.
			if err := copiarLimitado(&conteudo, parte, maxMemoria); err != nil {
				_ = parte.Close()
				return submissaoLida{}, err
			}
			_ = parte.Close()
			continue
		}

		if !ehCampoConhecido(nome) || definidos[nome] {
			_ = parte.Close()
			continue
		}

		valor, err := io.ReadAll(io.LimitReader(parte, limiteDeCampoDeTexto))
		_ = parte.Close()
		if err != nil {
			return submissaoLida{}, fmt.Errorf("campo %q: %w", nome, erroDeLeitura(err))
		}
		definidos[nome] = true
		guardarCampo(&lida.submissao, nome, string(valor))
	}

	lida.submissao.ArquivoEnviado = temPDF
	lida.submissao.NomeDoArquivo = nomePDF
	lida.conteudo = []byte(conteudo.String())
	return lida, nil
}

// limiteDeCampoDeTexto protege contra um campo de texto de tamanho absurdo.
//
// Um megabyte é ordens de grandeza acima de uma data ou de um identificador, e
// nenhuma requisição legítima chega perto — então o limite não altera
// comportamento observável, só impede que um campo de texto consuma memória
// como se fosse o arquivo.
const limiteDeCampoDeTexto = 1 << 20

// parteEhArquivo reproduz a regra do Salvo: é arquivo quando a
// `Content-Disposition` TEM o parâmetro `filename`, ainda que vazio.
//
// `parte.FileName()` do Go não serve aqui: ele devolve a cadeia vazia nos dois
// casos que precisamos distinguir.
func parteEhArquivo(parte *multipart.Part) bool {
	_, params, err := mime.ParseMediaType(parte.Header.Get("Content-Disposition"))
	if err != nil {
		return false
	}
	_, tem := params["filename"]
	return tem
}

// nomeDoArquivo devolve o valor do parâmetro `filename`, que pode ser vazio.
func nomeDoArquivo(parte *multipart.Part) string {
	_, params, err := mime.ParseMediaType(parte.Header.Get("Content-Disposition"))
	if err != nil {
		return ""
	}
	return params["filename"]
}

func ehCampoConhecido(nome string) bool {
	switch nome {
	case CampoDataCaderno, CampoDataDisponibilizacao, CampoIDUsuario, CampoIDCaderno:
		return true
	default:
		return false
	}
}

func guardarCampo(s *domain.SubmissaoPDF, nome, valor string) {
	v := valor
	switch nome {
	case CampoDataCaderno:
		s.DataCaderno = &v
	case CampoDataDisponibilizacao:
		s.DataDisponibilizacao = &v
	case CampoIDUsuario:
		s.IDUsuario = &v
	case CampoIDCaderno:
		s.IDCaderno = &v
	}
}

// copiarLimitado copia o conteúdo respeitando o teto. Zero desliga o teto.
func copiarLimitado(destino *strings.Builder, origem io.Reader, maxBytes int64) error {
	if maxBytes <= 0 {
		if _, err := io.Copy(destino, origem); err != nil {
			return fmt.Errorf("lendo o arquivo: %w", erroDeLeitura(err))
		}
		return nil
	}

	// Lê um byte a mais que o permitido: se ele vier, o limite foi excedido.
	n, err := io.Copy(destino, io.LimitReader(origem, maxBytes+1))
	if err != nil {
		return fmt.Errorf("lendo o arquivo: %w", erroDeLeitura(err))
	}
	if n > maxBytes {
		return fmt.Errorf("%w: arquivo acima de %d bytes", ErrCorpoAcimaDoLimite, maxBytes)
	}
	return nil
}
