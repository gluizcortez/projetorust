// Package worker executa as importações em segundo plano, com teto de
// concorrência opcional e drenagem por notificação.
//
// Substitui o par Arc<AtomicUsize> mais espera por sondagem do legado. Toda
// tarefa é registrada antes de ser disparada, e um pânico dentro de uma tarefa
// nunca derruba o processo.
//
// Preenchido nas fases F8 e F11.
package worker
