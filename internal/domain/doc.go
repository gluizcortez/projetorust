// Package domain contém o núcleo do serviço: entidades, a máquina de estados
// da importação, o acumulador de críticas de validação, os erros sentinela e
// as portas que os adaptadores implementam.
//
// Este pacote NÃO conhece HTTP, banco de dados, PDF, índice de busca,
// registro estruturado ou qualquer biblioteca de infraestrutura. Ele importa
// apenas a biblioteca padrão, e isso é verificado por internal/arch_test.go.
//
// Preenchido na fase F3.
package domain
