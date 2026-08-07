// Package middleware reúne os decoradores da cadeia HTTP: identificador de
// requisição, registro, recuperação de pânico, limite de corpo, autenticação e
// limitação de taxa.
//
// Cada um é uma func(http.Handler) http.Handler. A ordem de aplicação é
// normativa e está documentada em docs/ESPECIFICACAO.md §1.2.
//
// Preenchido na fase F9.
package middleware
