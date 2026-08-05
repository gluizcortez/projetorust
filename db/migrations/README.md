# Migrações

`0001_baseline.sql` — entregue na fase **F4** — contém apenas **verificações não
destrutivas** do esquema existente (existência de tabela e de coluna, com falha
explicativa). Nenhum `CREATE`, `ALTER` ou `DROP` sobre estruturas existentes.

O esquema real é a decisão aberta **D-13**; a reconstrução inferida está em
`docs/ESPECIFICACAO.md` §2.

Migrações que criam índices (idempotência por hash, fase F11) devem usar
`CREATE INDEX CONCURRENTLY` e ser reversíveis.
