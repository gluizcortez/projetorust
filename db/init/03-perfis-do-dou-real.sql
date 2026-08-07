-- Perfis casados com `exemplos/dou-secao1-2026-07-08.pdf` — um Diário Oficial
-- da União DE VERDADE (Seção 1, nº 126, 8 de julho de 2026, página 177).
--
-- Enquanto `02-dados-de-exemplo.sql` semeia perfis sintéticos para o PDF
-- sintético, este arquivo existe para responder a uma pergunta diferente: **o
-- serviço funciona sobre um diário real?**
--
-- Cada expressão está no SEU PRÓPRIO PERFIL de propósito. Dentro de um mesmo
-- perfil, a primeira expressão a encontrar uma página consome aquela página e
-- as demais não geram recorte para ela (INV-P12) — o que é o comportamento
-- correto, mas esconderia quais expressões casaram. Uma por perfil torna o
-- resultado legível: uma linha por expressão.
--
-- As oito linhas abaixo NÃO são arbitrárias. São o resultado MEDIDO contra
-- este PDF, e cada uma demonstra uma propriedade do serviço:
--
--   301  BR BPO TECNOLOGIA E SERVICOS         CASA — frase de 4 termos
--   302  CASAMAX COMERCIAL E SERVICOS LTDA    CASA — frase de 5 termos
--   303  LEI GERAL DE PROTECAO DE DADOS       CASA — no PDF está "Lei Geral de
--                                             Proteção de Dados", com acento e
--                                             em caixa mista. Prova a remoção
--                                             de diacríticos e o rebaixamento
--                                             de caixa (INV-P07).
--   304  DEBORAH DA SILVA FELIX               CASA — no PDF, "Dra. Deborah da
--                                             Silva Felix"
--   305  HOMOLOGACOES DE ARQUIVAMENTO         CASA — no PDF, "HOMOLOGAÇÕES DE
--                                             ARQUIVAMENTO", caixa alta e
--                                             acentuada
--
--   306  EMPRESA BRASILEIRA DE CORREIOS E TELEGRAFOS
--                                             NÃO CASA — e o motivo está no
--                                             PDF, não no serviço: a extração
--                                             devolve "TELEG R A FO S", com
--                                             espaços entre as letras, porque
--                                             é assim que o texto foi
--                                             posicionado no documento. Vira
--                                             cinco termos onde a expressão
--                                             espera um.
--   307  LEI GERAL DE PROTEÇÃO DE DADOS       NÃO CASA — a MESMA expressão do
--                                             perfil 303, com acento. Expressão
--                                             acentuada é INERTE, porque o
--                                             texto indexado já perdeu os
--                                             acentos e a expressão não passa
--                                             pela mesma normalização. É
--                                             DEFEITO PRESERVADO do legado —
--                                             INV-P19 —, e o par 303/307 é a
--                                             demonstração viva dele.
--   308  PREFEITURA MUNICIPAL DE SAO PAULO    NÃO CASA — controle negativo: não
--                                             está no documento.
--
-- Reproduzir a medição está no README, em "Validando com um diário real".

INSERT INTO recorte.tb_cliente (id_cliente, status) VALUES (300, 'A');

INSERT INTO recorte.tb_perfil (id_perfil, id_cliente)
SELECT g, 300 FROM generate_series(301, 308) g;

INSERT INTO recorte.tb_perfil_caderno (id_perfil, id_cadernos, status)
SELECT g, 1, 'S' FROM generate_series(301, 308) g;

INSERT INTO recorte.tb_perfil_variacao (id_perfil, expressao_nm) VALUES
    (301, 'BR BPO TECNOLOGIA E SERVICOS'),
    (302, 'CASAMAX COMERCIAL E SERVICOS LTDA'),
    (303, 'LEI GERAL DE PROTECAO DE DADOS'),
    (304, 'DEBORAH DA SILVA FELIX'),
    (305, 'HOMOLOGACOES DE ARQUIVAMENTO'),
    (306, 'EMPRESA BRASILEIRA DE CORREIOS E TELEGRAFOS'),
    (307, 'LEI GERAL DE PROTEÇÃO DE DADOS'),
    (308, 'PREFEITURA MUNICIPAL DE SAO PAULO');
