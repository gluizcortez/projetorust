-- INSERT em recorte.tb_recorte_texto — funcao salvar_recorte
--
-- Transcricao LITERAL de reference/main.rs:587-595.
--
-- A coluna `recorte` recebe o TEXTO INTEGRAL DA PAGINA, normalizado e sem
-- diacriticos — nao um trecho ao redor da ocorrencia. Ver ESPECIFICACAO §5.4.
-- A origem e o literal 'PDF', embutido no proprio SQL.
-- >>>>> INICIO DA CONSULTA LITERAL — nao editar nada abaixo desta linha

        INSERT INTO
            recorte.tb_recorte_texto(
                id_recorte,
                origem,
                recorte
            )
        VALUES ($1, 'PDF', $2)
    