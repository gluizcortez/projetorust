-- Permissões do esquema `recorte` para o usuário da aplicação.
--
-- ------------------------------------------------------------------------
-- QUANDO VOCÊ PRECISA DESTE ARQUIVO
-- ------------------------------------------------------------------------
--
-- Sintoma: `POST /pdf` devolve **422 `Erro ao processar o PDF`** e o log traz
--
--     permission denied for schema recorte (SQLSTATE 42501)
--
-- O texto da resposta engana — é literal do serviço original (`main.rs:242`) e
-- cobre QUALQUER falha ao registrar a importação. O PDF nem chega a ser
-- aberto: a falha é no `INSERT`, antes disso.
--
-- ------------------------------------------------------------------------
-- POR QUE ACONTECE
-- ------------------------------------------------------------------------
--
-- O esquema foi criado por um usuário e a aplicação conecta por outro. É o
-- caso de quem aplicou `db/init/01-esquema.sql` pelo DBeaver conectado como
-- `postgres` (ou outro superusuário) e depois apontou a aplicação para
-- `postgres://recorte:...`. No PostgreSQL, criar um esquema NÃO dá acesso a
-- ele para os demais usuários — nem para o dono do banco.
--
-- Conferir quem é o dono:
--
--     SELECT nspname, pg_get_userbyid(nspowner) AS dono
--     FROM pg_namespace WHERE nspname = 'recorte';
--
-- Com `docker compose up` isso não acontece: os scripts de
-- `/docker-entrypoint-initdb.d` rodam como `POSTGRES_USER`, que é o mesmo
-- `recorte` que a aplicação usa, então o dono já nasce certo.
--
-- ------------------------------------------------------------------------
-- COMO RODAR
-- ------------------------------------------------------------------------
--
-- Precisa ser executado por um SUPERUSUÁRIO ou pelo dono do esquema — o
-- usuário da aplicação não pode conceder privilégios a si mesmo. O passo a
-- passo das três formas (DBeaver, Docker e psql) está no README, na seção
-- "Se /pdf devolver 422".
--
-- Este arquivo é SQL puro: não usa `\set` nem `\if`, então roda igual no
-- psql, no DBeaver, no pgAdmin ou em qualquer cliente.
--
-- Se o usuário da sua aplicação NÃO se chama `recorte`, troque o nome nas
-- cinco linhas abaixo — é o único lugar em que ele aparece.
--
-- Não é preciso reiniciar a aplicação: a próxima requisição já passa.

-- ------------------------------------------------------------------------
-- Os três níveis, todos necessários
-- ------------------------------------------------------------------------
--
-- MEDIDO submetendo o mesmo PDF depois de cada linha: conceder só o primeiro
-- troca o erro por `permission denied for table tb_importacao`, e só os dois
-- primeiros por `permission denied for sequence
-- tb_importacao_id_importacao_seq`. A sequência é a que se esquece, e é o que
-- `nextval` usa para gerar `id_importacao`.

-- 1. Entrar no esquema.
GRANT USAGE ON SCHEMA recorte TO recorte;

-- 2. Ler e escrever nas tabelas.
--
-- DELETE entra porque a manutenção o usa; o serviço em si nunca apaga linha.
GRANT SELECT, INSERT, UPDATE, DELETE
    ON ALL TABLES IN SCHEMA recorte TO recorte;

-- 3. Avançar as sequências das chaves primárias.
GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA recorte TO recorte;

-- ------------------------------------------------------------------------
-- Tabelas e sequências CRIADAS DEPOIS
-- ------------------------------------------------------------------------
--
-- Os `GRANT ... ON ALL` acima valem só para o que existe agora. Sem as duas
-- linhas abaixo, a próxima migração que criar tabela reintroduz exatamente o
-- mesmo 422 — e aí ninguém lembra que já resolveu isso uma vez.
--
-- `FOR ROLE CURRENT_USER` amarra ao usuário que está rodando este arquivo, que
-- é quem vai criar os objetos futuros.

ALTER DEFAULT PRIVILEGES FOR ROLE CURRENT_USER IN SCHEMA recorte
    GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO recorte;

ALTER DEFAULT PRIVILEGES FOR ROLE CURRENT_USER IN SCHEMA recorte
    GRANT USAGE, SELECT ON SEQUENCES TO recorte;

-- ------------------------------------------------------------------------
-- CONFERIR SE FUNCIONOU
-- ------------------------------------------------------------------------
--
-- As três linhas precisam devolver `true`. Rode como qualquer usuário:
--
--     SELECT has_schema_privilege('recorte', 'recorte', 'USAGE')            AS entra_no_esquema,
--            has_table_privilege ('recorte', 'recorte.tb_importacao', 'INSERT') AS insere,
--            has_sequence_privilege('recorte', 'recorte.tb_importacao_id_importacao_seq', 'USAGE') AS usa_sequencia;
--
-- O primeiro argumento é o USUÁRIO, o segundo é o objeto. Os dois se chamam
-- `recorte` aqui — o usuário e o esquema —, o que confunde à primeira vista.

-- ------------------------------------------------------------------------
-- A ALTERNATIVA, se você prefere não gerenciar permissões
-- ------------------------------------------------------------------------
--
-- Transferir a posse do esquema e de tudo que há nele para o usuário da
-- aplicação resolve de vez, e dispensa as linhas de privilégio padrão:
--
--     ALTER SCHEMA recorte OWNER TO recorte;
--     REASSIGN OWNED BY postgres TO recorte;   -- ⚠ afeta o banco INTEIRO
--
-- É mais simples e MENOS seguro: o dono pode apagar as próprias tabelas. Para
-- desenvolvimento local, serve. Para produção, prefira os GRANTs acima — a
-- aplicação não precisa de DDL.
