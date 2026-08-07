# Consultas SQL

As cinco consultas do legado, transcritas **caractere a caractere** de
`reference/main.rs` e embutidas com `go:embed`. Entregues na fase **F4**.

Nenhuma pode ser reescrita, reformatada ou "otimizada". Um teste em F4 carrega
as consultas daqui e do `main.rs` original e as compara.

A cláusula `ORDER BY tp.id_perfil, tpv.expressao_nm` de `chaves_pesquisa.sql`
governa a deduplicação por perfil (INV-P12) e é intocável.
