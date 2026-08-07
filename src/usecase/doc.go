// Package usecase orquestra o comportamento do serviço: a ingestão síncrona do
// PDF e o pipeline assíncrono de indexação e recorte.
//
// Depende exclusivamente das portas declaradas em domain. NÃO importa nenhum
// adaptador, nenhum driver e nenhuma biblioteca de infraestrutura — o que
// permite exercitar o pipeline inteiro com dublês, sem banco, sem PDF e sem
// rede.
//
// Preenchido na fase F8.
package usecase
