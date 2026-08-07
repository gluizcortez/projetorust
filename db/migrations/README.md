# Migrações

`0001_baseline.sql` — entregue na fase **F4** — contém apenas **verificações não
destrutivas** do esquema existente (existência de tabela e de coluna, com falha
explicativa). Nenhum `CREATE`, `ALTER` ou `DROP` sobre estruturas existentes.

O esquema real é a decisão aberta **D-13**; a reconstrução inferida está em
`docs/ESPECIFICACAO.md` §2.

Migrações que criam índices (idempotência por hash, fase F11) devem usar
`CREATE INDEX CONCURRENTLY` e ser reversíveis.

## `0002_idempotencia_indice` — fase F11

Cria `ix_importacao_idempotencia` sobre
`(hash, id_cadernos, data_caderno, id_importacao DESC)`, que sustenta a consulta
de `IDEMPOTENCIA_POR_HASH`.

Entregue em dois arquivos, `.up.sql` e `.down.sql`. **Nenhum dos dois pode rodar
dentro de uma transação** — `CREATE INDEX CONCURRENTLY` e `DROP INDEX
CONCURRENTLY` são recusados em bloco transacional. Aplicar em autocommit:

```sh
psql "$DATABASE_URL" --single-transaction=off -f db/migrations/0002_idempotencia_indice.up.sql
```

Ordem de operação obrigatória: **aplicar a migração, confirmar que o índice está
válido, e só então ligar `IDEMPOTENCIA_POR_HASH`.** Ligar a chave antes faz cada
submissão varrer `tb_importacao` inteira.

O índice **não é único** de propósito — um banco de produção já tem duplicatas,
porque o legado nunca deduplicou. Ver o cabeçalho do `.up.sql`.
