package domain

import (
	"context"
	"time"
)

// Este arquivo declara as portas do núcleo: as interfaces que os adaptadores
// implementam. Todas são pequenas e definidas pelo consumidor.
//
// O contrato de cada método está no comentário: o que é erro, o que é
// resultado vazio, e o que acontece com entrada degenerada. Onde o
// comportamento vem do legado, a linha de origem é citada.

// RepositorioImportacao persiste e atualiza o registro de importação.
type RepositorioImportacao interface {
	// Registrar insere a importação e devolve o identificador gerado.
	// Reproduz reference/main.rs:447-477.
	Registrar(ctx context.Context, imp Importacao) (int64, error)

	// AtualizarStatus grava o novo estado. Reproduz main.rs:651-680.
	AtualizarStatus(ctx context.Context, id int64, s StatusImportacao) error

	// MarcarInicio grava data_inicio com current_timestamp AVALIADO NO
	// SERVIDOR de banco, nunca com a hora do processo. Reproduz main.rs:682-698.
	MarcarInicio(ctx context.Context, id int64) error

	// MarcarTermino grava data_fim e total_recortes.
	//
	// Reproduz main.rs:700-718, inclusive o comportamento de INV-P18: total
	// acima de 2.147.483.647 devolve ErrEstouroNumerico e NÃO executa comando
	// algum, deixando as duas colunas intocadas.
	MarcarTermino(ctx context.Context, id int64, totalRecortes int32) error
}

// RepositorioPerfil lê as expressões a procurar em um documento.
type RepositorioPerfil interface {
	// ChavesPesquisa devolve os pares perfil/expressão do caderno da
	// importação, ORDENADOS por (id_perfil, expressao_nm).
	//
	// A ordem é normativa: governa a deduplicação por perfil (INV-P12) e
	// portanto qual expressão é gravada para uma página disputada. Vem do
	// ORDER BY de main.rs:560-562 e não pode ser alterada.
	//
	// Nenhuma chave encontrada devolve fatia vazia e erro nil.
	ChavesPesquisa(ctx context.Context, idImportacao int64) ([]ChavePesquisa, error)
}

// RepositorioRecorte grava os recortes de uma chave de pesquisa.
type RepositorioRecorte interface {
	// Salvar grava os recortes e devolve quantos foram gravados.
	//
	// Reproduz main.rs:573-631. Cada recorte gera duas inserções, em
	// tb_recorte e tb_recorte_texto, que precisam ser atômicas entre si
	// (achado A03). O escopo transacional é UMA CHAMADA — não a importação
	// inteira, o que preservaria trabalho parcial diferente do legado
	// (INV-P14).
	//
	// Fatia vazia não é chamada pelo caso de uso (main.rs:308); se for,
	// devolve 0 sem tocar no banco.
	Salvar(ctx context.Context, idImportacao int64, chave ChavePesquisa, recortes []Recorte) (int32, error)
}

// ExtratorTexto obtém o texto de um documento PDF.
type ExtratorTexto interface {
	// ExtrairPaginas devolve o texto JÁ NORMALIZADO de cada página, na ordem
	// do documento. Reproduz main.rs:479-506.
	//
	// Um PDF TRUNCADO é aceito e devolve fatia VAZIA com erro nil — não é
	// ErrPDFInvalido. Arquivo vazio e arquivo que não é PDF devolvem
	// ErrPDFInvalido. Ver INV-P20.
	ExtrairPaginas(ctx context.Context, conteudo []byte) ([]string, error)
}

// Indexador constrói o índice de busca de um documento.
type Indexador interface {
	// Construir indexa as páginas, numerando a partir de 1.
	// Fatia vazia devolve um índice vazio, não erro.
	Construir(ctx context.Context, paginas []string) (Indice, error)
}

// Indice é o índice em memória de um documento, descartado ao fim.
type Indice interface {
	// Frase devolve os acertos de uma busca de FRASE — sequência de termos em
	// posições consecutivas, distância zero. NÃO é busca de subcadeia
	// (INV-P05).
	//
	// Uma página gera no máximo um recorte por expressão, mesmo com várias
	// ocorrências. Expressão que tokeniza para vazio devolve fatia vazia e
	// erro nil, nunca erro nem correspondência universal (INV-P06).
	//
	// Seguro para uso concorrente sobre o mesmo índice.
	Frase(ctx context.Context, expressao string) ([]Recorte, error)

	// Fechar libera os recursos do índice.
	Fechar() error
}

// UnidadeDeTrabalho executa uma função dentro de uma transação.
type UnidadeDeTrabalho interface {
	// EmTransacao propaga a transação pelo contexto. Erro ou pânico dentro da
	// função revertem tudo.
	EmTransacao(ctx context.Context, fn func(ctx context.Context) error) error
}

// Relogio fornece a hora atual.
//
// Existe para tornar determinísticos os testes que observam duração. NÃO
// substitui o current_timestamp do banco em data_inicio e data_fim, que é
// avaliado no servidor e continua sendo.
type Relogio interface {
	Agora() time.Time
}
