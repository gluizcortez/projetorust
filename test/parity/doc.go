// Package parity compara a implementação Go com o corpus dourado capturado do
// serviço legado, em cinco camadas: texto bruto, texto normalizado, termos do
// índice, conjunto de recortes e estado final do banco.
//
// Os oráculos estão em test/testdata/expected, gerados por
// tools/capturar-corpus. Ver docs/CONTEXT.md, fase F0.
//
// Preenchido progressivamente nas fases F5 a F12.
package parity
