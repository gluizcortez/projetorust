// Package postgres implementa as portas de repositório sobre pgx, além da
// unidade de trabalho que torna atômica a gravação do par tb_recorte e
// tb_recorte_texto.
//
// As consultas SQL são transcritas caractere a caractere do legado e vivem em
// arquivos .sql embutidos — nenhuma é reescrita, reformatada ou "otimizada".
// Em particular, a cláusula ORDER BY de queries/chaves_pesquisa.sql governa a
// deduplicação por perfil (INV-P12) e é intocável.
//
// Este pacote NÃO conhece HTTP nem regra de negócio: traduz porta e SQL.
//
// Preenchido na fase F4.
package postgres
