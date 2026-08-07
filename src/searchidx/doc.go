// Package searchidx constrói o índice posicional em memória e executa a busca
// de frase que substitui o Tantivy.
//
// A busca é de FRASE sobre posições de termos, com distância zero — nunca de
// subcadeia (INV-P05). O analisador léxico replica a cadeia do legado, com
// descarte de termos de 40 bytes ou mais, medido em bytes (INV-P03, INV-P04).
//
// Este pacote NÃO deduplica e NÃO ordena por perfil: isso é do caso de uso.
//
// Preenchido nas fases F6 e F7.
package searchidx
