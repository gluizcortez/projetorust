-- UPDATE do status — funcao atualizar_status_importacao
--
-- Transcricao LITERAL de reference/main.rs:664-671.
-- >>>>> INICIO DA CONSULTA LITERAL — nao editar nada abaixo desta linha

        UPDATE
            recorte.tb_importacao
        SET
            status = $1
        WHERE
            id_importacao = $2
    