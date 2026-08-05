-- INSERT em recorte.tb_importacao — funcao registrar_pdf
--
-- Transcricao LITERAL de reference/main.rs:448-462. Nenhum caractere pode ser
-- alterado: espacamento, quebras de linha e ordem das colunas fazem parte da
-- transcricao verificada por TestConsultasSaoIdenticasAoLegado.
--
-- A ordem dos parametros $1..$8 corresponde a ordem dos bind de main.rs:465-472.
-- >>>>> INICIO DA CONSULTA LITERAL — nao editar nada abaixo desta linha

        INSERT INTO
            recorte.tb_importacao (
                id_inclusao,
                id_cadernos,
                data_caderno,
                data_disponibilizacao,
                status,
                nome_original_pdf,
                tipo_caderno,
                hash
            )
        VALUES ($1, $2, $3, $4, $5, $6, $7, $8 )
        RETURNING id_importacao
    