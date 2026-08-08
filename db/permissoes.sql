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
-- O texto da resposta engana — é literal do serviço original
-- (`main.rs:242`) e cobre QUALQUER falha ao registrar a importação. O PDF nem
-- chega a ser aberto: a falha é no `INSERT`, antes disso.
--
-- ------------------------------------------------------------------------
-- POR QUE ACONTECE
-- ------------------------------------------------------------------------
--
-- O esquema foi criado por um usuário e a aplicação conecta por outro. É o
-- caso típico de quem aplicou `db/init/01-esquema.sql` pelo DBeaver conectado
-- como `postgres` (ou outro superusuário) e depois apontou a aplicação para
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
-- `recorte` que a aplicação usa, então o dono já está certo.
--
-- ------------------------------------------------------------------------
-- COMO USAR
-- ------------------------------------------------------------------------
--
-- Rode como SUPERUSUÁRIO ou como dono do esquema — o usuário da aplicação não
-- pode conceder a si mesmo:
--
--     psql "postgres://postgres@localhost:5432/recorte" -f db/permissoes.sql
--
-- Trocando o nome do usuário, se não for `recorte`:
--
--     psql ... -v app=meu_usuario -f db/permissoes.sql
--
-- Não é preciso reiniciar a aplicação: a próxima requisição já passa.

\if :{?app}
\else
    \set app recorte
\endif

-- ------------------------------------------------------------------------
-- Os três níveis, todos necessários
-- ------------------------------------------------------------------------
--
-- MEDIDO submetendo o mesmo PDF depois de cada linha: conceder só o primeiro
-- troca o erro por `permission denied for table tb_importacao`, e só os dois
-- primeiros por `permission denied for sequence
-- tb_importacao_id_importacao_seq`. A sequência é fácil de esquecer e é o que
-- `nextval` usa para gerar `id_importacao`.

-- 1. Entrar no esquema.
GRANT USAGE ON SCHEMA recorte TO :app;

-- 2. Ler e escrever nas tabelas.
--
-- DELETE entra porque a migração de linha de base e a manutenção o usam; o
-- serviço em si nunca apaga linha.
GRANT SELECT, INSERT, UPDATE, DELETE
    ON ALL TABLES IN SCHEMA recorte TO :app;

-- 3. Avançar as sequências das chaves primárias.
GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA recorte TO :app;

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
    GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO :app;

ALTER DEFAULT PRIVILEGES FOR ROLE CURRENT_USER IN SCHEMA recorte
    GRANT USAGE, SELECT ON SEQUENCES TO :app;

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
