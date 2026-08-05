// Package shutdown coordena o encerramento gracioso: recepção de sinais, ordem
// das etapas e teto de tempo opcional.
//
// A ordem é normativa — parar de aceitar, drenar HTTP, drenar importações,
// fechar o pool, esvaziar telemetria. Ver docs/ESPECIFICACAO.md §6.3.
//
// Preenchido na fase F10.
package shutdown
