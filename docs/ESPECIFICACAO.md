# Especificação executável — Serviço de recorte de diários oficiais

> **Fase F0.** Este documento é a especificação normativa do comportamento
> observável do serviço, reconstruída por engenharia reversa de
> `reference/main.rs` (SHA-256 `4c5a7b48…828b9580`, 730 linhas).
>
> **Regra de precedência:** o código Rust é o oráculo. Onde esta especificação
> divergir dele, o código vence e esta especificação é corrigida.
>
> **Convenções:**
> - `main.rs:123` — citação de linha da referência.
> - **`INFERIDO — confirmar`** — afirmação deduzida, não observada. Precisa de
>   confirmação contra o sistema em produção antes de virar teste normativo.
> - **`DEFEITO PRESERVADO`** — comportamento que parece defeito e que a
>   reescrita deve reproduzir mesmo assim.

---

## Sumário

1. [Contrato HTTP](#1-contrato-http)
2. [Esquema de banco de dados](#2-esquema-de-banco-de-dados)
3. [Máquina de estados da importação](#3-máquina-de-estados-da-importação)
4. [Pipeline de normalização de texto](#4-pipeline-de-normalização-de-texto)
5. [Semântica do laço de recorte](#5-semântica-do-laço-de-recorte)
6. [Ciclo de vida do processo](#6-ciclo-de-vida-do-processo)
7. [Evoluções atrás de chave — o que NÃO é contrato do legado](#7-evoluções-atrás-de-chave--o-que-não-é-contrato-do-legado)

---

## 1. Contrato HTTP

### 1.1 Configuração de escuta

O endereço é `{SERVIDOR_IP}:{SERVIDOR_PORTA}` (`main.rs:50`).

| Variável | Padrão | Comportamento na leitura |
|---|---|---|
| `SERVIDOR_IP` | `192.168.42.1` (`main.rs:24`) — **o porte usa `0.0.0.0`, ver abaixo** | `unwrap_or` — qualquer valor é aceito sem validação (`main.rs:44`) |
| `SERVIDOR_PORTA` | `6001` (`main.rs:25`) | `unwrap_or_default().parse::<u16>().unwrap_or(6001)` (`main.rs:45–48`) |

> **DIVERGÊNCIA DELIBERADA — endereço de escuta.**
>
> O porte usa `0.0.0.0` como padrão. `192.168.42.1` só existe na rede em que o
> serviço original rodava: numa máquina de desenvolvimento ou dentro de um
> contêiner, ligar nele falha com *cannot assign requested address* e o processo
> não sobe.
>
> `0.0.0.0` é um **superconjunto** — atende em todas as interfaces, inclusive na
> `192.168.42.1` quando ela existe. Nenhuma requisição que chegava ao serviço
> antes deixa de chegar; o que muda é que ele também atende nas demais.
> `SERVIDOR_IP=192.168.42.1` restaura o comportamento exato do legado, e a
> constante `config.ServidorIPDoLegado` guarda o valor.
>
> **Credencial.** `API_KEY` também ganhou padrão, com o MESMO valor que o legado
> trazia como constante no código-fonte (`main.rs:26`). O legado não lia essa
> variável; o porte lê, e sem ela usa aquele valor. Ver `config.APIKeyPadrao`.

Consequências de `SERVIDOR_PORTA`, todas **`DEFEITO PRESERVADO`**:

| Valor | Resultado | Motivo |
|---|---|---|
| não definida | `6001` | `unwrap_or_default()` devolve `""`, `parse` falha |
| `"abc"` | `6001` | `parse::<u16>` falha |
| `"70000"` | `6001` | excede `u16::MAX` |
| `"-1"` | `6001` | `u16` não aceita sinal |
| `""` | `6001` | `parse` falha |
| `"0"` | escuta em porta efêmera atribuída pelo sistema | `0` é um `u16` válido |

`DATABASE_URL` é obrigatória: a ausência produz pânico com a mensagem
`Variável ambiental DATABASE_URL` (`main.rs:36`).

O arquivo `.env` é carregado antes, com falha silenciosa (`main.rs:32`).

### 1.2 Cadeia de middleware

| Camada | Aplicada em | Origem |
|---|---|---|
| `Logger` | `Service` — envolve tudo, inclusive 404 | `main.rs:60` |
| `RequestId` | roteador raiz — todas as rotas | `main.rs:59` |
| `autenticar` | apenas `/pdf` | `main.rs:54` |
| `affix_state::inject(tarefas)` | apenas `/pdf` | `main.rs:55` |

`RequestId::new()` define o cabeçalho de requisição `x-request-id` quando
ausente. `upload_pdf` o lê em `main.rs:134` com `unwrap_or_default()` — nunca
falha, no pior caso é a cadeia vazia.

### 1.3 `GET /ping`

| Item | Valor |
|---|---|
| Autenticação | **nenhuma** (`main.rs:52`) |
| Código | `200` |
| Corpo | `pong` (`main.rs:114`) |
| `Content-Type` | `text/plain; charset=utf-8` — **MEDIDO** na fase F9 |

Não verifica o banco nem qualquer dependência. Responde `pong` com o
PostgreSQL indisponível.

### 1.4 `POST /pdf`

Autenticado. Corpo `multipart/form-data`.

#### 1.4.1 Campos de entrada

| Nome do campo | Tipo | Formato | Leitura |
|---|---|---|---|
| `data-caderno` | texto | `%Y-%m-%d` | `main.rs:138`, análise em `152` |
| `data-disponibilizacao` | texto | `%Y-%m-%d` | `main.rs:139`, análise em `165` |
| `id-usuario` | texto | inteiro `i64` | `main.rs:141`, análise em `178` |
| `id-caderno` | texto | inteiro `i32` | `main.rs:142`, análise em `191` |
| `pdf` | arquivo | — | `main.rs:143` |

Os nomes usam **hífen**, não sublinhado. Campos repetidos: prevalece o
**primeiro** — **MEDIDO** na fase F9 contra multipart real.

`%Y-%m-%d` é **permissivo**, ao contrário do que esta especificação afirmava
até a fase F3. Medido contra chrono 0.4: `2024-3-15`, `2024-03-5`, `24-03-15`,
`  2024-03-15`, `+2024-03-15` e `-2024-03-15` são **todos aceitos**. A gramática
completa e a tabela de medição estão em `INVARIANTES.md`, **INV-P21** — que é
uma armadilha de paridade, porque `time.Parse("2006-01-02", …)` do Go rejeita
os seis casos e mudaria o corpo da resposta 400.

#### 1.4.2 Textos de crítica — literais normativos

Ordem de acumulação fixa (`main.rs:151–214`). É contrato observável.

| Ordem | Condição | Texto literal | Linha |
|---|---|---|---|
| 1 | `data-caderno` presente e não analisável | `Data do caderno é inválida` | 155 |
| 1 | `data-caderno` ausente | `Data do caderno não informada` | 159 |
| 2 | `data-disponibilizacao` presente e não analisável | `Data de disponibilização é inválida` | 168 |
| 2 | `data-disponibilizacao` ausente | `Data de disponibilização não informada` | 172 |
| 3 | `id-usuario` presente e não analisável | `Id do usuário é inválido` | 181 |
| 3 | `id-usuario` ausente | `Id do usuário não informado` | 185 |
| 4 | `id-caderno` presente e não analisável | `Id do caderno é inválido` | 194 |
| 4 | `id-caderno` ausente | `Id do caderno não informado` | 198 |
| 5 | arquivo presente e sem nome | `PDF não possui nome` | 207 |
| 5 | arquivo ausente | `PDF não enviado` | 211 |

Por posição, no máximo uma crítica é acrescentada. O separador é
`,` — vírgula **sem espaço** (`main.rs:220`, `criticas.join(",")`).

**Exemplo completo** — requisição sem nenhum campo:

```
Data do caderno não informada,Data de disponibilização não informada,Id do usuário não informado,Id do caderno não informado,PDF não enviado
```

**Exemplo misto** — datas inválidas, ids ausentes, PDF presente:

```
Data do caderno é inválida,Data de disponibilização é inválida,Id do usuário não informado,Id do caderno não informado
```

#### 1.4.2.1 De onde vem o arquivo, e onde ele fica — **MEDIDO**

O PDF **chega na própria requisição**, na parte `pdf` do multipart. O serviço
não busca o documento em disco, em fila, em armazenamento de objeto nem em
diretório vigiado: quem submete é o cliente que chama `POST /pdf`. Não existe
outro caminho de entrada.

O que difere entre legado e porte é **onde os bytes ficam durante a requisição**:

| | Legado (Salvo) | Porte (Go) |
|---|---|---|
| Leitura do corpo | Salvo grava a parte em arquivo temporário; `main.rs:233` lê de `arquivo.path()` | `r.MultipartReader()` lê do socket direto para memória |
| Caminho | `/tmp/salvo_http_multipartXXXXXX/{nonce}.pdf` | — |
| Sobrevive à requisição? | **não** — `Drop` de `FilePart` apaga arquivo e diretório | — |
| Persiste depois? | **não** | **não** |

MEDIDO por `tools/sonda-http --bin sonda-arquivo`. O efeito observável pela API é
**idêntico**; a diferença é o meio, e importa para quem dimensiona `/tmp` ou
monta o contêiner com sistema de arquivos somente leitura.

O que sobrevive à importação, dos dois lados, é só `nome_original_pdf` e `hash`
em `tb_importacao`, mais o texto das páginas que geraram recorte. **O documento
em si não é guardado em lugar nenhum** — ver `DECISOES-ABERTAS.md`, **D-21**.

> ⚠ **O arcabouço impõe um teto que o `main.rs` não pede.** O `salvo_core` traz
> `GLOBAL_SECURE_MAX_SIZE = 64 KiB` desde a versão 0.75, aplicado ao corpo
> inteiro. Acima dele `req.file("pdf")` devolve `None` e a resposta é
> **400 `PDF não enviado`** — indistinguível de arquivo ausente. Se a versão de
> produção for ≥ 0.75, o legado recusa qualquer diário real. O porte **não**
> impõe teto por padrão. É **D-25**, bloqueante, e depende de **D-15**.

#### 1.4.3 Respostas

| # | Condição | Código | Corpo (literal) | Linhas |
|---|---|---|---|---|
| R1 | Cabeçalho `X-API-KEY` ausente | `401` | `Faltou a X-API-KEY` | 126–128 |
| R2 | `X-API-KEY` presente e diferente da constante | `401` | `X-API-KEY inválida` | 121–123 |
| R3 | Uma ou mais críticas | `400` | críticas unidas por `,` | 218–224 |
| R4 | Falha ao inserir em `tb_importacao` | `422` | `Erro ao processar o PDF` | 239–244 |
| R5 | Sucesso | `200` | `PDF carregado com sucesso` | 339–340 |
| R6 | Ramo inalcançável (ver §1.4.5) | `400` | `[ID Requisição: {id_requisicao}] -> PDF não enviado` | 226–230 |

`Content-Type` de todas: `text/plain; charset=utf-8`. **MEDIDO** na fase F9 por
`tools/sonda-http`, para as três formas de renderização que o legado usa —
`Text::Plain(&str)`, `&String` e `&'static str`. Ver `DECISOES-ABERTAS.md`, D-10.

R1 e R2 têm textos **distintos**. A diferença revela se a chave existe — é
divulgação de informação, e é contrato existente. Preservada. Ver
`DECISOES-ABERTAS.md`, D-09.

A resposta R5 é emitida **antes** do processamento (`main.rs:339` executa antes
do término da tarefa disparada em `257`). O cliente não tem como saber o
desfecho: não existe endpoint de consulta de status.

#### 1.4.4 Autenticação

```rust
if let Some(key) = req.headers().get("X-API-KEY") {   // main.rs:119
    if key != API_KEY { /* 401 R2 */ }                // main.rs:120
} else { /* 401 R1 */ }                               // main.rs:125
```

- Nome do cabeçalho: `X-API-KEY`. A busca em `HeaderMap` é insensível a
  maiúsculas, então `x-api-key` também é aceito. **MEDIDO** na fase F9.
- Comparação de `HeaderValue` com `&str`: byte a byte, com **encerramento
  antecipado**. Vulnerável a ataque de tempo. **`DEFEITO PRESERVADO`** quanto ao
  resultado; a reescrita usa comparação de tempo constante, o que não altera
  nenhuma resposta observável.
- Cabeçalho presente e vazio ⇒ R2 (diferente da constante).
- Cabeçalho repetido: `HeaderMap::get` devolve o primeiro. **MEDIDO** na fase F9.

#### 1.4.5 Caminho inalcançável em `main.rs:226–230`

`if arquivo.is_none()` só poderia ser verdadeiro se a crítica `PDF não enviado`
tivesse sido acrescentada em `211`, o que faria `218` retornar antes. O bloco é
**código morto**.

Sua mensagem (`main.rs:228`) usa uma string literal sem interpolação: as chaves
`{id_requisicao}` sairiam **cruas** na resposta. **D-07 foi resolvida na fase
F9: o bloco não é portado.**

#### 1.4.5.1 Segundo caminho inalcançável — `PDF não possui nome`

**Descoberto na fase F9 por medição.** A crítica de `main.rs:207` exige
`req.file("pdf")` devolvendo `Some` com `name()` a `None`. Não existe corpo
multipart que produza esse estado:

| `Content-Disposition` da parte `pdf` | `req.file("pdf")` | Resultado |
|---|---|---|
| `filename` ausente | `None` | crítica `PDF não enviado` |
| `filename=""` | `Some`, nome `""` | **aceito**, grava nome vazio |
| `filename=" "` | `Some`, nome `" "` | aceito |

O parâmetro `filename` é o que faz a parte ser um arquivo; havendo arquivo,
sempre há nome. A crítica é **código morto**, como o bloco de 226–230.

**Armadilha de paridade.** O `ParseMultipartForm` do Go classifica
`filename=""` como VALOR, não como arquivo — porque `Part.FileName()` devolve a
cadeia vazia tanto para `filename=""` quanto para `filename` ausente. Usá-lo
transformaria uma requisição hoje ACEITA em `400 PDF não enviado`. A camada HTTP
analisa as partes à mão por causa disso; ver
`internal/adapter/httpapi/multipart.go`.

#### 1.4.6 Sequência do caminho síncrono

1. Lê `x-request-id` (`134`) e registra a chegada (`136`).
2. Lê os quatro campos de texto e o arquivo (`138–143`).
3. Valida acumulando críticas (`151–214`).
4. Registra os parâmetros recebidos (`216`).
5. Havendo crítica: registra o erro (`219`) e responde R3 (`221–223`).
6. Lê o conteúdo do arquivo temporário — `unwrap()`, pânico possível (`233`).
7. Calcula SHA-256 e o guarda em `hash_sha256` (`234–235`).
8. `registrar_pdf` (`237`); erro ⇒ registra (`240`) e responde R4 (`241–243`).
9. `atualizar_status_importacao(id, 0)` — resultado descartado (`249`).
10. Obtém o contador do `Depot` e incrementa (`251–252`).
11. Dispara a tarefa de segundo plano (`257`).
12. Responde R5 (`339–340`).

**Sem limite de tamanho de corpo e sem validação de tipo do arquivo.** Qualquer
arquivo enviado no campo `pdf` é aceito; a falha só aparece no processamento
assíncrono, como status −1.

> As chaves `MAX_UPLOAD_BYTES` e `VALIDAR_ASSINATURA_PDF` mudam isso — e as duas
> nascem desligadas, justamente para que o descrito acima continue sendo o
> comportamento padrão. Ver §7.

### 1.5 Rota inexistente e método não permitido — **MEDIDO**

Capturado na fase F9 por `tools/sonda-http`, que reconstrói o roteador de
`main.rs:52-60`. Detalhe completo em `DECISOES-ABERTAS.md`, **D-08**.

| Requisição | Código |
|---|---|
| `GET /naoexiste` | 404 |
| `GET /` | **405** — e não 404 |
| `POST /ping`, `DELETE /ping` | 405 |
| `GET /pdf`, com ou **sem** chave | **405** — o método perde antes da autenticação |
| `GET /ping/` | **200** |
| `GET /PING` | 404 — o caminho é sensível a maiúsculas |

**O corpo depende do cabeçalho `Accept`.** O catcher do Salvo negocia conteúdo:

| `Accept` | `Content-Type` | Corpo |
|---|---|---|
| ausente, `*/*`, `text/html` | `text/html` | página de 905 bytes (404) / 944 (405) |
| `application/json` | `application/json` | `{"error":{"code":…,"name":…,"brief":…}}` |
| `text/plain` | `text/plain` | `code: …\n\nname: …\n\nbrief: …` |
| `application/xml` | `application/xml` | `<?xml …><Data>…</Data>` |

⚠ O HTML foi medido contra **salvo 0.95.2** e não é contrato estável entre
versões do arcabouço. Status, `Content-Type` e negociação são. Reconfirmar
quando **D-15** for respondida.

#### 1.5.1 Normalização de caminho — **MEDIDO (segunda rodada)**

A primeira rodada (F9) só mediu `/ping/` e `/PING`, e o resto desta seção era
**inferido apesar de estar marcado como medido**. A segunda rodada
(`tools/sonda-http --bin sonda-caminho`, recuperável com
`git checkout 227c97c -- tools/sonda-http`) mediu 45 casos contra o Salvo de
verdade e corrigiu duas afirmações. Detalhe em **D-08** e **D-24**.

A regra medida, em uma frase:

> parta em `/`, descarte os segmentos **vazios**, decodifique **cada segmento**
> que sobrou, e junte de volta com `/`.

| Caminho | Código | Por quê |
|---|---|---|
| `/ping` `/ping/` `/ping//` `//ping` `/ping///` | 200 | segmento vazio some |
| `/pi%6Eg` `/%70ing` `/%70%69%6E%67` | 200 | o segmento decodifica para `ping` |
| `/ping?x=1` | 200 | a query não é caminho |
| `/` `//` `///` | **405** | zero segmentos **é a raiz**, que existe sem método |
| `/ping/x` | 404 | dois segmentos não casam com um |
| `/./ping` `/.` `/./ ` `/ping/..` `/x/../ping` | **404** | `.` e `..` são segmentos, **não** são ignorados |
| `/ping%20` `/%20ping` `/ping+` `/ping%09` `/ping%00` | 404 | não decodifica para `ping` |
| `/ping%2F` `/%2Fping` `/ping%2f` | **404** | barra vinda de `%2F` **não** vira separador |
| `/ping%23f` | 404 | cerquilha codificada é literal |
| `/ping%zz` `/ping%2` `/ping%` | 404 | percentual inválido não decodifica |
| `/ping#f` | 200 | o fragmento cru é descartado |

**As duas correções.** `/./ping` responde **404**, não 200 — o Salvo descarta os
segmentos vazios e só eles. E `GET /` responde **405** em qualquer método,
porque no legado a raiz é rota (`Router::new()` casa o caminho vazio) e não tem
método; `catcher.go` já registrava isso desde a F9, o roteador não fazia.

**A ordem importa: decodificar DEPOIS de partir.** Em Go, `r.URL.Path` já vem
decodificado, então `%2F` vira barra **antes** do fatiamento e `/ping%2F`
colapsa para `/ping`. O porte roteia por `r.URL.EscapedPath()` e decodifica
segmento a segmento, que é a ordem do Salvo. O efeito colateral é de segurança,
e é o lado bom: nenhuma forma codificada alcança rota que a forma literal não
alcançaria.

**Duas linhas o porte não reproduz** — `/ping%zz` (o `net/http` responde 400
antes de qualquer manipulador) e `/ping#f` (indistinguível de `/ping%23f` depois
da análise de URL do Go). São **D-24**, e nenhuma é alcançável por cliente
conforme.

---

## 2. Esquema de banco de dados

> Todo este capítulo é **`INFERIDO — confirmar`**. Nenhuma migração existe no
> projeto; o esquema foi reconstruído a partir das cinco consultas literais.
> Tipos, nulidade, chaves e restrições precisam ser confrontados com o banco
> real. A fase F4 entrega uma migração de linha de base que **verifica** esta
> reconstrução no arranque.

Esquema: `recorte`. **`INFERIDO — confirmar`**: não há `SET search_path`; todas
as consultas qualificam o esquema explicitamente.

### 2.1 `recorte.tb_importacao`

Escrita por `registrar_pdf`, `atualizar_status_importacao`,
`registrar_inicio_importacao`, `registrar_termino_importacao`.
Lida por `obter_chaves_pesquisa`.

| Coluna | Tipo inferido | Nulidade | Evidência |
|---|---|---|---|
| `id_importacao` | `bigint` gerado | não nulo, PK | `RETURNING id_importacao` (`461`) coletado em `(i64,)` (`464`) |
| `id_inclusao` | `bigint` | anulável | `bind(Option<i64>)` (`466`) |
| `id_cadernos` | `integer` | anulável | `bind(Option<i32>)` (`467`); comparado em `554` |
| `data_caderno` | `date` | anulável | `bind(Option<NaiveDate>)` (`468`) |
| `data_disponibilizacao` | `date` | anulável | `bind(Option<NaiveDate>)` (`469`) |
| `status` | `integer` | anulável | `bind(Option<i32>)` (`470`); `bind(i32)` (`674`) |
| `nome_original_pdf` | `text` | anulável | `bind(Option<String>)` (`471`) |
| `tipo_caderno` | `text` | anulável | `bind(Option<String>)` (`472`) |
| `hash` | `text` | anulável | `bind(Option<String>)` (`473`) |
| `data_inicio` | `timestamp` | anulável | `= current_timestamp` (`687`) |
| `data_fim` | `timestamp` | anulável | `= current_timestamp` (`705`) |
| `total_recortes` | `integer` | anulável | `bind(i32::try_from(...))` (`712`) |

`data_inicio` e `data_fim` recebem `current_timestamp`, **avaliado no servidor
PostgreSQL** — não no processo. A reescrita não pode substituir por hora local
do serviço.

### 2.2 `recorte.tb_recorte`

| Coluna | Tipo inferido | Evidência |
|---|---|---|
| `id_recorte` | `bigint` gerado, PK | `RETURNING id_recorte` (`584`) em `(i64,)` (`601`) |
| `id_importacao` | `bigint` | `bind(id_pdf: i64)` (`602`) |
| `nr_pagina` | `bigint` | `bind(i64::try_from(recorte.page)?)` (`603`) |
| `id_perfil` | `bigint` | `bind(chave.id_perfil: i64)` (`604`) |
| `expressao_busca` | `text` | `bind(&chave.expressao_nm)` (`605`) |
| `dt_recorte` | `timestamp` | `current_timestamp` (`583`) |

### 2.3 `recorte.tb_recorte_texto`

| Coluna | Tipo inferido | Evidência |
|---|---|---|
| `id_recorte` | `bigint` | `bind(id_recorte)` (`618`) |
| `origem` | `text` | literal `'PDF'` no SQL (`594`) |
| `recorte` | `text` | `bind(&recorte.highlight)` (`619`) |

**Conteúdo de `recorte`:** o **texto integral da página**, já normalizado e sem
diacríticos — não um trecho ao redor da ocorrência. Ver §5.4.

Relação com `tb_recorte`: 1:1 na prática (uma inserção por recorte). Não há
chave estrangeira declarada no código; a existência de uma no banco é
**`INFERIDO — confirmar`**.

### 2.4 Tabelas de perfil (somente leitura)

Exercitadas apenas por `obter_chaves_pesquisa` (`545–563`).

| Tabela | Colunas | Filtro aplicado |
|---|---|---|
| `recorte.tb_perfil_variacao` | `id_perfil`, `expressao_nm` | `expressao_nm IS NOT NULL` |
| `recorte.tb_perfil` | `id_perfil`, `id_cliente` | — |
| `recorte.tb_perfil_caderno` | `id_perfil`, `id_cadernos`, `status` | `status = 'S'` |
| `recorte.tb_cliente` | `id_cliente`, `status` | `status = 'A'` |

`id_perfil` é lido como `i64` (`main.rs:540`); `expressao_nm` como `String` não
anulável (`541`), o que é consistente com o filtro `IS NOT NULL`.

### 2.5 Consultas literais

As cinco consultas são normativas e devem ser transcritas **caractere a
caractere** na fase F4. Localização:

| Consulta | Linhas | Função |
|---|---|---|
| INSERT importação | 448–462 | `registrar_pdf` |
| SELECT chaves de pesquisa | 545–563 | `obter_chaves_pesquisa` |
| INSERT recorte | 574–585 | `salvar_recorte` |
| INSERT texto do recorte | 587–595 | `salvar_recorte` |
| UPDATE status | 664–671 | `atualizar_status_importacao` |
| UPDATE início | 683–690 | `registrar_inicio_importacao` |
| UPDATE término | 701–709 | `registrar_termino_importacao` |

A cláusula `ORDER BY tp.id_perfil, tpv.expressao_nm` (`560–562`) **governa a
deduplicação** descrita em §5.3. Não pode ser alterada, reordenada nem
"otimizada".

**Ordenação sensível à configuração regional:** o resultado de
`ORDER BY tpv.expressao_nm` depende do `COLLATE` do banco. Para expressões que
diferem apenas por acento, maiúsculas ou pontuação, uma configuração diferente
muda a ordem — e portanto muda **qual expressão fica registrada** em
`tb_recorte.expressao_busca`. Ver `DECISOES-ABERTAS.md`, D-02.

---

## 3. Máquina de estados da importação

### 3.1 Valores

| Valor | Nome | Gravado em | Significado |
|---|---|---|---|
| `-1` | Erro | 314, 320, 329, 334 | Falha terminal, causa não distinguida |
| `0` | Recebido | 470 (INSERT), 249 (UPDATE) | Registro criado |
| `1` | Selecionado | 261 | Tarefa de fundo iniciada |
| `2` | Indexando | 268 | Antes da extração do PDF |
| `3` | Recortando | 272 | Índice pronto |
| `4` | *reservado* | — | **Nunca gravado nem lido** |
| `5` | Finalizado | 325 | Laço concluído sem erro |

O comentário em `main.rs:652–663` documenta os valores; o enum planejado em
`633–648` está comentado e nunca foi implementado.

### 3.2 Escrita dupla de `status = 0`

O INSERT grava `0` via `DiarioPDF::default()` (`main.rs:439`), e o UPDATE em
`249` grava `0` de novo. **`DEFEITO PRESERVADO`** — duas escritas, mesmo valor.
Uma auditoria que conte comandos observa duas operações.

### 3.3 Transições

```
                    ┌──────────────────────────────────────┐
                    ▼                                      │
  (inexistente) ──► 0 ──► 1 ──► 2 ──► 3 ──► 5         (0 grava duas vezes)
                          │      │      │
                          │      ▼      ▼
                          └────► -1 ◄───┘
```

| De | Para | Gatilho | Linha |
|---|---|---|---|
| — | `0` | INSERT bem-sucedido | 464–474 |
| `0` | `0` | UPDATE redundante | 249 |
| `0` | `1` | Início da tarefa | 261 |
| `1` | `2` | Antes de `criar_indice` | 268 |
| `2` | `3` | `criar_indice` retornou `Ok` | 272 |
| `2` | `-1` | `criar_indice` retornou `Err` | 334 |
| `3` | `-1` | `obter_chaves_pesquisa` falhou | 329 |
| `3` | `-1` | `recortar` falhou | 320 |
| `3` | `-1` | `salvar_recorte` falhou | 314 |
| `3` | `5` | Laço concluído | 325 |

Nenhuma transição sai de `-1` ou de `5`. Ambos são terminais.

### 3.4 Ordem exata das escritas na tarefa de fundo

```
registrar_inicio_importacao(id)      →  data_inicio = current_timestamp   (260)
atualizar_status_importacao(id, 1)                                        (261)
atualizar_status_importacao(id, 2)                                        (268)
[criar_indice]                                                            (270)
atualizar_status_importacao(id, 3)                                        (272)
[obter_chaves_pesquisa]                                                   (274)
[laço de chaves — grava recortes]                                         (282–323)
atualizar_status_importacao(id, 5)                                        (325)
registrar_termino_importacao(id, n)  →  data_fim, total_recortes          (326)
```

`data_inicio` é gravado **antes** de `status = 1`. A ordem importa para qualquer
consumidor que observe as duas colunas.

### 3.5 Estados presos — sem recuperação

Se o processo morrer entre `1` e `5`, a linha permanece indefinidamente em `1`,
`2` ou `3`. Não existe varredura, reprocessamento ou expiração. **`DEFEITO
PRESERVADO`**; a correção é a chave `VARREDURA_ORFAS` na fase F11.

### 3.6 Resultados de gravação de status são descartados

Todas as chamadas usam `let _ = ...` (`249`, `260`, `261`, `268`, `272`, `314`,
`320`, `325`, `326`, `329`, `334`). Uma falha ao gravar o status é
**silenciosamente ignorada** e o fluxo continua. Em particular, a falha ao
gravar `-1` não é registrada em log nem propagada.

---

## 4. Pipeline de normalização de texto

### 4.1 Sequência canônica

Executada em `criar_indice` (`main.rs:479–536`).

```
bytes do PDF
  │
  ├─ 1. mupdf::Document::from_bytes(conteudo, "pdf")                   (482)
  │
  └─ PARA CADA página do documento:                                    (489)
       │
       ├─ 2. page.to_text_page(TextPageOptions::empty())               (490)
       │
       ├─ PARA CADA bloco:                                             (492)
       │    └─ PARA CADA linha:                                        (493)
       │         ├─ 3. caracteres → String, irrecuperável vira ' '     (495–496)
       │         ├─ 4. .trim()                                         (497)
       │         ├─ 5. + "\n"                                          (494)
       │         ├─ 6. junção de hífens                                (498–500)
       │         ├─ 7. remoção de diacríticos                          (501)
       │         └─ 8. acumula na lista de linhas da página            (502)
       │
       └─ 9. página = linhas.join("")   ← sem separador                (505)
```

A ordem **6 → 7** é significativa. Ver §4.3.

### 4.2 Detalhes normativos de cada passo

**Passo 3 — caracteres.** `.map(|c| c.char().unwrap_or(' '))` (`main.rs:496`).
Caractere que o MuPDF não converte vira **espaço** `U+0020`, não é descartado.
Espaço é separador de termos: descartar colaria termos vizinhos e criaria
correspondências inexistentes.

**Passo 4 — aparo.** `str::trim` do Rust remove todo caractere com
`char::is_whitespace`, o que inclui `U+00A0` (espaço inquebrável) e
`U+3000`, mas **não** inclui `U+200B` (espaço de largura zero).

**Passo 5 — quebra.** `format!("{}\n", ...)` (`main.rs:494`): exatamente um
`\n` por linha, acrescentado **depois** do aparo.

**Passo 6 — junção de hífens.** Padrão `(?imx)(\w+)(-\n)`, substituição `$1`
(`main.rs:487`, `498–500`).

- Flag `i`: irrelevante para este padrão.
- Flag `m`: irrelevante — não há `^` nem `$`.
- Flag `x`: neste padrão específico não há espaço literal, então não tem efeito.
  A sequência `\n` é o **escape** de nova linha e sobrevive ao modo extended.
- `\w` no *crate* `regex` é **Unicode**: casa `ç`, `á`, `ã`, `ê`. Neste ponto
  do pipeline o texto **ainda está acentuado**, porque a remoção de diacríticos
  é o passo 7.

**Passo 7 — diacríticos.** `diacritics::remove_diacritics` (`main.rs:501`).
Usa tabela própria; não é equivalente a decomposição canônica seguida de
remoção de marcas. Ver `INVARIANTES.md`, INV-P07.

**Passo 9 — montagem.** `pagina.join("")` (`main.rs:505`): sem separador entre
blocos. A página final tem exatamente uma quebra por linha de texto e **nenhuma
fronteira de bloco**.

### 4.3 Equivalência entre aplicação por linha e por página

O legado aplica os passos 6 e 7 **por linha** (`main.rs:498–501`), antes da
concatenação em `505`. Aplicá-los à página já concatenada produz **resultado
idêntico**. Demonstração:

Sejam `L₁ … Lₙ` as linhas, cada uma terminada em `\n`; `H` a junção de hífens;
`D` a remoção de diacríticos; `∘` a concatenação.

1. `D` é um mapeamento caractere a caractere, logo distribui sobre a
   concatenação: `D(x) ∘ D(y) = D(x ∘ y)`.
2. `H` substitui ocorrências de `(\w+)(-\n)`. Como `\n` não é caractere de
   palavra, `\w+` não atravessa fronteira de linha. Logo toda correspondência
   está inteiramente contida em uma linha, e o conjunto de correspondências em
   `L₁ ∘ … ∘ Lₙ` é a união das correspondências em cada `Lᵢ`.

Portanto `D(H(L₁)) ∘ … ∘ D(H(Lₙ)) = D(H(L₁ ∘ … ∘ Lₙ))`.

**Consequência prática:** a reescrita pode aplicar a normalização por página, o
que é mais simples e mais rápido. A escolha deve ser registrada em `CONTEXT.md`
e o teste de paridade cobre as duas formas.

**Efeito da junção:** a linha `"conti-\n"` vira `"conti"` — **sem** a quebra.
A linha seguinte `"nuacao\n"` é anexada logo após pela concatenação, resultando
em `"continuacao\n"`. A junção acontece pela remoção da quebra, não por uma
operação de emenda explícita.

### 4.4 Indexação

Após a normalização (`main.rs:510–531`):

| Item | Valor | Linha |
|---|---|---|
| Campo `page` | `u64`, `STORED` — armazenado, **não indexado** | 512 |
| Campo `text` | `TEXT \| STORED` — indexado com posições e armazenado | 513 |
| Índice | `Index::create_in_ram` | 517 |
| Orçamento do escritor | `500_000_000` bytes | 519 |
| Documentos | um por página | 524–529 |
| Numeração | `(i + 1) as u64` — **começa em 1** | 526 |

O analisador aplicado a `TEXT` é o `default` do Tantivy, e o mesmo analisador é
aplicado à consulta pelo `QueryParser`. Sua composição exata é normativa e está
em `INVARIANTES.md`, INV-P03.

---

## 5. Semântica do laço de recorte

### 5.1 Estrutura

`main.rs:282–323`. Pseudocódigo normativo:

```
nr_recortes  := 0
pages        := conjunto vazio      // main.rs:280
id_perfil    := 0                   // main.rs:281  ← ver INV-P13

PARA CADA chave EM chaves:                                    (282)
    recortes := recortar(idx, chave.expressao_nm)             (283)
    SE erro: status ← -1; ENCERRA a importação                (319–321)

    ordenar recortes por página, ESTÁVEL                      (285)

    SE id_perfil ≠ chave.id_perfil:                           (287)
        pages     := conjunto vazio                           (288)
        id_perfil := chave.id_perfil                          (289)

    filtrados := lista vazia                                  (296)
    PARA CADA recorte EM recortes:                            (298)
        SE recorte.page ∉ pages:                              (300)
            filtrados += recorte                              (301)
        pages += recorte.page        ← insere SEMPRE          (303)

    nr_recortes += tamanho(filtrados)                         (306)

    SE filtrados não vazio:                                   (308)
        salvar_recorte(id, chave, filtrados)                  (310)
        SE erro: status ← -1; ENCERRA a importação            (313–315)

status ← 5                                                    (325)
registrar_termino_importacao(id, nr_recortes)                 (326)
```

### 5.2 Ordenação

`recortes.sort_by(|a, b| a.page.cmp(&b.page))` (`main.rs:285`).
`sort_by` do Rust é **estável**. Como há no máximo um documento por página, a
estabilidade não é observável aqui — mas a reescrita deve usar ordenação
estável mesmo assim, para não depender dessa coincidência.

### 5.3 Deduplicação — por perfil, atravessando expressões

O conjunto `pages` é reiniciado **apenas quando o identificador de perfil muda**
(`main.rs:287–290`). Dentro de um mesmo perfil, ele acumula ao longo de **todas
as expressões processadas até ali**.

Consequência normativa: **a primeira expressão de um perfil a encontrar uma
página consome aquela página.** As expressões seguintes do mesmo perfil não
geram recorte para ela.

Como as chaves chegam ordenadas por `(id_perfil, expressao_nm)` (`main.rs:560–562`),
"primeira" significa **alfabeticamente anterior** segundo o `COLLATE` do banco.
Portanto o valor gravado em `tb_recorte.expressao_busca` para uma página
disputada é determinado pela ordenação do banco.

Perfis diferentes não interferem entre si: a mesma página gera um recorte para
cada perfil.

**Inserção incondicional:** `pages.insert(pagina)` em `main.rs:303` executa
**fora** do `if`, para todo recorte, inclusive os descartados. Como o descarte
só ocorre quando a página já está no conjunto, a reinserção é idempotente e não
altera o resultado. Registrado por fidelidade.

### 5.4 Conteúdo do recorte

`recortar` (`main.rs:400–406`) constrói:

| Campo | Origem | Persistido? |
|---|---|---|
| `page` | campo `page` do documento (`402`) | sim, em `nr_pagina` |
| `text` | campo `text` do documento (`403`) | **não — dado morto** |
| `highlight` | mesmo campo `text`, lido em `389` | sim, em `tb_recorte_texto.recorte` |

`text` e `highlight` recebem o **mesmo valor**. Apenas `highlight` é gravado
(`main.rs:619`). O campo `text` não tem efeito observável e pode ser omitido na
reescrita **sem alteração de comportamento** — ver `MAPA-DE-CHAMADAS.md`, §4.1.

O valor gravado é o **texto integral da página**, normalizado e sem acentos —
não um trecho ao redor da ocorrência.

### 5.5 Consulta e filtro pelo operador `&`

**Consulta** (`main.rs:375`): `format!(r#""{key}""#)` — a expressão é envolvida
em aspas duplas, produzindo uma **busca de frase** com distância zero.

Uma expressão contendo `"` produz consulta sintaticamente inválida ⇒
`parse_query` devolve `Err` ⇒ a importação inteira vai a −1. Ver
`INVARIANTES.md`, INV-P17.

**Filtro adicional** (`main.rs:392–398`), aplicado **depois** da busca, apenas
quando a expressão contém `&`:

```rust
if key.contains('&') {
    let exp = format!("(?imx){}", key.replace('&', r"\s*&\s*"));
    let re = regex::Regex::new(&exp).unwrap();
    if !re.is_match(&texto) { continue; }
}
```

A flag `x` faz o motor **ignorar todo espaço literal do padrão**. Para a chave
`ACME & FILHOS`, o padrão efetivo é `ACME\s*&\s*FILHOS`. Ver `INVARIANTES.md`,
INV-P01 — esta é a divergência de maior impacto da migração.

A expressão regular é compilada **dentro do laço de documentos**
(`main.rs:394`), uma vez por acerto. `.unwrap()`: um padrão inválido derruba a
tarefa por pânico. Ver `DECISOES-ABERTAS.md`, D-06.

### 5.6 Interrupção deixa trabalho parcial gravado

Falha em `recortar` ou em `salvar_recorte` grava −1 e **retorna**, abandonando
as chaves restantes. Tudo que já foi gravado **permanece**. Não há compensação
nem reversão. **`DEFEITO PRESERVADO`**.

Falha em `obter_chaves_pesquisa` grava −1 e encerra sem gravar recorte algum
(`main.rs:327–331`).

### 5.7 Contagem de recortes

`nr_recortes` soma o tamanho de `filtrados` **antes** da gravação
(`main.rs:306`), e a gravação só ocorre se a lista não estiver vazia (`308`).
Como uma falha de gravação encerra a importação, o valor só chega a
`registrar_termino_importacao` quando todas as gravações tiveram sucesso —
então a contagem corresponde ao efetivamente gravado.

`registrar_termino_importacao` converte com `i32::try_from` (`main.rs:712`):
acima de `2 147 483 647` a conversão **falha**, a função devolve `Err`, e o
resultado é descartado por `let _ =` (`326`) — o status permanece `5` e
`data_fim`/`total_recortes` **não são gravados**. **`DEFEITO PRESERVADO`**.

---

## 6. Ciclo de vida do processo

### 6.1 Arranque

| Ordem | Ação | Linha | Falha |
|---|---|---|---|
| 1 | `dotenv().ok()` | 32 | silenciosa |
| 2 | `tracing_subscriber::fmt().init()` | 34 | pânico se já inicializado |
| 3 | Lê `DATABASE_URL` | 36 | **pânico**: `Variável ambiental DATABASE_URL` |
| 4 | Conecta ao banco | 38, 85 | **pânico** (`unwrap`) |
| 5 | Define `DB_POOL` | 39 | **pânico** se já definido |
| 6 | Cria o contador de tarefas | 42 | — |
| 7 | Lê `SERVIDOR_IP` e `SERVIDOR_PORTA` | 44–48 | nunca falha |
| 8 | Monta rotas | 52–56 | — |
| 9 | `TcpListener::bind` | 58 | **pânico** se a porta estiver ocupada |
| 10 | Dispara o ouvinte de sinais | 64 | — |
| 11 | Registra `Servidor iniciando em: {servidor}` | 66 | — |
| 12 | Dispara o servidor e aguarda | 68–77 | pânico do servidor propaga ao `main` |

A conexão ao banco acontece **antes** de escutar na porta: com o banco
indisponível, o processo não sobe.

### 6.2 Sinais

`listen_shutdown_signal` (`main.rs:88–110`) aguarda, em `select!`:

| Sinal | Saída em *stdout* | Linha |
|---|---|---|
| `SIGINT` (Ctrl-C) | `>>>>> <CTRL>-C recebido` | 104 |
| `SIGTERM` | `>>>>> Kill recebido` | 105 |

Usa `println!`, **não** o subsistema de registro — a mensagem não sai
estruturada nem passa pelo `tracing`.

Em seguida: `handle.stop_graceful(None)` (`main.rs:109`). O argumento `None`
significa **sem teto de tempo**.

### 6.3 Drenagem

```rust
server.serve(service).await;                       // main.rs:69
while tarefas.load(Ordering::SeqCst) > 0 {         // main.rs:71
    tracing::info!("Aguardando tarefas encerrarem.");
    sleep(Duration::from_millis(100)).await;       // main.rs:73
}
```

1. `stop_graceful` faz o servidor parar de aceitar conexões e aguardar as
   requisições em curso.
2. `serve` retorna.
3. O laço espera o contador de importações em andamento chegar a zero,
   **consultando a cada 100 ms** e registrando `Aguardando tarefas encerrarem.`
   a cada volta.
4. `main` aguarda a tarefa (`main.rs:76–77`) e encerra.

**Sem teto de tempo em nenhuma das etapas.** Uma importação travada segura o
processo indefinidamente. **`DEFEITO PRESERVADO`**; a chave `SHUTDOWN_TIMEOUT`
da fase F11 tem padrão `0`, que reproduz a espera indefinida.

O pool de conexões **não é fechado explicitamente**. O `scopeguard::defer` de
`main.rs:263` só decrementa o contador e registra.

### 6.4 Contador de tarefas

| Operação | Linha | Momento |
|---|---|---|
| `fetch_add(1, SeqCst)` | 252 | após o INSERT, antes de disparar a tarefa |
| `fetch_sub(1, SeqCst)` | 265 | em `defer`, ao término da tarefa |

O `defer` (`main.rs:263–266`) garante o decremento inclusive em caso de pânico
dentro da tarefa. O registro `Processo de recorte FINALIZADO` sai junto.

Um pânico dentro da tarefa de fundo **não derruba o processo** — o `tokio`
o captura na `JoinHandle`, que é descartada. A importação fica presa no último
status gravado. Ver §3.5.

---

## 7. Evoluções atrás de chave — o que NÃO é contrato do legado

Tudo o que esta especificação descreve das seções 1 a 6 é o comportamento do
serviço original, reproduzido pelo porte em Go. Esta seção existe para marcar a
fronteira: o que vem a seguir **não** está no legado, e **não** acontece com a
configuração no padrão.

### 7.1 A regra

> Toda evolução tem uma chave de configuração cujo valor padrão reproduz
> exatamente o legado. **Nenhuma liga por padrão.**

Portanto: uma instância com o ambiente vazio — a não ser pelos dois segredos
obrigatórios — se comporta como as seções 1 a 6 descrevem, byte a byte. É o que
a suíte de paridade verifica.

### 7.2 O que cada chave acrescenta

| Chave | O que passa a existir |
|---|---|
| `MAX_UPLOAD_BYTES` | Resposta **400** com a crítica `PDF excede o tamanho máximo` (D-16, opção B). |
| `VALIDAR_ASSINATURA_PDF` | Recusa síncrona de arquivo sem o prefixo `%PDF-`, com **a mesma 422 e o mesmo texto** de §1.4.4 — nenhum literal novo. |
| `RESPOSTA_PROBLEM_JSON` | Respostas de **erro** (≥ 400) em `application/problem+json`. As de sucesso permanecem em `text/plain`. |
| `RATE_LIMIT_RPS` | Resposta **429** com `Retry-After`, e o literal `Limite de requisições excedido`. |
| `STATUS_ENDPOINT` | Rota `GET /importacao/{id}`, em JSON. Adição pura. |
| `HEALTH_ENDPOINTS` | Rotas `/health/live` e `/health/ready`. **`/ping` não muda** — §1.3 continua valendo integralmente. |
| `MAX_IMPORTACOES_CONCORRENTES` | Fila de submissões acima do teto. **A resposta HTTP não muda**; muda a latência. |
| `GRAVACAO_EM_LOTE` | Dois comandos por chave de pesquisa em vez de dois por recorte. **O estado do banco é idêntico** (§5), incluindo `dt_recorte`. |
| `IDEMPOTENCIA_POR_HASH` | Reenvio de documento **finalizado** devolve o id existente, com **a mesma resposta 200** de §1.4.3, sem registrar importação nova. |
| `VARREDURA_ORFAS` | Tarefa periódica sobre importações paradas em 1–3 (§3.5). Com a política padrão, apenas registra e conta. |

### 7.3 Os três literais que só existem com chave ligada

Nenhum deles aparece em `reference/main.rs`; todos são inalcançáveis com a
configuração no padrão.

| Literal | Chave | Código |
|---|---|---|
| `PDF excede o tamanho máximo` | `MAX_UPLOAD_BYTES` | 400 |
| `Limite de requisições excedido` | `RATE_LIMIT_RPS` | 429 |
| `Importação não encontrada`, `Identificador inválido`, `Erro ao consultar a importação` | `STATUS_ENDPOINT` | 404, 400, 500 |
| `vivo`, `pronto`, `indisponível` | `HEALTH_ENDPOINTS` | 200, 200, 503 |

### 7.4 O que NÃO existe, e por quê

**Reprocessamento automático de importação presa.** A varredura não pode
reprocessar porque o serviço **não guarda o documento**: `tb_importacao` tem o
nome original e o resumo SHA-256, e os bytes do PDF são descartados com a
tarefa. Ver `docs/DECISOES-ABERTAS.md`, **D-21**.

Consequência que vale além da varredura: **toda importação que falha é perda
definitiva de trabalho**, para qualquer causa. A única recuperação é o cliente
reenviar o documento.

### 7.5 Onde está o resto

Valores recomendados, ordem de ativação e o cálculo de memória do teto de
concorrência estão em `docs/OPERACAO.md`. O contrato em formato legível por
máquina, com as adições marcadas, está em `api/openapi.yaml`.
