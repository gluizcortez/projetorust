# Decisões abertas

> **Fase F0.** Perguntas que o código-fonte não responde e cuja resposta muda a
> implementação. Cada item traz: por que importa, como verificar, o **padrão
> provisório** adotado se ninguém responder, e a fase que ela bloqueia.
>
> **Política de resolução** (`roadmap`, risco R5): item sem resposta em **5 dias
> úteis** adota o padrão provisório e é registrado como risco aceito em
> `CONTEXT.md`. Itens marcados **BLOQUEANTE** não têm padrão provisório viável e
> impedem o fechamento da fase indicada.

## Painel

| ID | Pergunta | Bloqueia | Estado | Responsável |
|---|---|---|---|---|
| D-01 | Existe perfil com `id_perfil = 0`? | F8 | aberta | operação |
| D-02 | Qual o `COLLATE` do banco? | F4, F8 | aberta | operação |
| D-03 | Quais as versões exatas de `tantivy`, `mupdf` e `diacritics`? | **F6, F7** | parcial · limite **medido** | quem mantém o Rust |
| D-04 | Qual o maior PDF e o maior número de recortes já processados? | F11, F12 | aberta | operação |
| D-05 | Existem expressões com `"` cadastradas? | F7, F8 | aberta · **efeito medido** | operação |
| D-06 | O que fazer com padrão de `&` que não compila? | F8 | aberta · **ampliada em F7** | arquitetura |
| D-07 | Reproduzir ou remover o ramo morto de `main.rs:226–230`? | F9 | aberta | arquitetura |
| D-08 | Qual a resposta para rota inexistente e método não permitido? | F9 | aberta | captura empírica |
| D-09 | Manter a distinção entre 401 "ausente" e 401 "inválida"? | F9 | **decidida** | arquitetura |
| D-10 | Qual o `Content-Type` exato de cada resposta? | F9 | aberta | captura empírica |
| D-11 | Corpus de PDFs reais e *dump* de perfis | **F5, F6, F7, F12** | aberta · **BLOQUEANTE** | operação |
| D-12 | O `search_path` é assumido em algum lugar? | F4 | aberta | operação |
| D-13 | Qual o esquema real das sete tabelas? | F4 | aberta | operação |
| D-14 | A infraestrutura permite espelhar tráfego? | F12 | aberta | infraestrutura |
| D-15 | Onde estão o `Cargo.toml` e o `Cargo.lock` originais? | **F0, F6** | aberta · **BLOQUEANTE** | quem mantém o Rust |
| D-16 | Qual o limite prático de resposta para `MAX_UPLOAD_BYTES`? | F11 | aberta | produto |
| D-17 | Quantos perfis têm expressões acentuadas (hoje inertes)? | — | aberta · **escalar a produto** | produto |
| D-18 | PDFs truncados chegam em produção? | — | aberta · **escalar a produto** | operação |

---

## D-01 · Existe perfil com `id_perfil = 0`?

**Por que importa.** `main.rs:281` inicializa a variável de controle da
deduplicação com `0`. Existindo um perfil com esse identificador, a primeira
chave processada não dispara o reinício do conjunto de páginas. Ver
`INVARIANTES.md`, INV-P13.

**Como verificar.**
```sql
SELECT count(*) FROM recorte.tb_perfil WHERE id_perfil = 0;
SELECT min(id_perfil), max(id_perfil) FROM recorte.tb_perfil;
```

**Análise prévia.** O efeito prático é nulo, porque o conjunto de páginas já
nasce vazio e a variável só é usada para detectar mudança. A demonstração está
em INV-P13.

**Padrão provisório.** Usar sentinela explícito (ponteiro nulo ou booleano de
primeira iteração), equivalente nos dois cenários, com comentário citando
INV-P13.

---

## D-02 · Qual o `COLLATE` do banco?

**Por que importa.** `ORDER BY tpv.expressao_nm` (`main.rs:562`) determina qual
expressão consome uma página disputada dentro de um perfil (INV-P12) — e
portanto **qual valor é gravado em `tb_recorte.expressao_busca`**. Configurações
regionais diferentes ordenam de forma diferente expressões que divergem por
acento, caixa ou pontuação.

Exemplo: `ACME` e `ação` ordenam de forma distinta em `C` e em `pt_BR.UTF-8`.

**Como verificar.**
```sql
SELECT datcollate, datctype FROM pg_database WHERE datname = current_database();
SELECT collname FROM pg_collation WHERE oid = (
    SELECT collation FROM information_schema.columns
    WHERE table_schema='recorte' AND table_name='tb_perfil_variacao'
      AND column_name='expressao_nm'
);
```

**Padrão provisório.** Fixar o ambiente de teste no mesmo `COLLATE` de produção
e **não** replicar a ordenação em Go: a ordem vem do banco, e o serviço apenas
a respeita. Registrar o valor observado no `CONTEXT.md`.

---

## D-03 · Quais as versões exatas de `tantivy`, `mupdf` e `diacritics`? — **PARCIAL**

**Por que importa.** Três invariantes dependem de valores que só a versão exata
determina:

| Invariante | Dependência |
|---|---|
| INV-P03 | limite de comprimento de termo do analisador `default` |
| INV-P04 | se o limite é medido em bytes ou runas nessa versão |
| INV-P07 | conteúdo da tabela de mapeamento do *crate* `diacritics` |
| INV-P11 | comportamento da extração de texto da versão do MuPDF |

Adivinhar esses valores produz um serviço que parece correto e diverge em
casos raros — exatamente o modo de falha que este roadmap existe para evitar.

**Como verificar.** Obter o `Cargo.lock` do projeto (ver D-15) e ler as entradas
`tantivy`, `mupdf`, `mupdf-sys`, `diacritics`, `regex`. Em seguida, ler o código
das versões correspondentes para extrair o limite e a tabela.

**Estado parcial — medições já feitas.** As versões resolvidas no ambiente de
desenvolvimento durante a fase F0 foram:

| *Crate* | Versão resolvida | Base da resolução |
|---|---|---|
| `tantivy` | 0.22.1 | API observada em `main.rs` (`TantivyDocument`, `ReloadPolicy::OnCommitWithDelay`) |
| `mupdf` | 0.4.4 | restrição `"0.4"` |
| `diacritics` | 0.2.2 | restrição `"0.2"` |

Com essas versões, o corpus sintético **mediu** o valor de INV-P03/INV-P04:

> **Limite de comprimento de termo = 40 bytes; descarta comprimento ≥ 40.**
> Medido em **bytes**, não em runas — provado pelo documento
> `12b-inv-p04-bytes-contra-runas.pdf`, em que `α`×19 (38 bytes / 19 runas)
> é indexado e `α`×20 (40 bytes / 20 runas) não é.

**O que ainda falta.** Confirmar que a versão em produção é a mesma. Se for, a
medição acima fecha INV-P03 e INV-P04 e **desbloqueia F6**. Se não for, basta
alinhar o `Cargo.lock`, reexecutar `capturar-corpus` e reler a medição — o
método é reprodutível e leva minutos.

**Padrão provisório.** Adotar 40 bytes com a regra `≥ 40 descarta`, marcado no
código como constante nomeada com referência a esta decisão. A fase F6 pode
prosseguir com esse valor **desde que** a constante fique isolada e a suíte de
paridade seja reexecutada quando D-15 for resolvida.

---

## D-04 · Qual o maior PDF e o maior número de recortes já processados?

**Por que importa.** Dimensiona três coisas: o valor recomendado de
`MAX_IMPORTACOES_CONCORRENTES` (memória por importação × concorrência), o valor
de `MAX_UPLOAD_BYTES`, e se o cenário de estouro de `total_recortes` (INV-P18)
é teórico ou alcançável.

**Como verificar.**
```sql
SELECT max(total_recortes), avg(total_recortes),
       percentile_cont(0.99) WITHIN GROUP (ORDER BY total_recortes)
FROM recorte.tb_importacao WHERE total_recortes IS NOT NULL;

SELECT count(*) FROM recorte.tb_recorte GROUP BY id_importacao ORDER BY 1 DESC LIMIT 10;
```
Para o tamanho dos PDFs, consultar o armazenamento de arquivos ou os registros
do servidor HTTP.

**Padrão provisório.** Tratar INV-P18 como caminho de código coberto por teste
mas improvável em produção. Deixar `MAX_UPLOAD_BYTES` e
`MAX_IMPORTACOES_CONCORRENTES` em `0` (comportamento atual) até a medição de
F12 produzir números reais.

---

## D-05 · Existem expressões com `"` cadastradas?

**Por que importa.** Uma expressão com aspas duplas aborta a importação inteira
(INV-P17). Se o cenário ocorre em produção, há importações falhando hoje por
essa razão, e a reescrita precisa reproduzir a falha — ou a decisão de corrigir
precisa ser explícita e atrás de chave.

**Como verificar.**
```sql
SELECT id_perfil, expressao_nm
FROM recorte.tb_perfil_variacao
WHERE expressao_nm LIKE '%"%' OR expressao_nm LIKE '%\%';

SELECT count(*) FROM recorte.tb_importacao WHERE status = -1;
```
Cruzar as importações em −1 com os cadernos cujos perfis tenham expressões
suspeitas.

**Padrão provisório.** Reproduzir a falha, conforme a regra do oráculo. Se a
consulta revelar ocorrências em produção, abrir item de produto — não corrigir
dentro da migração.

### O que a fase F7 mediu

A falha do legado é do **analisador de consulta** do Tantivy: `main.rs:375` monta
a consulta por interpolação sem escape, `format!(r#""{key}""#)`, e a aspa
desbalanceia as aspas externas. A captura registrou **78 combinações** de
desfecho `erro` no corpus — todas de expressões com `"` ou com `\` final.

**A busca em Go não tem analisador de consulta.** A expressão é tokenizada pelo
mesmo caminho do texto, e a aspa é apenas mais um separador: não existe erro a
devolver. Das 78, **75 são bem definidas em Go** e **3 falham por outro motivo**
— `ACME & FILHOS \` também tem `&` e barra invertida final, e o filtro do
operador a recusa.

**Consequência para a decisão.** Reproduzir INV-P17 exige um teste explícito de
aspas **antes** da busca, no caso de uso — não no índice. É trabalho de F8, e
depende desta pergunta: se nenhuma expressão de produção tem `"`, o teste é
código morto e não deve ser escrito. `TestINVP17AspasNaoAbortamABusca` fixa o
comportamento atual para que a escolha seja consciente.

---

## D-06 · O que fazer com padrão de `&` que não compila?

**Por que importa.** `main.rs:394` usa `.unwrap()` na compilação da expressão
regular. Um padrão inválido causa **pânico dentro da tarefa de fundo**. O
`tokio` captura o pânico na `JoinHandle`, que é descartada — o processo
sobrevive, mas a importação fica **presa no último status gravado** (`3`), sem
nunca ir a `-1` nem a `5`.

Isso difere do tratamento de erro comum, que grava `-1`. A reescrita precisa
escolher entre reproduzir o estado preso ou normalizar para `-1`.

**Como verificar.** Buscar importações paradas em `status = 3`:
```sql
SELECT id_importacao, data_inicio FROM recorte.tb_importacao
WHERE status IN (1,2,3) AND data_inicio < now() - interval '1 day';
```
E verificar se alguma expressão produz padrão inválido após a substituição de
`&` e o pré-processamento do modo *extended*.

**Análise.** Após `key.replace('&', "\s*&\s*")`, os padrões inválidos possíveis
vêm de metacaracteres desbalanceados na própria expressão: `(`, `[`, `*` inicial,
`\` final. São plausíveis em razões sociais.

**Padrão provisório.** Reproduzir o **efeito observável**: a importação fica
presa no status `3`. Implementar como erro tipado que interrompe a tarefa **sem**
gravar `-1`, com registro em log de nível erro. Documentar como
`DEFEITO PRESERVADO` e propor a normalização para `-1` como evolução em F11,
atrás de chave.

### Ampliação na fase F7 — o que ficou medido

A fase F7 confirmou a análise acima e acrescentou três coisas.

**1. O pânico é real e foi capturado.** `tools/capturar-corpus` agora envolve
`recortar` em `catch_unwind` e registra três desfechos por expressão — `ok`,
`erro` e `panico` — em `test/testdata/expected/*.busca.json`. Sobre o corpus
sintético, **9 pânicos** em 1.378 combinações, todos com expressões contendo `&`
e sintaxe inválida.

**2. O pânico é condicional ao acerto** (INV-P23). A expressão só é compilada
dentro do laço sobre os resultados, então a mesma expressão inválida é inofensiva
em 23 dos 26 documentos. Qualquer normalização precisa preservar isso, ou
transformará importações hoje bem-sucedidas em falhas.

**3. O conjunto de padrões recusados é MAIOR em Go do que em Rust.** O `.unwrap()`
não é a única fonte de recusa: o RE2 é um analisador sintático diferente do
*crate* `regex`. A fase F7 mediu 250.000 expressões adversariais e classificou:

| Classe | Motivo | Comportamento em Go |
|---|---|---|
| `(?-x)`, `(?x)` | a flag `x` não existe no RE2 | recusa na compilação |
| `\w`, `\W`, `\b`, `\B` | o `\w` do Rust é `[\p{Alphabetic}\p{M}\p{Nd}\p{Pc}\p{Join_Control}]`; `Alphabetic` e `Join_Control` são propriedades derivadas que o `\p{...}` do RE2 não expõe | **recusa deliberada** |
| `[a[bc]]` | classe aninhada: o Rust lê união de conjuntos, o RE2 lê `[` literal — os dois compilam e produzem autômatos **diferentes** | **recusa deliberada** |
| `\s*{2}` e afins | o RE2 recusa repetição aninhada que o Rust aceita | recusa na compilação |

As duas linhas marcadas **recusa deliberada** são escolha de engenharia desta
fase: traduzir por aproximação criaria divergência **silenciosa** — recorte
errado gravado sem que ninguém perceba —, e recusar troca isso por falha
**alta**, no mesmo regime em que o RE2 já recusa `(?-x)`.

**Conserto exato, se alguma expressão real precisar.** `\w` e `\W` são
exprimíveis em RE2 por uma classe explícita de intervalos, gerada perguntando ao
próprio motor do Rust — é a mesma técnica já usada três vezes no projeto
(`palavra_table.go`, `alfanumerico_table.go`, `diacriticos_table.go`). `\b` e
`\B` **não** têm conserto: usam o `\w` interno do motor, que no RE2 é ASCII e
não é configurável. Classe aninhada exigiria um analisador de classes com
álgebra de conjuntos para achatar os intervalos.

**O que decidir, e o custo de não decidir.** A pergunta continua sendo a de
sempre — reproduzir o estado preso em `3` ou normalizar para `-1`. O que mudou é
que ela agora **bloqueia F8**, não F7: a busca devolve `ErrExpressaoInvalida` e
quem escolhe o status é a máquina de estados. Enquanto não houver resposta, F8
adota o padrão provisório acima.

**Como reduzir o risco a zero sem decidir.** As quatro classes só são alcançáveis
por expressões que contenham sintaxe de expressão regular. Uma consulta resolve:

```sql
SELECT DISTINCT expressao_nm
FROM recorte.tb_perfil_variacao
WHERE expressao_nm ~ '[\\[\\](){}*+?|^$]|\\\\'
ORDER BY 1;
```
Resultado vazio significa que nenhuma das divergências residuais é alcançável em
produção, e D-06 deixa de ser risco para virar nota de rodapé.

---

## D-07 · Reproduzir ou remover o ramo morto de `main.rs:226–230`?

**Por que importa.** O bloco é inalcançável (`ESPECIFICACAO.md`, §1.4.5) e sua
mensagem contém chaves de interpolação não expandidas, que sairiam cruas na
resposta.

**Como verificar.** Confirmar a inalcançabilidade com cobertura de código sobre
o binário Rust instrumentado, ou por inspeção — a demonstração já está em
`MAPA-DE-CHAMADAS.md`, §4.4.

**Padrão provisório.** **Não portar o bloco.** Justificativa: código
comprovadamente inalcançável não tem comportamento observável, logo omiti-lo
não viola a regra de paridade. Registrar a omissão em `CONTEXT.md` e no
registro de decisão arquitetural, com a demonstração de inalcançabilidade
anexada. Se a demonstração for refutada, o bloco volta com a mensagem literal,
chaves cruas inclusive.

---

## D-08 · Qual a resposta para rota inexistente e método não permitido?

**Por que importa.** Não está no código: é comportamento padrão do roteador do
Salvo. Os testes de contrato de F9 precisam do valor exato.

**Como verificar.** Captura empírica contra o serviço em execução:
```
GET  /rota-inexistente        → código, Content-Type, corpo
POST /ping                    → código, Content-Type, corpo
GET  /pdf                     → código, Content-Type, corpo
DELETE /pdf                   → código, Content-Type, corpo
HEAD /ping                    → código, corpo
```
Registrar o resultado literal em `ESPECIFICACAO.md`, §1.5.

**Observação.** `GET /pdf` e `DELETE /pdf` são especialmente relevantes: é
preciso saber se o middleware de autenticação executa **antes** ou **depois** da
resolução de método — ou seja, se uma requisição sem `X-API-KEY` para
`GET /pdf` responde `401` ou `405`.

**Padrão provisório.** Nenhum. É captura empírica barata e deve ser feita antes
de F9.

---

## D-09 · Manter a distinção entre 401 "ausente" e 401 "inválida"? — **DECIDIDA**

**Pergunta.** As respostas `Faltou a X-API-KEY` e `X-API-KEY inválida` revelam
se o cabeçalho foi enviado. É divulgação de informação de baixo impacto.

**Decisão.** **Manter as duas mensagens distintas.** São contrato observável e
clientes podem depender delas. Unificá-las é mudança de contrato e está fora do
escopo da migração.

**Consequência.** Registrado como **risco de segurança aceito** em `CONTEXT.md`.
A unificação pode ser proposta como evolução posterior ao corte, com
comunicação aos clientes.

---

## D-10 · Qual o `Content-Type` exato de cada resposta?

**Por que importa.** Os testes de contrato de F9 comparam cabeçalho e corpo. A
inferência é `text/plain; charset=utf-8` para todas as respostas, mas o Salvo
pode variar conforme o tipo Rust renderizado — `&'static str`, `String`,
`Text::Plain`.

**Como verificar.** Captura empírica das seis respostas de `ESPECIFICACAO.md`,
§1.4.3, registrando o cabeçalho `Content-Type` literal de cada uma.

**Padrão provisório.** `text/plain; charset=utf-8` para todas, marcado como
`INFERIDO — confirmar` na especificação até a captura.

---

## D-11 · Corpus de PDFs reais e *dump* de perfis — **BLOQUEANTE**

**Por que importa.** É o oráculo de paridade das fases F5, F6, F7 e F12. Sem
documentos reais, a verificação cobre apenas as propriedades que conseguimos
antecipar — e o risco R2 do roadmap (corpus não representativo) se materializa
depois do corte, que é o pior momento possível.

**O que é necessário.**

| Item | Quantidade mínima | Observação |
|---|---|---|
| PDFs de diário, anonimizados | 30 | amostragem estratificada por caderno e por mês |
| PDFs de fechamento de mês | 3 | volume e formato mudam nessa janela |
| PDF com camada de reconhecimento óptico | 2 | gera termos longos e caracteres irrecuperáveis |
| PDF corrompido / truncado | 1 | caminho de erro |
| PDF protegido por senha | 1 | caminho de erro |
| *Dump* anonimizado das tabelas de perfil | 1 | reproduz a consulta de chaves |
| Saídas capturadas do sistema atual | por PDF | geradas por `tools/capturar-corpus` |

**Anonimização.** Precisa **preservar as propriedades que importam** —
acentuação, hifenização no fim de linha, comprimento dos termos, presença de
`&`, caracteres não decomponíveis — substituindo nomes reais por nomes
sintéticos com o mesmo perfil de caracteres. Redação por tarja destrói
justamente o que o corpus precisa testar.

**Padrão provisório.** O corpus **sintético** gerado por
`tools/gerar-corpus-sintetico` cobre as propriedades antecipáveis e destrava o
desenvolvimento das fases F5 a F8. Ele **não substitui** o corpus real para os
critérios de saída de F12.

---

## D-12 · O `search_path` é assumido em algum lugar?

**Por que importa.** Todas as consultas qualificam o esquema `recorte.`
explicitamente, então aparentemente não há dependência. Mas se o banco de
produção define um `search_path` por papel ou por banco, o pool em Go deve
reproduzi-lo para evitar diferença em objetos não qualificados — funções,
sequências, tipos.

**Como verificar.**
```sql
SHOW search_path;
SELECT rolname, rolconfig FROM pg_roles WHERE rolconfig IS NOT NULL;
SELECT datname, datconfig FROM pg_database WHERE datconfig IS NOT NULL;
```

**Padrão provisório.** Não definir `search_path` no `AfterConnect`. Se a
verificação revelar configuração, replicá-la e registrar.

---

## D-13 · Qual o esquema real das sete tabelas?

**Por que importa.** Todo o capítulo 2 de `ESPECIFICACAO.md` está marcado como
`INFERIDO — confirmar`. Tipos, nulidade, chaves, restrições, índices e gatilhos
podem divergir do deduzido, e a divergência só apareceria em produção.

**Como verificar.**
```sql
\d+ recorte.tb_importacao
\d+ recorte.tb_recorte
\d+ recorte.tb_recorte_texto
\d+ recorte.tb_perfil
\d+ recorte.tb_perfil_variacao
\d+ recorte.tb_perfil_caderno
\d+ recorte.tb_cliente
```
Ou `pg_dump --schema-only --schema=recorte`.

**Pontos de atenção específicos:**

| Questão | Impacto |
|---|---|
| `tb_recorte.nr_pagina` é `bigint` ou `integer`? | O `bind` usa `i64` (`main.rs:603`) |
| Existe gatilho em `tb_recorte` ou `tb_recorte_texto`? | Pode alterar o efeito da gravação em lote de F11 |
| Existe chave estrangeira entre `tb_recorte` e `tb_recorte_texto`? | Determina se a falha do segundo INSERT já é impedida hoje |
| `tb_importacao.status` é anulável? | Afeta a modelagem do tipo em Go |
| Há índice em `tb_importacao.hash`? | Necessário para a idempotência de F11 |

**Padrão provisório.** Manter a reconstrução inferida e entregar a migração de
linha de base de F4 apenas com **verificações não destrutivas**, que falham no
arranque descrevendo a divergência.

---

## D-14 · A infraestrutura permite espelhar tráfego?

**Por que importa.** É o risco R8 do roadmap. A execução em sombra de F12 depende
de duplicar as requisições de produção. Descobrir a impossibilidade em F12
custa duas semanas de calendário; descobrir em F1 custa uma conversa.

**Como verificar.** Perguntar à infraestrutura se o balanceador ou a malha de
serviço em uso suporta espelhamento, e se há capacidade para um banco espelho.

**Padrão provisório.** Alternativa já desenhada: registro de requisições
(incluindo os PDFs) por um período, e reprodução em lote contra a instância Go.
Exige espaço de armazenamento e um componente de captura — que precisa ser
orçado **agora**, não em F12.

---

## D-15 · Onde estão o `Cargo.toml` e o `Cargo.lock` originais? — **BLOQUEANTE**

**Por que importa.** Sem eles: (a) D-03 não pode ser resolvida; (b) a ferramenta
`tools/capturar-corpus` não pode ser compilada contra as mesmas versões, e
portanto o corpus dourado não seria capturado do sistema real; (c) não há como
saber se há dependências, *features* ou *patches* que alterem o comportamento
das bibliotecas.

O repositório recebeu apenas o `main.rs`.

**Como verificar.** Solicitar o repositório completo do serviço Rust, ou ao
menos `Cargo.toml`, `Cargo.lock` e qualquer `build.rs`.

**Padrão provisório.** `tools/capturar-corpus` é entregue com um `Cargo.toml`
que declara as dependências **sem fixar versão exata**, acompanhado de um
`README` explicando que as versões precisam ser alinhadas ao `Cargo.lock`
original antes de a captura ter valor normativo. Enquanto isso, o corpus
sintético sustenta o desenvolvimento.

---

## D-16 · Qual o limite prático de resposta para `MAX_UPLOAD_BYTES`?

**Por que importa.** Quando a chave for ligada em F11, um corpo acima do limite
precisa de uma resposta. Hoje esse caso não existe, então **qualquer** resposta
é comportamento novo. As opções não são equivalentes para os clientes.

**Opções.**

| Opção | Código | Corpo | Observação |
|---|---|---|---|
| A | `413` | `Payload Too Large` | semanticamente correto; código novo no contrato |
| B | `400` | nova crítica, ex. `PDF excede o tamanho máximo` | reaproveita o formato de crítica existente |
| C | `422` | `Erro ao processar o PDF` | reaproveita texto existente; menos informativo |

**Padrão provisório.** Opção **B** — mantém o formato de resposta que os clientes
já sabem interpretar e acrescenta a crítica ao final da lista, preservando a
ordem das cinco existentes. Só entra em vigor com a chave ligada.

**Decisão de produto.** Precisa de confirmação antes de F11.

---

## D-17 · Quantos perfis têm expressões acentuadas? — **escalar a produto**

**Por que importa.** INV-P19 demonstra que o texto indexado tem os diacríticos
removidos e a expressão de busca **não**. Uma `expressao_nm` com qualquer
acento produz termos que não existem no índice e **nunca gera recorte**.

Isso foi **confirmado empiricamente** na fase F0: no documento
`17-inv-p19-expressao-acentuada.pdf`, que contém `MARIA DA CONCEIÇÃO SOUZA`,
a expressão `CONCEICAO SOUZA` gerou recorte e a expressão `CONCEIÇÃO SOUZA`
não gerou nenhum.

**Consequência.** Todo perfil cadastrado com acentuação está **silenciosamente
inerte em produção hoje**. O cliente paga pelo monitoramento e não recebe os
recortes. É um defeito de produto, não uma questão de migração.

**Como verificar.**
```sql
SELECT tc.id_cliente, tp.id_perfil, tpv.expressao_nm
FROM recorte.tb_perfil_variacao tpv
  JOIN recorte.tb_perfil tp  ON tp.id_perfil  = tpv.id_perfil
  JOIN recorte.tb_cliente tc ON tc.id_cliente = tp.id_cliente
WHERE tpv.expressao_nm ~ '[^\x00-\x7F]'
  AND tc.status = 'A'
ORDER BY tc.id_cliente, tp.id_perfil;
```

**Encaminhamento.** Não é decisão da migração. A migração **preserva** o
defeito (INV-P19). Este item existe para garantir que o achado chegue a quem
pode decidir corrigi-lo, com o dimensionamento do impacto em mãos.

**Padrão provisório.** Preservar o comportamento. Abrir item de produto
separado com o resultado da consulta.

---

## D-18 · PDFs truncados chegam em produção? — **escalar a produto**

**Por que importa.** INV-P20 demonstra que um PDF truncado é **aceito** pelo
MuPDF, produz zero páginas, e a importação termina em `status = 5` com
`total_recortes = 0` — indistinguível, no banco, de um diário legítimo sem
ocorrências. Arquivo vazio e arquivo que não é PDF, ao contrário, vão a `-1`.

**Consequência.** Um upload corrompido em trânsito é registrado como sucesso.
Nenhum alarme dispara e os recortes daquele diário simplesmente não existem.

**Como verificar.**
```sql
-- importações finalizadas sem nenhum recorte
SELECT date_trunc('month', data_caderno) AS mes, count(*)
FROM recorte.tb_importacao
WHERE status = 5 AND coalesce(total_recortes, 0) = 0
GROUP BY 1 ORDER BY 1 DESC;

-- comparar com a média de recortes por importação no mesmo caderno
```
Uma taxa de importações finalizadas com zero recortes acima do esperado para o
caderno é indício de documentos truncados.

**Encaminhamento.** A migração **preserva** o comportamento (INV-P20). A
detecção é candidata a evolução em F11 (`VALIDAR_ASSINATURA_PDF` não cobre este
caso — o truncado tem assinatura válida; seria preciso alertar sobre "zero
páginas extraídas"). A decisão de alertar ou rejeitar é de produto.

**Padrão provisório.** Preservar. Registrar métrica de "documentos com zero
páginas extraídas" desde F2, sem alterar comportamento — observabilidade não é
mudança de contrato.
