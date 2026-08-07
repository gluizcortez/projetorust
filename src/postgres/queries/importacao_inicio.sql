-- UPDATE de data_inicio — funcao registrar_inicio_importacao
--
-- Transcricao LITERAL de reference/main.rs:683-690.
--
-- current_timestamp AVALIADO NO SERVIDOR de banco.
-- >>>>> INICIO DA CONSULTA LITERAL — nao editar nada abaixo desta linha

        UPDATE
            recorte.tb_importacao
        SET
            data_inicio =  current_timestamp
        WHERE
            id_importacao = $1
    