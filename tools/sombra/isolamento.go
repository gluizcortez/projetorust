package sombra

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// A restrição mais dura da execução em sombra:
//
//	"A instância em sombra NUNCA escreve no banco de produção. Isolamento
//	 verificado por credencial somente leitura no banco real."
//
// Uma restrição que ninguém verifica é uma intenção. O que segue transforma a
// intenção em teste: ele TENTA escrever e exige que o banco recuse.

// ErrIsolamentoViolado indica que a credencial consegue escrever onde não deve.
var ErrIsolamentoViolado = errors.New("isolamento violado: a credencial escreve no banco de produção")

// Executor é o mínimo para tentar uma escrita.
//
// Separado de Consultador porque a verificação precisa EXECUTAR, não consultar.
type Executor interface {
	Exec(ctx context.Context, sql string, args ...any) (comandoExecutado, error)
}

// comandoExecutado é o que o pgx devolve em Exec. Declarado como interface
// vazia porque a verificação só olha o ERRO — se a escrita passou, já falhou.
type comandoExecutado any

// tabelasProtegidas são as que a sombra jamais pode tocar em produção.
//
// A lista é a das três que o serviço escreve. As de perfil e cliente são
// somente leitura para o serviço inteiro, inclusive em produção, e por isso não
// entram aqui: escrevê-las já seria defeito com ou sem sombra.
var tabelasProtegidas = []string{
	"recorte.tb_importacao",
	"recorte.tb_recorte",
	"recorte.tb_recorte_texto",
}

// VerificarIsolamento tenta escrever em cada tabela protegida e exige recusa.
//
// # Por que dentro de uma transação revertida
//
// Se a credencial for indevidamente de escrita, a tentativa vai SUCEDER — e uma
// linha de lixo entraria no banco de produção, que é exatamente o que se quer
// evitar. A transação revertida garante que a verificação não cause o dano que
// ela procura.
//
// # Por que UPDATE e não INSERT
//
// `UPDATE ... WHERE false` não toca em linha alguma nem depende de coluna
// obrigatória, e mesmo assim exige a permissão de escrita: o PostgreSQL confere
// a permissão ANTES de avaliar o predicado. É a sonda mais barata possível.
func VerificarIsolamento(ctx context.Context, tx Executor) error {
	var violadas []string

	for _, tabela := range tabelasProtegidas {
		// A instrução é montada com o nome vindo de uma lista FIXA no código —
		// não há entrada de usuário aqui, e o PostgreSQL não aceita nome de
		// tabela como parâmetro.
		sql := fmt.Sprintf("UPDATE %s SET status = status WHERE false", tabela)
		if tabela != "recorte.tb_importacao" {
			sql = fmt.Sprintf("DELETE FROM %s WHERE false", tabela)
		}

		if _, err := tx.Exec(ctx, sql); err == nil {
			violadas = append(violadas, tabela)
		}
	}

	if len(violadas) > 0 {
		return fmt.Errorf("%w: %s", ErrIsolamentoViolado, strings.Join(violadas, ", "))
	}
	return nil
}

// ResumoDoIsolamento descreve o que foi verificado, para o relatório.
func ResumoDoIsolamento() string {
	return "tentativa de escrita recusada em: " + strings.Join(tabelasProtegidas, ", ")
}
