-- SELECT do resumo de uma importacao — chave STATUS_ENDPOINT
--
-- NAO tem equivalente no legado: ele nunca le tb_importacao de volta. Por isso
-- este arquivo NAO passa pelo marcador de consulta literal e NAO entra em
-- ConsultasLiterais() — nao ha original com que compara-lo.
--
-- Le apenas as colunas que a resposta expoe. `hash` e `nome_original_pdf`
-- ficam de fora de proposito: ver domain.ResumoDaImportacao.
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
    id_importacao = $1
