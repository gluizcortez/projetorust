-- SELECT de importacao equivalente — chave IDEMPOTENCIA_POR_HASH
--
-- NAO tem equivalente no legado. Procura o MESMO documento (hash) para o MESMO
-- caderno (id_cadernos) na MESMA data de caderno (data_caderno). Ver
-- domain.ChaveDeIdempotencia para por que os tres campos, e nao so o hash.
--
-- ORDER BY id_importacao DESC LIMIT 1 devolve a MAIS RECENTE. Duplicatas
-- existem legitimamente no historico: o legado nunca deduplicou, entao um banco
-- em producao ja tem varias linhas com o mesmo hash. Pegar a mais recente e o
-- unico criterio estavel.
--
-- O indice que sustenta esta consulta esta em db/migrations/0002.
SELECT
    id_importacao,
    status,
    data_caderno,
    data_disponibilizacao,
    data_inicio,
    data_fim,
    total_recortes
FROM
    recorte.tb_importacao
WHERE
    hash = $1
    AND id_cadernos = $2
    AND data_caderno = $3
ORDER BY
    id_importacao DESC
LIMIT 1
