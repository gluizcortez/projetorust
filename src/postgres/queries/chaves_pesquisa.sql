-- SELECT das chaves de pesquisa — funcao obter_chaves_pesquisa
--
-- Transcricao LITERAL de reference/main.rs:545-563.
--
-- ATENCAO — A CLAUSULA ORDER BY E INTOCAVEL.
--
-- `ORDER BY tp.id_perfil, tpv.expressao_nm` governa a deduplicacao por perfil
-- descrita em docs/INVARIANTES.md, INV-P12: dentro de um mesmo perfil, a
-- PRIMEIRA expressao a encontrar uma pagina consome aquela pagina, e as demais
-- nao geram recorte para ela. Portanto esta ordenacao determina QUAL EXPRESSAO
-- e gravada em tb_recorte.expressao_busca para uma pagina disputada.
--
-- Alterar, reordenar ou "otimizar" esta clausula muda o conteudo do banco.
-- O resultado tambem depende do COLLATE do servidor — ver DECISOES-ABERTAS.md,
-- D-02.
-- >>>>> INICIO DA CONSULTA LITERAL — nao editar nada abaixo desta linha

        SELECT DISTINCT
            tp.id_perfil,
            tpv.expressao_nm
        FROM
            recorte.tb_perfil_variacao tpv 
            INNER JOIN recorte.tb_perfil tp ON tpv.id_perfil = tp.id_perfil
            INNER JOIN recorte.tb_perfil_caderno ec ON tp.id_perfil = ec.id_perfil
            INNER JOIN recorte.tb_cliente tc ON tc.id_cliente =  tp.id_cliente
            INNER JOIN recorte.tb_importacao ti ON ti.id_cadernos = ec.id_cadernos
        WHERE
            tpv.expressao_nm IS NOT NULL
            AND ec.status = 'S'
            AND tc.status = 'A'
            AND ti.id_importacao = $1
        ORDER BY
            tp.id_perfil,
            tpv.expressao_nm 
    