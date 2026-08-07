-- SELECT das importacoes presas — chave VARREDURA_ORFAS
--
-- NAO tem equivalente no legado, que nunca varre nada.
--
-- FOR UPDATE SKIP LOCKED e o que torna a varredura segura com varias
-- instancias: cada uma tranca as linhas que levou e as demais PULAM essas
-- linhas em vez de esperar por elas. Sem SKIP LOCKED, duas instancias
-- serializariam na mesma linha e a segunda a processaria de novo depois do
-- commit da primeira.
--
-- data_inicio IS NOT NULL e deliberado. Uma importacao em status 1..3 com
-- data_inicio nula travou ANTES do UPDATE de data_inicio — e nao ha nela
-- nenhum carimbo de tempo com que medir idade. Varre-la seria arriscar
-- derrubar uma importacao que acabou de comecar.
--
-- Status 0 tambem fica de fora: foi aceita e ainda nao comecou, podendo estar
-- legitimamente na fila do executor. Ver domain.StatusPresos.
SELECT
    id_importacao,
    status,
    data_inicio
FROM
    recorte.tb_importacao
WHERE
    status = ANY($1::int[])
    AND data_inicio IS NOT NULL
    AND data_inicio < $2
ORDER BY
    id_importacao
LIMIT $3
FOR UPDATE SKIP LOCKED
