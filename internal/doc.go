// Package internal é a raiz da árvore privada do serviço.
//
// A organização segue portas e adaptadores. A regra de dependência é única e
// vale para todo o repositório: as dependências apontam para dentro.
//
//	cmd/          ponto de entrada; só monta e executa
//	internal/domain    entidades, estados e portas — não depende de nada
//	internal/usecase   orquestração — depende apenas de domain
//	internal/adapter   implementações das portas — dependem de domain
//	internal/platform  infraestrutura transversal — não é chamada por domain
//
// Esta regra é verificada por arch_test.go, que falha a integração contínua
// quando domain ou usecase importam infraestrutura.
//
// Este pacote não contém código executável: existe para documentar a árvore e
// hospedar o teste de arquitetura.
package internal
