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
| D-01 | Existe perfil com `id_perfil = 0`? | — | **resolvida na F8** · sentinela explícito | operação |
| D-02 | Qual o `COLLATE` do banco? | F4, F8 | aberta | operação |
| D-03 | Quais as versões exatas de `tantivy`, `mupdf` e `diacritics`? | **F6, F7** | parcial · limite **medido** | quem mantém o Rust |
| D-04 | Qual o maior PDF e o maior número de recortes já processados? | F11, F12 | aberta · régua de memória **medida e não confirmada** na F12 | operação |
| D-05 | Existem expressões com `"` cadastradas? | — | aberta · **padrão provisório implementado na F8** | operação |
| D-06 | O que fazer com padrão de `&` que não compila? | F11 | aberta · **padrão provisório implementado na F8** | arquitetura |
| D-07 | Reproduzir ou remover o ramo morto de `main.rs:226–230`? | — | **resolvida na F9** · inalcançável, não portado | arquitetura |
| D-08 | Qual a resposta para rota inexistente e método não permitido? | — | **RESOLVIDA na F9** · medida | captura empírica |
| D-09 | Manter a distinção entre 401 "ausente" e 401 "inválida"? | F9 | **decidida** | arquitetura |
| D-10 | Qual o `Content-Type` exato de cada resposta? | — | **RESOLVIDA na F9** · medida | captura empírica |
| D-11 | Corpus de PDFs reais e *dump* de perfis | **F5, F6, F7, F12, F13** | aberta · **BLOQUEANTE — barra o corte** | operação |
| D-12 | O `search_path` é assumido em algum lugar? | F4 | aberta | operação |
| D-13 | Qual o esquema real das sete tabelas? | F4 | aberta | operação |
| D-14 | A infraestrutura permite espelhar tráfego? | **F12, F13** | aberta · **BLOQUEANTE — sem ela não há sombra** | infraestrutura |
| D-15 | Onde estão o `Cargo.toml` e o `Cargo.lock` originais? | **F0, F6, F13** | aberta · **BLOQUEANTE — o oráculo é condicional** | quem mantém o Rust |
| D-16 | Qual o limite prático de resposta para `MAX_UPLOAD_BYTES`? | F11 | **implementada na F11** · opção B | produto |
| D-17 | Quantos perfis têm expressões acentuadas (hoje inertes)? | — | aberta · **escalar a produto** | produto |
| D-18 | PDFs truncados chegam em produção? | — | aberta · **escalar a produto** | operação |
| D-20 | O rodapé "salvo" pode sair das páginas de 404 e 405? | — | aberta | produto |
| D-21 | O documento submetido passa a ser arquivado? | F11 (varredura) | **aberta** · descoberta na F11 | produto |
| D-22 | Escopo da transação de gravação: por chave ou por recorte? | — | **aberta** · divergência conhecida | arquitetura |
| D-23 | Os registros podem sair em stderr em vez de stdout? | — | **aberta** · divergência conhecida | operação |
| D-24 | Caminho malformado: 400 do `net/http` onde o legado dá 404 | — | **aberta** · divergência conhecida, sem conserto barato | arquitetura |
| D-25 | O legado recusa PDF acima de 64 KiB? O Salvo tem esse teto por padrão | **F13** | **aberta** · **BLOQUEANTE — depende de D-15** | quem mantém o Rust |

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

### Implementado na fase F8 — a pergunta deixou de bloquear

`usecase.deduplicadorPorPerfil` usa um booleano de primeira iteração no lugar do
`let mut id_perfil: i64 = 0`. `TestINVP13PerfilZeroNaoAlteraOResultado` exercita
justamente a sequência que distinguiria as duas implementações — perfil 0 na
primeira chave — e confirma que o resultado é o mesmo.

A resposta continua sendo útil para o inventário do banco, mas **não muda mais
nenhuma linha de código**: qualquer que seja ela, o comportamento é idêntico.

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
aspas **antes** da busca, no caso de uso — não no índice.

### Implementado na fase F8, seguindo o padrão provisório

`usecase.Pipeline.buscar` verifica `strings.ContainsRune(expressao, '"')` antes
de consultar o índice e, havendo aspas, grava −1 e encerra a importação — o
desfecho do legado. É o padrão provisório desta ficha: **reproduzir a falha**.

São seis linhas, cobertas por `TestINVP17AspasAbortamAImportacao`. Se a resposta
for **"não existem expressões com aspas em produção"**, a verificação vira código
morto e deve ser removida junto com o teste — está marcada no código para que
isso seja uma remoção de dois lugares, não uma caçada.

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
sempre — reproduzir o estado preso em `3` ou normalizar para `-1`.

### Implementado na fase F8, seguindo o padrão provisório

`usecase.Pipeline.encerrarPreso` reproduz o efeito observável do pânico: registra
o evento, contabiliza a métrica de desfecho `preso` e **não grava status algum**.
A importação fica em `recortando`, exatamente como o legado a deixaria.
`TestD06ExpressaoInvalidaDeixaAImportacaoPresa` falha se alguém trocar isso por
`-1`.

Duas observações que só existem por causa da métrica: o legado **não tem** como
distinguir uma importação presa por pânico de uma presa por queda do processo —
as duas ficam paradas no mesmo status. O contador `importacoes_total{estado="preso"}`
torna a primeira visível sem alterar o banco, e é a evidência que a resposta
desta ficha precisa.

**A normalização para −1 é da fase F11, atrás de chave.** Não pode entrar antes:
mudaria o estado final de uma importação que hoje trava.

**Um pânico de verdade em Go é caso diferente e recebe tratamento diferente.** O
único pânico alcançável no legado é o `.unwrap()` desta ficha, que em Go já virou
erro tipado. Um pânico Go restante é defeito de programação, sem comportamento
legado a preservar: `Pipeline.recuperarDePanico` registra a pilha e grava −1,
porque deixar a linha travada em silêncio por causa de um defeito nosso seria a
pior das opções.

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

## D-07 · Reproduzir ou remover o ramo morto de `main.rs:226–230`? — **RESOLVIDA**

**Por que importa.** O bloco é inalcançável (`ESPECIFICACAO.md`, §1.4.5) e sua
mensagem contém chaves de interpolação não expandidas, que sairiam cruas na
resposta.

**Resolvida na fase F9: o bloco NÃO foi portado.** Código comprovadamente
inalcançável não tem comportamento observável, logo omiti-lo não viola a regra de
paridade. A demonstração está em `MAPA-DE-CHAMADAS.md` §4.4: a crítica
`PDF não enviado` em `main.rs:211` faz `main.rs:218` retornar antes.

### E a fase F9 encontrou um SEGUNDO ramo inalcançável

A crítica **`PDF não possui nome`** (`main.rs:207`) exige `req.file("pdf")`
devolvendo `Some` com `name()` a `None`. A sonda tentou as três formas de chegar
a esse estado:

| `Content-Disposition` da parte `pdf` | Resultado no legado |
|---|---|
| `filename` **ausente** | não é arquivo → `PDF não enviado` |
| `filename=""` | **é** arquivo, nome `""` → **aceito, 200** |
| `filename=" "` | é arquivo, nome `" "` → aceito, 200 |

Não há caminho: sem `filename` não existe arquivo, e com `filename` sempre existe
nome. **A crítica é inalcançável pela camada HTTP.**

Ela continua implementada em `domain.SubmissaoPDF.Validar`, porque o domínio não
sabe de onde vem a submissão. O que a fase F9 garante é que o caminho HTTP se
comporta como o legado, e `TestClassificacaoDaParteDoArquivo` fixa as quatro
linhas da tabela acima.

## D-08 · Qual a resposta para rota inexistente e método não permitido? — **RESOLVIDA**

**Resolvida na fase F9 por medição.** `tools/sonda-http` reconstrói o roteador de
`reference/main.rs:52-60` com o mesmo encadeamento de hoops e pergunta ao próprio
Salvo. O resultado é bem mais específico do que "o padrão do roteador".

### Códigos

| Requisição | Código | Observação |
|---|---|---|
| `GET /naoexiste` | **404** | |
| `GET /` | **405** | e **não** 404 |
| `POST /ping` | **405** | |
| `DELETE /ping` | **405** | |
| `GET /pdf` **com** chave | **405** | |
| `GET /pdf` **sem** chave | **405** | o método perde **antes** da autenticação — não é 401 |
| `GET /ping/` | **200** | barra ao final é normalizada |
| `GET /PING` | **404** | o caminho é sensível a maiúsculas |

### O catcher NEGOCIA CONTEÚDO

Esta é a parte que um porte ingênuo perderia inteira. O corpo depende do
cabeçalho `Accept`:

| `Accept` | `Content-Type` | Corpo |
|---|---|---|
| ausente, `*/*` ou `text/html` | `text/html` | página de **905 bytes** (404) ou **944** (405) |
| `application/json` | `application/json` | `{"error":{"code":404,"name":"Not Found","brief":"…"}}` |
| `text/plain` | `text/plain` | `code: 404\n\nname: Not Found\n\nbrief: …` |
| `application/xml` | `application/xml` | `<?xml …><Data><code>404</code>…</Data>` |

O `http.NotFound` do Go devolveria `404 page not found\n` em `text/plain` para
todos os casos — corpo, tipo e negociação errados de uma vez só.

### Normalização de caminho — **CORRIGIDA na segunda rodada**

⚠ **A versão anterior desta seção estava errada, e estava marcada como medida.**
A sonda da F9 mediu `/ping/` e `/PING`; o resto — `//ping`, `/ping//`, `/./ping`
— foi **inferido** da leitura do Salvo e escrito como se tivesse sido medido. O
porte copiou a inferência e ficou com um `/./ping` respondendo 200 onde o legado
responde 404.

A segunda rodada (`tools/sonda-http --bin sonda-caminho`, 45 casos, servidor de
verdade com a linha de requisição escrita byte a byte) mediu. A sonda não está
na árvore; volta com `git checkout 227c97c -- tools/sonda-http`. A regra é:

> parta em `/`, descarte os segmentos **vazios**, decodifique **cada segmento**
> que sobrou, e junte de volta com `/`.

Daí saem todas as observações:

```
200   /ping   /ping/   /ping//   //ping   /ping///
200   /pi%6Eg   /%70ing   /%70%69%6E%67        o segmento decodifica
405   /   //   ///                             zero segmentos É a raiz
404   /ping/x                                  dois segmentos
404   /./ping   /.   /ping/..   /x/../ping     `.` e `..` são segmentos
404   /ping%20   /ping+   /ping%09   /ping%00  não decodifica para `ping`
404   /ping%2F   /%2Fping   /ping%2f           `%2F` não vira separador
```

O `ServeMux` do Go não faz nada disso: com o padrão `/ping`, uma requisição a
`/ping/` devolve 404. Sem a normalização, **cinco formas que hoje respondem
`pong` passariam a responder 404.**

**A ordem importa.** Em Go, `r.URL.Path` já vem decodificado, então `%2F` vira
barra **antes** do fatiamento e `/ping%2F` colapsa para `/ping` — 200 onde o
legado dá 404. O porte roteia por `r.URL.EscapedPath()` e decodifica segmento a
segmento, que é a ordem do Salvo.

O que o porte **não** alcança está em **D-24**.

### O que ficou implementado

`internal/adapter/httpapi/catcher.go` reproduz tudo, com o HTML **byte a byte**,
inclusive o rodapé com o link para `salvo.rs`. Preservar é a regra do projeto; a
remoção do rodapé está proposta em **D-20**.

### Ressalva de versão

A medição vale para **salvo 0.95.2**, fixado em `tools/sonda-http/Cargo.lock`. A
versão de produção é **D-15**, ainda aberta, e o HTML do catcher **não é contrato
estável entre versões do Salvo**. Status, `Content-Type` e a negociação são
estáveis; o texto exato precisa ser reconfirmado quando D-15 for respondida —
basta reexecutar a sonda com a versão certa.

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

## D-10 · Qual o `Content-Type` exato de cada resposta? — **RESOLVIDA**

**Resolvida na fase F9 por medição.** Todas as respostas do serviço saem com

```
text/plain; charset=utf-8
```

Confirmado pela sonda para as cinco formas de renderização que o legado usa —
`Text::Plain(&str)` na autenticação, `&String` nas críticas e `&'static str` no
sucesso, no 422 e no `pong`. A inferência que estava na especificação estava
**certa**, e agora é medição.

As respostas de rota inexistente e método não permitido **não** seguem essa
regra: são do catcher e negociam conteúdo. Ver **D-08**.

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

### Implementado na fase F11 — com uma correção do enunciado

A opção B foi implementada: `domain.CriticaPDFAcimaDoLimite`, com o texto
`PDF excede o tamanho máximo`, resposta **400**.

**O enunciado desta decisão estava errado num detalhe.** Ele dizia "acrescenta a
crítica ao final da lista, preservando a ordem das cinco existentes", supondo
que a crítica nova conviveria com as do legado. Ela **não convive**: a leitura
do corpo aborta assim que o limite estoura, e nesse ponto os campos de texto
ainda não foram lidos — não há como saber se a data do caderno também faltava.
A crítica sai **sozinha**.

Não é problema: um cliente que recebe 400 com uma única crítica a interpreta com
o mesmo código que usa para as demais. Mas quem for confirmar a decisão precisa
saber que a resposta é essa, e não a combinação.

**A fase F9 havia deixado 422 como provisório** neste caminho, antes de a decisão
existir. `TestLimiteDeCorpoLigadoRecusa` foi corrigido junto.

**O que ainda depende de produto.** Confirmar a opção B — e, se ela for aceita,
o valor do limite, que depende de **D-04**.

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

---

## D-20 · O rodapé "salvo" pode sair das páginas de 404 e 405?

**Por que importa.** As respostas de rota inexistente e método não permitido são
páginas HTML do catcher do Salvo, e o rodapé traz um link para `https://salvo.rs`
(ver **D-08**). O serviço em Go reproduz a página **byte a byte**, rodapé
incluído, porque a regra do projeto é preservar corpo de resposta.

O resultado é um serviço em Go anunciando um arcabouço em Rust que ele não usa
mais. É correto pela regra e estranho na prática.

**Como verificar se alguém depende disso.** Nenhum cliente razoável analisa o
HTML de um 404. O que pode existir é monitoração que compare o corpo, ou teste de
aceitação de terceiros. Uma busca nos registros de acesso por 404 e 405
frequentes, e a origem deles, resolve:

```sql
-- se houver registro de acesso em banco; caso contrário, no agregador de logs
SELECT rota, count(*) FROM acesso WHERE status IN (404, 405)
GROUP BY 1 ORDER BY 2 DESC LIMIT 20;
```

Volume desprezível e nenhuma origem automatizada significa que o corpo não é
contrato de ninguém.

**Padrão provisório.** **Reproduzir byte a byte**, como está. Trocar o rodapé é
alteração de corpo de resposta e precisa de decisão explícita.

**Encaminhamento.** Se a resposta for "pode sair", a troca é de uma constante em
`internal/adapter/httpapi/catcher.go` e de dois literais no oráculo de teste.
Convém decidir junto com **D-15**: se a versão de produção do Salvo tiver outro
HTML, o byte a byte atual está errado de qualquer forma e as duas coisas se
resolvem na mesma passada.

---

## D-21 · O documento submetido passa a ser arquivado? — **descoberta na F11**

**Como apareceu.** A fase F11 pede, para `VARREDURA_ORFAS`, uma política que
"reprocesse ou marque como −1, conforme política configurável". Ao implementar,
verificou-se que **reprocessar é impossível**.

**Por quê.** O serviço não guarda o documento. `recorte.tb_importacao` tem
`nome_original_pdf` e `hash`, e nada mais. Reprocessar exigiria buscar o arquivo
em algum lugar, e **não há lugar**.

Isso não é limitação da implementação: é o desenho do legado.

> ⚠ **CORRIGIDO por medição.** A frase anterior desta seção dizia que "os bytes
> do PDF vivem em memória durante o processamento (...) não há gravação em
> disco, nem no legado nem no porte". A segunda metade está certa; **a primeira
> está errada para o legado**.
>
> `main.rs:233` não lê o corpo da requisição — lê um ARQUIVO:
>
> ```rust
> let conteudo = tokio::fs::read(&arquivo.path()).await.unwrap();
> ```
>
> `arquivo.path()` só existe porque o Salvo já gravou aquela parte do multipart
> em disco. MEDIDO por `tools/sonda-http --bin sonda-arquivo`:
>
> | | Legado (Salvo 0.95.2) | Porte (Go) |
> |---|---|---|
> | Onde os bytes ficam | `/tmp/salvo_http_multipartXXXXXX/{nonce}.pdf` | só memória |
> | Existe durante a requisição? | **sim** | não |
> | Sobrevive à requisição? | **não** — `Drop` de `FilePart` apaga | — |
>
> **O que NÃO muda:** nada persiste dos dois lados, então a conclusão desta
> decisão continua valendo inteira — não há de onde reprocessar. O que muda é o
> alcance da afirmação: o legado toca o disco, o porte não, e isso importa para
> quem dimensiona `/tmp` ou monta o contêiner com sistema de arquivos somente
> leitura. Ver **D-25** para a consequência maior que a mesma medição revelou.

**Consequência imediata.** As políticas implementadas são `observar` (padrão,
apenas registra e conta) e `erro` (grava −1). `reprocessar` é recusada pela
configuração, com mensagem que aponta para esta decisão.

**Consequência maior, e a razão de a pergunta existir.** Toda importação que
falha é **perda definitiva de trabalho**. Não há reprocessamento possível para
nenhuma causa — nem para as que já existem hoje: status −1 por PDF corrompido,
importação presa por expressão inválida (D-06), tarefa morta por queda do
processo. Em todos os casos, a única recuperação é **o cliente reenviar o
documento**, e isso depende de alguém perceber e pedir.

**Como verificar o tamanho do problema.**
```sql
-- importações que terminaram em erro, por mês
SELECT date_trunc('month', data_caderno) AS mes, count(*)
FROM recorte.tb_importacao WHERE status = -1
GROUP BY 1 ORDER BY 1 DESC;

-- importações presas: em 1..3 há mais de um dia
SELECT status, count(*)
FROM recorte.tb_importacao
WHERE status IN (1, 2, 3) AND data_inicio < current_timestamp - interval '1 day'
GROUP BY 1;
```

Volume desprezível significa que arquivar não se paga. Volume relevante
significa que hoje se perde trabalho em silêncio.

**Opções, se a resposta for "sim, arquivar".**

| Opção | Onde | Custo | Observação |
|---|---|---|---|
| A | Armazenamento de objetos (S3 ou compatível) | dependência nova + credencial | O usual. O hash já existente serve de chave. |
| B | Diretório em volume persistente | nenhum código novo de dependência | Não sobrevive a contêiner efêmero sem volume; complica escalar horizontalmente. |
| C | Coluna `bytea` em `tb_importacao` | nenhuma dependência | Infla a tabela mais consultada do esquema. Não recomendado. |

**Padrão provisório.** **Não arquivar**, que é o comportamento atual. A
varredura fica com as duas políticas que não dependem do documento. Nenhuma
mudança de comportamento é introduzida por esta decisão ficar aberta.

**Encaminhamento.** É decisão de produto e de infraestrutura, não de
arquitetura: envolve custo de armazenamento, retenção e possivelmente
tratamento de dado pessoal contido nos diários.

---

## D-22 · Escopo da transação de gravação: por chave ou por recorte?

**Descoberta na auditoria de paridade posterior à F12.**

**O fato.** `salvar_recorte` do legado (`main.rs:573–631`) **não abre transação
nenhuma** — o código traz `// TODO: Implementar controle de transação?`. Cada
par de `INSERT` é confirmado sozinho.

O porte usa **uma transação por chamada**, o que torna a chave inteira atômica.

**A diferença observável.** Chave com cinco recortes, falha ao gravar o
terceiro:

| | Legado | Porte |
|---|---|---|
| Recortes daquela chave | **2 permanecem** | **0** |
| Linha órfã em `tb_recorte` sem texto | possível | impossível |

O caminho feliz é idêntico. A diferença só existe quando a gravação falha no
meio de uma chave.

**Como surgiu.** Consequência direta do achado **A03**: o par
`tb_recorte`/`tb_recorte_texto` não fica atômico sem transação, e a transação
por chamada arrasta a chave inteira.

**Opções.**

| Opção | Efeito | Custo |
|---|---|---|
| A — manter por chamada | O que existe hoje. Mais limpo que o legado, mas diverge dele no caminho de falha. | nenhum |
| B — transação por PAR de recortes | Reproduz o legado com exatidão E mantém A03 corrigido. | uma transação por recorte em vez de uma por chave |
| C — sem transação | Paridade total, inclusive a linha órfã. | reintroduz o defeito A03 |

**Padrão provisório.** Manter a **opção A**. A diferença só aparece num caminho
de falha raro, e o estado que ela produz é estritamente mais limpo — nunca há
recorte sem texto.

**Por que ainda assim é decisão.** A regra do projeto é paridade total, e isto é
uma divergência. Se ela precisar sumir, a **opção B** a elimina sem custo de
correção: é trocar o escopo do `EmTransacao` de `Salvar` para o laço interno.

**Como verificar se importa em produção.**
```sql
-- importações em -1 que ainda assim têm recortes: é o caso em que o escopo
-- da transação muda o resultado
SELECT count(*) FROM recorte.tb_importacao i
WHERE i.status = -1
  AND EXISTS (SELECT 1 FROM recorte.tb_recorte r WHERE r.id_importacao = i.id_importacao);
```
Contagem desprezível significa que o caminho de falha quase não acontece e a
opção A se sustenta.

---

## D-23 · Os registros podem sair em stderr em vez de stdout?

**Descoberta na auditoria de paridade posterior à F12.**

**O fato.** O legado usa `tracing_subscriber::fmt().init()` (`main.rs:34`), cuja
saída padrão é **stdout**, e `println!` para as mensagens de sinal
(`main.rs:104–105`) — também stdout.

O porte escreve **tudo em stderr** (`cmd/recorte-api/main.go:83`), em **JSON**
por padrão, com campos estruturados.

**Duas diferenças, não uma.**

| | Legado | Porte |
|---|---|---|
| Destino | stdout | **stderr** |
| Formato | texto do `tracing` | **JSON** (`LOG_FORMATO=text` dá texto do `slog`, ainda diferente) |

**Por que não foi tratado como divergência até aqui.** A fase F2 tratou
observabilidade como MELHORIA, não como contrato — registro estruturado,
identificador de requisição e nível configurável são o ponto de ter reescrito.
A especificação nunca prometeu paridade de log.

**Quando importa.** `docker logs`, `journald` e a maioria dos coletores capturam
os dois fluxos e a diferença é invisível. Ela aparece em:

- `./recorte-api > app.log` — o arquivo fica **vazio**;
- qualquer coisa que analise o texto do log do legado — deixa de casar, e isso
  vale para o formato tanto quanto para o destino.

**Padrão provisório.** Manter stderr e JSON. É a convenção para serviço em
contêiner, e o formato estruturado é o que torna o log consultável.

**Se a resposta for "precisa ser stdout".** É a troca de `os.Stderr` por
`os.Stdout` em `cmd/recorte-api/main.go`, uma linha. O formato é
`LOG_FORMATO=text`, que já existe.

---

## D-24 · Caminho malformado: 400 do `net/http` onde o legado dá 404

**Descoberta na segunda rodada de medição do roteamento**, motivada por um
relato de campo: um `curl` com espaço sobrando na URL virou `GET /ping%20` e
recebeu 404. A investigação confirmou que **esse caso está certo** — o legado
também responde 404 —, e no caminho encontrou 14 casos em que o porte
divergia. Onze foram corrigidos. Estes cinco não têm conserto proporcional.

### O que diverge

| Requisição crua | Legado | Porte | Camada |
|---|---|---|---|
| `GET /ping%` | 404 + catcher | **400** + `400 Bad Request` | análise de URL do `net/http` |
| `GET /ping%2` | 404 + catcher | **400** | idem |
| `GET /ping%zz` | 404 + catcher | **400** | idem |
| `GET /ping#f` | 200 `pong` | **404** + catcher | idem |
| `GET /pdf#x` | 405 + catcher | **404** + catcher | idem |
| `GET /ping\tx` | 400, corpo **vazio** | 400, corpo `400 Bad Request` | idem |

### Por que não tem conserto barato

**Percentual inválido.** `url.ParseRequestURI` recusa `/ping%zz`, e o
`net/http` responde 400 **sozinho**, antes de qualquer `Handler`. Nenhum
middleware vê a requisição — o `http.Server` não expõe gancho para requisição
malformada. A única saída seria um invólucro de `net.Listener` reescrevendo a
linha de requisição antes de o servidor a analisar, o que significa
reimplementar o enquadramento do HTTP — *keep-alive*, *pipelining*, TLS — a
troco de trocar um código de erro por outro.

**Fragmento cru.** O Salvo descarta `#…` e roteia `/ping`; o Go mantém a
cerquilha no caminho. E os dois casos são **indistinguíveis** depois da análise:
MEDIDO, `EscapedPath()` devolve `/ping%23f` tanto para `/ping#f` quanto para
`/ping%23f`, que no legado respondem 200 e 404. Separá-los exigiria rotear por
`r.RequestURI` — trocando a entrada do roteador inteiro, com a forma absoluta
(`http://host/path`, de procurador) e o `*` do `OPTIONS` junto.

### Por que a recomendação é ACEITAR

1. **Nenhuma é alcançável por cliente conforme.** Pela RFC 3986 §3.5 o
   fragmento **não é enviado ao servidor**: navegador, `curl`, Postman,
   `reqwest` e o cliente do Go todos o cortam. Percentual inválido e tabulação
   crua na linha de requisição também não saem de biblioteca nenhuma. Só um
   socket escrito à mão chega a estes casos.
2. **Nenhuma perde função nem afrouxa acesso.** Nos três primeiros a resposta
   continua sendo erro; nos dois de fragmento o porte é **mais restritivo** que
   o legado — 404 onde ele daria 200 ou 405 —, então nada que o legado protegia
   passa a ser alcançável.
3. **O custo do conserto é desproporcional**, e o próprio conserto é a classe de
   código que introduz falha pior do que a que corrige.

### O que ficou no lugar

`internal/adapter/httpapi/caminho_test.go` tem a tabela
`divergenciasConhecidas`, que **asserta** cada linha acima — não anota, asserta.
Se uma versão futura do Go passar a responder o que o legado responde, o teste
falha pedindo que a linha saia daqui e volte à tabela medida. Se passar a
responder uma terceira coisa, falha também.

### Se a resposta for "precisa ser idêntico"

Só há um caminho: intermediar a conexão antes do `net/http`. Estimativa honesta
— um `net.Listener` que envolve cada `net.Conn` com um leitor que localiza a
linha de requisição de **cada** requisição da conexão (não só a primeira),
reescreve `%` inválido como `%25` e corta o fragmento. Precisa acertar
`Content-Length`, corpo em pedaços e atualização para WebSocket, e passa a ser
código de segurança nosso, não do `net/http`. Recomendação: não.

---

## D-25 · O legado recusa PDF acima de 64 KiB? — **BLOQUEANTE**

**Descoberta ao medir onde o legado põe o arquivo submetido** (D-21), a partir de
uma pergunta simples de fora: "onde estaria o PDF quando a API estiver em
produção?".

### O fato medido

`tools/sonda-http --bin sonda-arquivo` (recuperável com
`git checkout 71e5775 -- tools/sonda-http`) reconstrói o caminho de `main.rs:203-234`
e submete multipart real contra um servidor Salvo real:

| Tamanho da parte `pdf` | Corpo total | Resposta |
|---|---|---|
| 32.768 bytes | 33.251 | `200 PDF carregado com sucesso` |
| 65.136 bytes | 65.619 | **`400 PDF não enviado`** |
| 65.536 bytes | 66.019 | **`400 PDF não enviado`** |
| 131.072 bytes | 131.555 | **`400 PDF não enviado`** |
| **165.447 — o DOU real do repositório** | 165.930 | **`400 PDF não enviado`** |

A causa está no fonte do arcabouço, não no `main.rs`:

```rust
// salvo_core-0.95.2/src/http/request.rs:33
static GLOBAL_SECURE_MAX_SIZE: AtomicUsize = AtomicUsize::new(64 * 1024);
```

O limite vale para o **corpo inteiro**, não só para a parte de arquivo. Estourado
ele, `req.file("pdf")` devolve `None`, o `main.rs:210` acumula a crítica
`PDF não enviado` e a resposta é 400 — **indistinguível de uma requisição que
realmente não trouxe arquivo**.

O `main.rs` **não configura nada disso**: não há `set_secure_max_size`,
`set_global_secure_max_size` nem o middleware `SecureMaxSize` em lugar nenhum
das 730 linhas.

### Quando o teto entrou no Salvo — também medido

| Versão do `salvo_core` | Tem `GLOBAL_SECURE_MAX_SIZE`? |
|---|---|
| 0.65.0 | **não** |
| 0.70.0 | **não** |
| 0.75.0 | sim, 64 KiB |
| 0.80.0 · 0.85.0 · 0.88.0 · 0.89.0 | sim, 64 KiB |
| 0.90.0 · 0.94.0 · 0.95.0 · 0.95.1 · 0.95.2 | sim, 64 KiB |

O teto entrou entre **0.70 e 0.75** e nunca mudou de valor desde então.

### Por que isso é grave, e por que provavelmente NÃO é o que roda em produção

Um serviço que existe para ingerir Diários Oficiais e recusa qualquer documento
acima de 64 KiB não processaria diário nenhum: o DOU real deste repositório tem
162 KiB **em uma única página**, e edições completas têm megabytes.

Duas leituras, e nenhuma pode ser escolhida sem D-15:

**(a) O legado roda Salvo < 0.75.** Sem o teto, aceita qualquer tamanho. É a
leitura compatível com o serviço funcionar. Nesse caso o porte já está correto e
esta decisão fecha sem código.

**(b) O legado roda Salvo ≥ 0.75.** Então ele recusa diários de verdade **hoje,
em produção**, com `400 PDF não enviado` — e a recusa é silenciosa do ponto de
vista do banco: sem registro em `tb_importacao`, sem status −1, sem nada. Só
apareceria em quem lê o corpo da resposta do `POST /pdf`.

A leitura (b) tem uma consequência desconfortável: se for verdade, existe um
defeito grave e não percebido no legado, e o porte em Go — que aceita — está
**consertando algo em silêncio**, contra a regra dura do projeto.

### Como responder

1. Obter o `Cargo.lock` de produção (**D-15**) e ler a versão de `salvo_core`.
2. Se ≥ 0.75, confirmar contra o serviço em produção com um documento real:
   ```sh
   curl -i -X POST https://<legado>/pdf -H "X-API-KEY: …" \
     -F data-caderno=2026-07-08 -F data-disponibilizacao=2026-07-08 \
     -F id-usuario=1 -F id-caderno=1 -F pdf=@diario-real.pdf
   ```
   `400 PDF não enviado` para um PDF que existe confirma o teto.
3. Conferir se há intermediário — Nginx, balanceador, API *gateway* — que já
   recuse ou já reescreva o corpo antes de o Salvo ver.

### Padrão provisório

**Não impor limite**, que é o comportamento atual do porte: `MAX_UPLOAD_BYTES`
nasce em `0`, desligado. É o único padrão sob o qual o serviço cumpre a função
que ele visivelmente cumpre em produção.

Registrado como **risco aceito**: se a resposta for (b), o porte diverge do
legado aceitando o que ele recusa.

### Se a resposta for (b) — o que implementar

A chave **já existe**: `MAX_UPLOAD_BYTES=65536`. O que ela **não** reproduz é a
resposta — hoje ela devolve 400 com crítica de corpo grande (D-16, opção B),
enquanto o legado devolve 400 `PDF não enviado`, sem distinguir de arquivo
ausente. Reproduzir exigiria uma segunda opção de política, não código novo de
leitura.

### Relação com as outras decisões

- **D-15** — sem o `Cargo.lock` não há como escolher entre (a) e (b). É mais uma
  consequência concreta daquela pergunta ficar aberta.
- **D-16** — mediu o limite prático de `MAX_UPLOAD_BYTES` sem saber que o
  arcabouço já impunha um. A opção B continua válida; ganha um valor candidato.
- **D-04** — "qual o maior PDF já processado" deixa de ser curiosidade de
  capacidade e vira evidência direta: qualquer resposta acima de 64 KiB **prova
  a leitura (a)** e fecha esta decisão.
