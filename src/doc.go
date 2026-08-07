// Package src é a raiz da árvore de código do serviço.
//
// A organização segue portas e adaptadores. A regra de dependência é única e
// vale para todo o repositório: as dependências apontam para dentro.
//
//	src/main       ponto de entrada; só monta e executa
//	src/domain     entidades, estados e portas — não depende de nada
//	src/usecase    orquestração — depende apenas de domain
//	src/config     leitura da configuração
//	src/app        montagem do grafo e ciclo de vida
//
// Implementações das portas, que dependem de domain:
//
//	src/httpapi     servidor HTTP e rotas
//	src/middleware  camadas do servidor HTTP
//	src/resposta    corpos de resposta
//	src/postgres    persistência
//	src/pdftext     extração de texto
//	src/searchidx   índice de pesquisa
//
// Infraestrutura transversal, que não é chamada por domain:
//
//	src/observability  registro, métricas e rastreamento
//	src/periodico      execução em intervalo
//	src/shutdown       captura de sinais
//	src/worker         reserva de execução
//
// A árvore é plana de propósito: um nível de pacotes sob src/, sem a divisão
// adapter/platform que existia antes. A regra de dependência continua valendo,
// mas passou a ser convenção — o teste que a verificava foi removido junto com
// os demais testes.
//
// Este pacote não contém código executável: existe para documentar a árvore.
package src
