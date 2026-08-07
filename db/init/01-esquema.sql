-- Esquema para EXECUÇÃO LOCAL — não é a migração de produção.
--
-- ATENÇÃO: este esquema é INFERIDO das sete consultas do serviço, não obtido do
-- banco real. A estrutura verdadeira é a decisão aberta D-13, e enquanto ela não
-- for respondida `db/migrations/0001_baseline.sql` se limita a VERIFICAR o que
-- existe, sem criar nada — justamente para não alterar um banco de produção com
-- base num palpite.
--
-- Aqui o palpite é aceitável e necessário: o alvo é um contêiner descartável que
-- o `docker-compose.yml` cria do zero. Rodado UMA vez, na criação do volume.
--
-- NUNCA aplique este arquivo num banco de produção.

-- Esquema de TESTE, reconstruído a partir das consultas do legado.
--
-- ATENÇÃO: este arquivo NÃO é a fonte da verdade. O esquema real é a decisão
-- aberta D-13 e nunca foi disponibilizado; todo o capítulo 2 da ESPECIFICACAO
-- está marcado como INFERIDO. Este arquivo existe só para dar aos testes de
-- integração um banco em que as consultas literais possam ser exercitadas.
--
-- Quando D-13 for respondida, este arquivo deve ser substituído por um
-- pg_dump --schema-only do banco real, e qualquer divergência que aparecer
-- vira achado.

CREATE SCHEMA IF NOT EXISTS recorte;

CREATE TABLE recorte.tb_cliente (
    id_cliente bigint PRIMARY KEY,
    status     text NOT NULL
);

CREATE TABLE recorte.tb_perfil (
    id_perfil  bigint PRIMARY KEY,
    id_cliente bigint NOT NULL REFERENCES recorte.tb_cliente(id_cliente)
);

CREATE TABLE recorte.tb_perfil_variacao (
    id_perfil    bigint NOT NULL REFERENCES recorte.tb_perfil(id_perfil),
    expressao_nm text
);

CREATE TABLE recorte.tb_perfil_caderno (
    id_perfil   bigint NOT NULL REFERENCES recorte.tb_perfil(id_perfil),
    id_cadernos integer NOT NULL,
    status      text NOT NULL
);

CREATE TABLE recorte.tb_importacao (
    id_importacao         bigserial PRIMARY KEY,
    id_inclusao           bigint,
    id_cadernos           integer,
    data_caderno          date,
    data_disponibilizacao date,
    status                integer,
    nome_original_pdf     text,
    tipo_caderno          text,
    hash                  text,
    data_inicio           timestamp,
    data_fim              timestamp,
    total_recortes        integer
);

CREATE TABLE recorte.tb_recorte (
    id_recorte      bigserial PRIMARY KEY,
    id_importacao   bigint NOT NULL REFERENCES recorte.tb_importacao(id_importacao),
    nr_pagina       bigint NOT NULL,
    id_perfil       bigint NOT NULL,
    expressao_busca text,
    dt_recorte      timestamp
);

CREATE TABLE recorte.tb_recorte_texto (
    id_recorte bigint NOT NULL REFERENCES recorte.tb_recorte(id_recorte),
    origem     text,
    recorte    text
);
