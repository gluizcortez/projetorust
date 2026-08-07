// Package pdftext extrai o texto de um PDF via MuPDF e aplica a normalização
// do legado: junção de hífens de fim de linha e remoção de diacríticos, nessa
// ordem.
//
// A ordem das operações e a estrutura da varredura (blocos, linhas,
// caracteres) são normativas — ver docs/INVARIANTES.md, INV-P02, INV-P07,
// INV-P08, INV-P09, INV-P10 e INV-P11.
//
// Este pacote NÃO indexa e NÃO busca. Preenchido nas fases F5 e F6.
package pdftext

// As tabelas de exceção Unicode são geradas comparando o Go com o próprio
// Rust, runa a runa. Ver tools/gerar-tabela-diacriticos.
//
//go:generate sh -c "cd ../.. && cargo build --release --quiet --manifest-path tools/gerar-tabela-diacriticos/dump/Cargo.toml && tools/gerar-tabela-diacriticos/dump/target/release/dump-unicode > /tmp/unicode.tsv && go run ./tools/gerar-tabela-diacriticos /tmp/unicode.tsv"
