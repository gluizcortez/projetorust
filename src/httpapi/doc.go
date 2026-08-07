// Package httpapi expõe o serviço por HTTP: roteamento, manipuladores e a
// tradução entre erros de domínio e respostas.
//
// É a única camada que conhece códigos de status e corpos de resposta. NÃO
// contém regra de negócio: validação e orquestração vivem em domain e usecase.
//
// O contrato de resposta em texto puro é normativo — ver docs/ESPECIFICACAO.md
// §1.4.3. Preenchido na fase F9.
package httpapi
