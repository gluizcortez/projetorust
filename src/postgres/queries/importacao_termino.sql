-- UPDATE de data_fim e total_recortes — funcao registrar_termino_importacao
--
-- Transcricao LITERAL de reference/main.rs:701-709.
--
-- current_timestamp AVALIADO NO SERVIDOR. O parametro $1 e int32: o legado
-- converte com i32::try_from ANTES de executar, e a falha impede a gravacao
-- das duas colunas (INV-P18).
-- >>>>> INICIO DA CONSULTA LITERAL — nao editar nada abaixo desta linha

        UPDATE
            recorte.tb_importacao
        SET
            data_fim =  current_timestamp,
            total_recortes = $1
        WHERE
            id_importacao = $2
    