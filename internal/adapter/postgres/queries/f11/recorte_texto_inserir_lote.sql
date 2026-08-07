-- INSERT em lote em recorte.tb_recorte_texto — chave GRAVACAO_EM_LOTE
--
-- Mesmas colunas e mesmo literal de origem ('PDF') da consulta literal
-- queries/recorte_texto_inserir.sql. O pareamento entre identificador e texto e
-- posicional: os dois vetores tem o mesmo comprimento e a mesma ordem, o que a
-- funcao chamadora garante.
INSERT INTO
    recorte.tb_recorte_texto(
        id_recorte,
        origem,
        recorte
    )
SELECT
    entrada.id_recorte,
    'PDF',
    entrada.recorte
FROM
    unnest($1::bigint[], $2::text[]) AS entrada(id_recorte, recorte)
