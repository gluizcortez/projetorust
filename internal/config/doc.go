// Package config carrega e valida a configuração do serviço a partir do
// ambiente, com .env opcional.
//
// Concentra os padrões que hoje estão espalhados pelo main.rs legado e as
// chaves de recurso das evoluções da fase F11 — todas com padrão que reproduz
// exatamente o comportamento atual.
//
// Este pacote NÃO decide comportamento: apenas expõe valores. Quem os
// interpreta é quem os recebe.
//
// Preenchido na fase F2.
package config
