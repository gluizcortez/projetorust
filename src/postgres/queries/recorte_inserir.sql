-- INSERT em recorte.tb_recorte — funcao salvar_recorte
--
-- Transcricao LITERAL de reference/main.rs:574-585.
--
-- dt_recorte usa current_timestamp AVALIADO NO SERVIDOR de banco. Nao pode ser
-- substituido pela hora do processo.
-- >>>>> INICIO DA CONSULTA LITERAL — nao editar nada abaixo desta linha

        INSERT INTO
            recorte.tb_recorte(
                id_importacao,
                nr_pagina,
                id_perfil,
                expressao_busca,
                dt_recorte
            )
        VALUES ($1, $2, $3, $4, current_timestamp)
        RETURNING id_recorte
    