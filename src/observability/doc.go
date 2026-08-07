// Package observability provê o registro estruturado, o rastreamento
// distribuído e as métricas do serviço.
//
// Identificadores de requisição e de importação viajam no contexto e são
// copiados para atributos a cada registro, em vez de interpolados no texto da
// mensagem.
//
// Este pacote NÃO decide o que registrar: oferece o instrumento.
//
// Preenchido na fase F2.
package observability
