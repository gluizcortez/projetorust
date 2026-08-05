-- Migração de linha de base — fase F4
--
-- Este arquivo NÃO cria, altera nem remove nada. Ele apenas VERIFICA que o
-- esquema esperado existe, falhando com uma mensagem que nomeia exatamente o
-- que está faltando.
--
-- A razão é o estado de docs/DECISOES-ABERTAS.md, D-13: o esquema real nunca
-- foi disponibilizado, e todo o capítulo 2 da ESPECIFICACAO está marcado como
-- INFERIDO. Emitir CREATE ou ALTER a partir de uma reconstrução inferida
-- arriscaria alterar um banco de produção com base num palpite.
--
-- Quando D-13 for respondida, esta migração passa a ser a linha de base real e
-- as evoluções (índice para idempotência, fase F11) vêm depois dela.

DO $$
DECLARE
    faltando text[] := ARRAY[]::text[];
    esperado record;
BEGIN
    -- Tabelas exercitadas pelo serviço. Ver ESPECIFICACAO §2.
    FOR esperado IN
        SELECT * FROM (VALUES
            ('recorte', 'tb_importacao'),
            ('recorte', 'tb_recorte'),
            ('recorte', 'tb_recorte_texto'),
            ('recorte', 'tb_perfil'),
            ('recorte', 'tb_perfil_variacao'),
            ('recorte', 'tb_perfil_caderno'),
            ('recorte', 'tb_cliente')
        ) AS t(esquema, tabela)
    LOOP
        IF NOT EXISTS (
            SELECT 1 FROM information_schema.tables
            WHERE table_schema = esperado.esquema AND table_name = esperado.tabela
        ) THEN
            faltando := faltando || format('tabela %I.%I', esperado.esquema, esperado.tabela);
        END IF;
    END LOOP;

    -- Colunas efetivamente lidas ou escritas pelas sete consultas.
    FOR esperado IN
        SELECT * FROM (VALUES
            ('tb_importacao', 'id_importacao'),
            ('tb_importacao', 'id_inclusao'),
            ('tb_importacao', 'id_cadernos'),
            ('tb_importacao', 'data_caderno'),
            ('tb_importacao', 'data_disponibilizacao'),
            ('tb_importacao', 'status'),
            ('tb_importacao', 'nome_original_pdf'),
            ('tb_importacao', 'tipo_caderno'),
            ('tb_importacao', 'hash'),
            ('tb_importacao', 'data_inicio'),
            ('tb_importacao', 'data_fim'),
            ('tb_importacao', 'total_recortes'),
            ('tb_recorte', 'id_recorte'),
            ('tb_recorte', 'id_importacao'),
            ('tb_recorte', 'nr_pagina'),
            ('tb_recorte', 'id_perfil'),
            ('tb_recorte', 'expressao_busca'),
            ('tb_recorte', 'dt_recorte'),
            ('tb_recorte_texto', 'id_recorte'),
            ('tb_recorte_texto', 'origem'),
            ('tb_recorte_texto', 'recorte'),
            ('tb_perfil', 'id_perfil'),
            ('tb_perfil', 'id_cliente'),
            ('tb_perfil_variacao', 'id_perfil'),
            ('tb_perfil_variacao', 'expressao_nm'),
            ('tb_perfil_caderno', 'id_perfil'),
            ('tb_perfil_caderno', 'id_cadernos'),
            ('tb_perfil_caderno', 'status'),
            ('tb_cliente', 'id_cliente'),
            ('tb_cliente', 'status')
        ) AS t(tabela, coluna)
    LOOP
        IF EXISTS (
            SELECT 1 FROM information_schema.tables
            WHERE table_schema = 'recorte' AND table_name = esperado.tabela
        ) AND NOT EXISTS (
            SELECT 1 FROM information_schema.columns
            WHERE table_schema = 'recorte'
              AND table_name = esperado.tabela
              AND column_name = esperado.coluna
        ) THEN
            faltando := faltando || format('coluna recorte.%I.%I', esperado.tabela, esperado.coluna);
        END IF;
    END LOOP;

    IF array_length(faltando, 1) > 0 THEN
        RAISE EXCEPTION
            'esquema incompatível com o esperado pelo serviço de recorte: % item(ns) ausente(s): %',
            array_length(faltando, 1), array_to_string(faltando, ', ')
            USING HINT = 'ver docs/ESPECIFICACAO.md §2 e docs/DECISOES-ABERTAS.md D-13';
    END IF;
END $$;
