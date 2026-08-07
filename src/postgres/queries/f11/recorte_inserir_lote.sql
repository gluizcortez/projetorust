-- INSERT em lote em recorte.tb_recorte — chave GRAVACAO_EM_LOTE
--
-- Substitui N execucoes de queries/recorte_inserir.sql por UMA. O resultado no
-- banco tem de ser IDENTICO ao modo linha a linha: mesmas linhas, mesma
-- associacao, mesma contagem.
--
-- As colunas e o literal current_timestamp sao os mesmos da consulta literal.
-- current_timestamp e o instante de INICIO DA TRANSACAO, e as duas modalidades
-- rodam dentro de uma transacao — logo dt_recorte tambem coincide.
--
-- WITH ORDINALITY + ORDER BY e o que preserva a ORDEM: as linhas sao inseridas
-- na ordem da entrada e o RETURNING as devolve na ordem de insercao. A coluna
-- nr_pagina volta junto para que o chamador CONFIRA o pareamento em vez de
-- confiar nele — ver RepositorioRecorte.salvarEmLote.
INSERT INTO
    recorte.tb_recorte(
        id_importacao,
        nr_pagina,
        id_perfil,
        expressao_busca,
        dt_recorte
    )
SELECT
    $1,
    entrada.nr_pagina,
    $3,
    $4,
    current_timestamp
FROM
    unnest($2::bigint[]) WITH ORDINALITY AS entrada(nr_pagina, ordem)
ORDER BY
    entrada.ordem
RETURNING id_recorte, nr_pagina
