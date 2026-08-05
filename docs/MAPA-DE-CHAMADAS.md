# Mapa de chamadas — `reference/main.rs`

> Artefato do passo 1 do procedimento da fase **F0**. Precede a especificação.
> Referência normativa: `reference/main.rs`, SHA-256
> `4c5a7b4806a4e42cc80bef7f8ff4e63544f1b66482a0fc6353415bef828b9580`, 730 linhas.
> Toda referência tem a forma `main.rs:LINHA`.

---

## 1. Inventário de itens de topo

| Item | Linhas | Tipo | Observação |
|---|---|---|---|
| `SERVIDOR_IP_PADRAO` | 24 | constante | `"192.168.42.1"` |
| `SERVIDOR_PORTA_PADRAO` | 25 | constante | `6001` |
| `API_KEY` | 26 | constante | **Segredo embutido no código** |
| `DB_POOL` | 28 | estado global | `OnceLock<PgPool>` |
| `main` | 30–78 | função | Ponto de entrada |
| `db_pool` | 80–82 | função | Acesso ao estado global |
| `make_db_pool` | 84–86 | função | Construção do pool |
| `listen_shutdown_signal` | 88–110 | função | Sinais |
| `ping` | 112–115 | manipulador | |
| `autenticar` | 117–130 | manipulador | Middleware (*hoop*) |
| `upload_pdf` | 132–341 | manipulador | 210 linhas |
| `calcular_hash` | 342–346 | função | |
| `ResultadoBusca` | 348–352 | struct | **Código morto** — nunca construída nem referenciada |
| `Recorte` | 354–359 | struct | |
| `recortar` | 361–410 | função | |
| `DiarioPDF` | 412–430 | struct | |
| `impl Default for DiarioPDF` | 432–445 | impl | Fonte dos padrões implícitos |
| `registrar_pdf` | 447–477 | função | SQL |
| `criar_indice` | 479–536 | função | |
| `ChavePesquisa` | 538–542 | struct | |
| `obter_chaves_pesquisa` | 544–571 | função | SQL |
| `salvar_recorte` | 573–631 | função | SQL |
| `StatusImportacao` | 633–648 | comentário | Enum planejado, nunca implementado |
| `atualizar_status_importacao` | 651–680 | função | SQL |
| `registrar_inicio_importacao` | 682–698 | função | SQL |
| `registrar_termino_importacao` | 700–718 | função | SQL |
| `notificar_por_telegram` | 720–728 | comentário | Nunca implementado; não compila |

---

## 2. Grafo de chamadas

```
main (30)
├── make_db_pool (38) ──────────► Pool::connect                    [E/S: banco]
├── DB_POOL.set (39)                                               [escreve estado global]
├── std::env::var × 3 (36,44,45)                                   [lê ambiente]
├── tokio::spawn ─► listen_shutdown_signal (64)
│                   └── handle.stop_graceful (109)
└── tokio::spawn ─► server.serve (69)
                    ├── [rota GET /ping]  ─► ping (52)
                    └── [rota POST /pdf]  ─► autenticar (54)
                                          ─► affix_state::inject(tarefas) (55)
                                          ─► upload_pdf (56)

upload_pdf (132)   ── CAMINHO SÍNCRONO, antes da resposta 200 ──
├── req.header("x-request-id") (134)
├── req.form × 4 (138,139,141,142)
├── req.file("pdf") (143)
├── chrono::NaiveDate::parse_from_str × 2 (152,165)
├── tokio::fs::read (233) ──────────────────────► .unwrap()        [E/S: disco; PÂNICO possível]
├── calcular_hash (234)
├── registrar_pdf (237) ────────────────────────► db_pool (473)    [escreve tb_importacao]
├── atualizar_status_importacao(id, 0) (249) ───► db_pool (676)    [escreve tb_importacao]
├── depot.obtain::<Arc<AtomicUsize>>() (251) ───► .unwrap()        [PÂNICO possível]
├── tarefas.fetch_add(1) (252)
└── tokio::task::spawn (257) ── CAMINHO ASSÍNCRONO, após a resposta ──
    ├── registrar_inicio_importacao (260) ──────► db_pool (694)    [escreve data_inicio]
    ├── atualizar_status_importacao(id, 1) (261)
    ├── scopeguard::defer (263) ─► tarefas.fetch_sub(1) (265)
    ├── atualizar_status_importacao(id, 2) (268)
    ├── criar_indice (270)
    │   ├── mupdf::Document::from_bytes (482)                      [E/S: nenhuma; CPU/memória]
    │   ├── regex::Regex::new (487) ────────────► .expect()
    │   ├── documento.pages() (489)
    │   ├── page.to_text_page (490)
    │   ├── [blocos → linhas → caracteres] (492–503)
    │   ├── re.replace_all (498)                                   ⚠ aplicado POR LINHA
    │   ├── diacritics::remove_diacritics (501)                    ⚠ aplicado POR LINHA
    │   ├── Schema::builder / build (510–515)
    │   ├── Index::create_in_ram (517)
    │   ├── idx.writer(500_000_000) (519)                          ⚠ 500 MB por importação
    │   └── idx_writer.commit (531)
    ├── atualizar_status_importacao(id, 3) (272)
    ├── obter_chaves_pesquisa (274) ────────────► db_pool (567)    [lê 4 tabelas de perfil]
    └── PARA CADA chave (282):
        ├── recortar (283)
        │   ├── Schema::builder / build (365–370)                  ⚠ esquema RECONSTRUÍDO
        │   ├── QueryParser::for_index / parse_query (375)         ⚠ `?` aborta a importação
        │   ├── idx.reader_builder (377–380)
        │   ├── searcher.search(TopDocs::with_limit(1_000_000)) (384)
        │   └── PARA CADA acerto (387):
        │       ├── searcher.doc (388)
        │       ├── doc.get_first(...).unwrap().as_str().unwrap() (389)
        │       ├── regex::Regex::new (394) ────► .unwrap()        ⚠ dentro do laço
        │       └── push Recorte (400)
        ├── recortes.sort_by(page) (285)                           [estável]
        ├── [reinício do conjunto de páginas se mudou o perfil] (287–290)
        ├── [filtragem de páginas já vistas] (298–304)
        └── salvar_recorte (310) ───────────────► db_pool (606,620) [escreve tb_recorte + tb_recorte_texto]
    ├── atualizar_status_importacao(id, 5) (325)
    └── registrar_termino_importacao (326) ─────► db_pool (713)    [escreve data_fim, total_recortes]
```

---

## 3. Quem lê e quem escreve — por função

| Função | Lê | Escreve | E/S externa |
|---|---|---|---|
| `main` | `DATABASE_URL`, `SERVIDOR_IP`, `SERVIDOR_PORTA`, `.env` | `DB_POOL` | Conecta ao banco; escuta TCP |
| `db_pool` | `DB_POOL` | — | — |
| `make_db_pool` | — | — | Conexão ao PostgreSQL |
| `listen_shutdown_signal` | `SIGINT`, `SIGTERM` | *stdout* (`println!`) | Sinais do sistema |
| `ping` | — | — | — |
| `autenticar` | Cabeçalho `X-API-KEY`, constante `API_KEY` | Resposta HTTP | — |
| `upload_pdf` | Corpo *multipart*, cabeçalho `x-request-id`, `Depot` | Resposta HTTP; `tb_importacao`; contador de tarefas | Lê arquivo temporário do disco |
| `calcular_hash` | Bytes do PDF | — | — |
| `recortar` | Índice em memória | — | — |
| `registrar_pdf` | `DiarioPDF` | `recorte.tb_importacao` (INSERT) | Banco |
| `criar_indice` | Bytes do PDF | — | — |
| `obter_chaves_pesquisa` | `tb_perfil_variacao`, `tb_perfil`, `tb_perfil_caderno`, `tb_cliente`, `tb_importacao` | — | Banco |
| `salvar_recorte` | `ChavePesquisa`, `Vec<Recorte>` | `recorte.tb_recorte`, `recorte.tb_recorte_texto` (INSERT) | Banco |
| `atualizar_status_importacao` | — | `recorte.tb_importacao.status` (UPDATE) | Banco |
| `registrar_inicio_importacao` | — | `recorte.tb_importacao.data_inicio` (UPDATE) | Banco |
| `registrar_termino_importacao` | — | `recorte.tb_importacao.data_fim`, `.total_recortes` (UPDATE) | Banco |

---

## 4. Achados estruturais do mapeamento

Itens que a leitura do grafo revela e que a especificação precisa registrar.

### 4.1 `Recorte.text` é dado morto

`Recorte` (354–359) tem três campos. `page` e `highlight` são persistidos
(`main.rs:602`, `main.rs:619`). O campo `text` é **escrito e nunca lido**:
`salvar_recorte` grava `recorte.highlight` (619), não `recorte.text`.

Em `recortar` (400–406), `text` e `highlight` recebem o **mesmo valor**, obtido
duas vezes do mesmo campo do documento (389 e 403). Portanto o campo `text` não
tem efeito observável e pode ser omitido na reescrita **sem alterar
comportamento**. Isso não é uma evolução atrás de chave: é remoção de dado morto.

### 4.2 `ResultadoBusca` é código morto

Declarada em 348–352, nunca construída nem referenciada. Não faz parte do
comportamento e não será portada.

### 4.3 Os atributos `#[sqlx(rename = ...)]` de `DiarioPDF` são decorativos

`DiarioPDF` deriva `sqlx::FromRow` (412), mas a struct **nunca é lida do banco** —
só alimenta os `bind` de um INSERT cujos nomes de coluna estão escritos
literalmente no SQL (450–458). Os `rename` são documentação, não mapeamento
efetivo. A correspondência campo→coluna que vale é a da ordem dos `bind`
(465–472) contra a ordem das colunas no INSERT (450–458).

### 4.4 O caminho de erro em 226–230 é inalcançável

Se `arquivo` for `None`, a crítica `"PDF não enviado"` é acrescentada em 211, e
o retorno antecipado de 218–224 dispara antes. Logo 226–230 nunca executa.
A mensagem em 228 usa `res.render("...{id_requisicao}...")` com uma **string
literal sem interpolação** — as chaves apareceriam cruas na resposta se o ramo
fosse alcançável. Ver `DECISOES-ABERTAS.md`, item D-07.

### 4.5 A junção de hífens é aplicada por linha, não por página

`main.rs:498` executa `replace_all` sobre `chars`, que naquele ponto contém
**uma única linha** já terminada em `\n` (494–497). A remoção de diacríticos
(501) também é por linha. A concatenação da página só acontece em 505,
com `join("")`.

O efeito de juntar as duas metades da palavra vem da concatenação: a linha
`"conti-\n"` vira `"conti"` (sem quebra), e a linha seguinte `"nuacao\n"` é
anexada logo após, produzindo `"continuacao\n"`.

**Equivalência com a aplicação por página** (relevante para a reescrita): as
duas formas produzem resultado idêntico, porque (a) `remove_diacritics` é um
mapeamento por caractere e portanto distribui sobre a concatenação, e (b) o
padrão `(\w+)(-\n)` não pode casar através de uma fronteira de linha, já que
`\n` não é caractere de palavra e `\w+` não o atravessa. A demonstração está
registrada em `ESPECIFICACAO.md`, §4.3. A reescrita pode usar qualquer uma das
duas formas; a escolha fica registrada em `CONTEXT.md`.

### 4.6 Uma expressão de perfil com aspas aborta a importação inteira

`main.rs:375` monta a consulta como `format!(r#""{key}""#)`. Se `expressao_nm`
contiver o caractere `"`, a consulta resultante é sintaticamente inválida,
`parse_query` devolve `Err`, o `?` propaga, `recortar` devolve `Err`, e o ramo
de erro em 318–322 grava status −1 e **encerra a importação**, descartando
todas as chaves restantes. Uma única expressão malformada no cadastro derruba
o processamento inteiro daquele documento. Ver `INVARIANTES.md`, INV-P17.

### 4.7 O contador de tarefas não cobre a janela inteira

`tarefas.fetch_add(1)` ocorre em 252, **depois** do INSERT em 237 e do UPDATE em
249. Entre o início do manipulador e a linha 252 há E/S de banco e de disco que
não são contabilizadas na drenagem. Um encerramento iniciado nessa janela não
espera por elas — mas o `Server::stop_graceful` espera a *requisição HTTP*
terminar, e essas operações estão dentro da requisição. A janela realmente
descoberta é nula. Registrado por completude.

### 4.8 Duas escritas de `status = 0` em sequência

O INSERT em 447–477 grava `status` a partir de `DiarioPDF::default()`, que é
`Some(0)` (439). O UPDATE em 249 grava `0` novamente. Duas escritas, mesmo
valor. Preservado. Ver `ESPECIFICACAO.md`, §3.2.
