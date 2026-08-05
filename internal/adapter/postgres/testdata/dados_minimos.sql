-- Conjunto mínimo de perfis para exercitar a consulta de chaves de pesquisa.
--
-- Desenhado para cobrir os filtros da consulta (cliente ativo, caderno com
-- status 'S', expressão não nula) e a ordenação que governa INV-P12.

INSERT INTO recorte.tb_cliente (id_cliente, status) VALUES
    (100, 'A'),   -- ativo
    (200, 'I');   -- inativo: precisa ser filtrado

INSERT INTO recorte.tb_perfil (id_perfil, id_cliente) VALUES
    (7, 100), (8, 100), (9, 100), (99, 200);

INSERT INTO recorte.tb_perfil_caderno (id_perfil, id_cadernos, status) VALUES
    (7,  1, 'S'),
    (8,  1, 'S'),
    (9,  1, 'N'),   -- caderno desabilitado: precisa ser filtrado
    (99, 1, 'S');   -- perfil de cliente inativo: precisa ser filtrado

INSERT INTO recorte.tb_perfil_variacao (id_perfil, expressao_nm) VALUES
    (7,  'BETA CONSTRUCOES'),
    (7,  'ALFA CONSTRUCOES'),
    (7,  NULL),               -- expressão nula: precisa ser filtrada
    (8,  'GAMA SERVICOS'),
    (9,  'DELTA LTDA'),
    (99, 'OMEGA SA');
