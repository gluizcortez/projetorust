# Serviço de recorte de diários oficiais — migração Rust → Go

Reescrita em Go do serviço de ingestão e recorte de diários oficiais, hoje em
Rust. **A regra dura do projeto é paridade comportamental total:** nenhuma
funcionalidade pode ser alterada ou removida, e toda evolução técnica entra
atrás de chave de configuração desligada por padrão.

## Como rodar

Nada precisa ser definido antes — nem chave, nem banco, nem endereço.

### Com Docker

```sh
make subir          # ou: docker compose up --build
curl localhost:6001/ping
```

Sobe o PostgreSQL, cria o esquema, semeia perfis de exemplo e sobe o serviço em
`http://localhost:6001`. Para derrubar: `make descer`.

### Sem Docker

Precisa de um PostgreSQL alcançável em
`postgres://recorte:recorte@localhost:5432/recorte`, com o esquema de
`db/init/` aplicado. Depois:

```sh
go run ./src/main
```

Com outro banco, defina `DATABASE_URL`.

### Submetendo um diário

```sh
curl -X POST http://localhost:6001/pdf \
  -H "X-API-KEY: 01956cb2-2f85-7440-9767-1a6651c10e0f" \
  -F "data-caderno=2024-03-15" \
  -F "data-disponibilizacao=2024-03-16" \
  -F "id-usuario=44521" \
  -F "id-caderno=1" \
  -F "pdf=@exemplos/diario-de-exemplo.pdf"
```

Resposta: `PDF carregado com sucesso`. O processamento é **assíncrono** — o 200
confirma o registro, não o resultado. O documento de exemplo casa com os perfis
semeados e produz um recorte.

### Se `/pdf` devolver 422 `Erro ao processar o PDF`

**O texto engana.** Ele é literal do serviço original (`main.rs:242`) e cobre
qualquer falha ao **registrar a importação no banco** — o PDF nem chega a ser
aberto. `/ping` continua respondendo `pong` porque não toca o banco, e
`/health/ready` também passa, porque só faz `Ping` na conexão.

A causa está no log, sempre:

```sh
docker compose logs recorte-api | grep "falha ao registrar"
```

As duas causas comuns, e o que fazer:

| No log | O que é | Correção |
|---|---|---|
| `relation "recorte.tb_importacao" does not exist` (42P01) | o esquema não foi criado | ver abaixo |
| `permission denied for schema recorte` (42501) | o esquema existe, o usuário da aplicação não tem acesso | `psql ... -f db/permissoes.sql` |

**Esquema ausente, com Docker.** Os scripts de `db/init/` rodam **só na criação
do volume**. Se o projeto já subiu antes, o volume sobreviveu e eles não
rodaram de novo — e `make descer` preserva o volume de propósito:

```sh
docker compose down -v      # o -v é o que apaga o volume
make subir
```

**Esquema ausente, sem Docker.** Aplique os três arquivos:

```sh
psql "postgres://recorte:recorte@localhost:5432/recorte" \
  -f db/init/01-esquema.sql \
  -f db/init/02-dados-de-exemplo.sql \
  -f db/init/03-perfis-do-dou-real.sql
```

**Permissão negada.** Acontece quando o esquema foi criado por um usuário
(`postgres`, pelo DBeaver) e a aplicação conecta por outro (`recorte`). No
PostgreSQL, criar um esquema não dá acesso a ele para os demais. Rode como
superusuário — o próprio arquivo explica o porquê de cada linha:

```sh
psql "postgres://postgres@localhost:5432/recorte" -f db/permissoes.sql
```

Não precisa reiniciar a aplicação: a requisição seguinte já passa.

## Validando com um diário real

`exemplos/diario-de-exemplo.pdf` é sintético: foi escrito para casar. A pergunta
que interessa é outra — **o serviço funciona sobre um diário de verdade?**

`exemplos/dou-secao1-2026-07-08.pdf` é um Diário Oficial da União real (Seção 1,
nº 126, 8 de julho de 2026, página 177 — deliberações do MPT). Uma página,
17.307 caracteres depois da normalização. `db/init/03-perfis-do-dou-real.sql`
semeia oito perfis casados com ele, **um por expressão**, para que o resultado
seja legível: uma linha por expressão.

```sh
curl -X POST http://localhost:6001/pdf \
  -H "X-API-KEY: 01956cb2-2f85-7440-9767-1a6651c10e0f" \
  -F "data-caderno=2026-07-08" \
  -F "data-disponibilizacao=2026-07-08" \
  -F "id-usuario=44521" \
  -F "id-caderno=1" \
  -F "pdf=@exemplos/dou-secao1-2026-07-08.pdf"
```

Espere um segundo e consulte — no DBeaver, ou por linha de comando:

```sql
SELECT v.id_perfil,
       v.expressao_nm,
       COALESCE('CASOU pág. ' || r.nr_pagina, 'não casou') AS resultado
FROM recorte.tb_perfil_variacao v
LEFT JOIN recorte.tb_recorte r ON r.id_perfil = v.id_perfil
WHERE v.id_perfil BETWEEN 301 AND 308
ORDER BY v.id_perfil;
```

O resultado é **exatamente** este — foi medido, não previsto:

| Perfil | Expressão | Resultado | O que isso prova |
|---|---|---|---|
| 301 | `BR BPO TECNOLOGIA E SERVICOS` | CASOU | frase de 4 termos |
| 302 | `CASAMAX COMERCIAL E SERVICOS LTDA` | CASOU | frase de 5 termos |
| 303 | `LEI GERAL DE PROTECAO DE DADOS` | CASOU | no PDF está `Lei Geral de Proteção de Dados` — **com acento e em caixa mista** |
| 304 | `DEBORAH DA SILVA FELIX` | CASOU | no PDF, `Dra. Deborah da Silva Felix` |
| 305 | `HOMOLOGACOES DE ARQUIVAMENTO` | CASOU | no PDF, `HOMOLOGAÇÕES DE ARQUIVAMENTO` |
| 306 | `EMPRESA BRASILEIRA DE CORREIOS E TELEGRAFOS` | não casou | ver abaixo |
| 307 | `LEI GERAL DE PROTEÇÃO DE DADOS` | não casou | **a mesma do 303, com acento** |
| 308 | `PREFEITURA MUNICIPAL DE SAO PAULO` | não casou | controle negativo |

E a importação:

```sql
SELECT id_importacao, status, total_recortes, nome_original_pdf
FROM recorte.tb_importacao;
--  1 | 5 | 5 | dou-secao1-2026-07-08.pdf
```

`status = 5` é *finalizado*. Com `STATUS_ENDPOINT=true`, o mesmo dado sai por
`GET /importacao/1` com a chave de API.

### As três linhas que não casaram valem mais que as cinco que casaram

**306 — o serviço está certo, o PDF é que é assim.** A extração devolve
`EMPRESA BRASILEIRA DE CORREIOS E TELEG R A FO S`: o texto foi posicionado com
espaço entre as letras no documento original, e vira cinco termos onde a
expressão espera um. Nenhum motor de busca por frase casaria — o legado em Rust
também não casa.

**307 — é DEFEITO PRESERVADO, e o par 303/307 é a demonstração viva.** A mesma
expressão, uma sem acento e outra com. O texto indexado passa pela remoção de
diacríticos; **a expressão cadastrada não passa.** Então expressão acentuada é
**inerte**: nunca casa com nada. Isso é comportamento do serviço original
(`INVARIANTES.md`, INV-P19) e foi reproduzido de propósito. Corrigir mudaria o
que os clientes recebem hoje, e a regra do projeto é preservar.

Se você cadastrar uma expressão e ela não casar, **o acento é a primeira coisa
a conferir.**

> **A chave de API está no repositório, em claro.**
>
> `01956cb2-2f85-7440-9767-1a6651c10e0f` é o mesmo valor que o serviço original
> em Rust trazia como constante no código-fonte. Ele é o padrão de
> `config.APIKeyPadrao` e está declarado em `docker-compose.yml`.
>
> Quem tem o repositório tem a credencial. **Para uma implantação real, defina
> `API_KEY` no ambiente** — a variável sempre vence o padrão.

## Onde está o quê

| Caminho | Conteúdo |
|---|---|
| `docs/roadmap-migracao-rust-go.html` | Documento de arquitetura: análise do legado, armadilhas de paridade, arquitetura alvo e as 14 fases com seus prompts |
| `docs/ESPECIFICACAO.md` | Especificação normativa do comportamento observável |
| `docs/INVARIANTES.md` | As 23 invariantes de paridade, com casos positivos e negativos |
| `docs/DECISOES-ABERTAS.md` | Perguntas pendentes, com padrão provisório e responsável |
| `docs/MAPA-DE-CHAMADAS.md` | Grafo de chamadas do legado e achados estruturais |
| `docs/CONTEXT.md` | Memória do projeto entre fases — decisões, desvios, pendências |
| `docs/OPERACAO.md` | Guia de operação: tabela de chaves, ordem de ativação, valores por porte de carga, métricas e alarmes |
| `docs/RELATORIO-PARIDADE.md` | Evidência da fase F12 e a recomendação explícita sobre o corte |
| `api/openapi.yaml` | Contrato da API, com as adições opcionais marcadas por `x-chave` |
| `src/main/` | Ponto de entrada |
| `src/` | O serviço: domínio, casos de uso, adaptadores e raiz de composição, um nível de pacotes |
| `db/init/` | Esquema e dois conjuntos de perfis de exemplo — o sintético e o do diário real |
| `db/permissoes.sql` | Os `GRANT` do esquema, para quando o dono não é o usuário da aplicação |
| `db/migrations/` | Migrações de banco |
| `deploy/Dockerfile`, `docker-compose.yml` | Empacotamento e execução local |
| `exemplos/` | Dois diários: um sintético, feito para casar, e um **Diário Oficial da União real** |

### O que saiu do repositório, e onde encontrar

A referência em Rust, o corpus dourado com seus oráculos, o ferramental de
captura e toda a aparelhagem de paridade (`test/parity`, `test/e2e`,
`test/carga`, `tools/comparador`, `tools/sombra`) **foram removidos da árvore de
trabalho** — eles não são necessários para o serviço rodar.

Continuam no histórico do git, no commit `2febb7a`:

```sh
git show 2febb7a --stat                        # o que existia
git show 2febb7a:reference/main.rs             # o serviço original
git checkout 2febb7a -- test tools reference   # traz tudo de volta
```

Os documentos citam `reference/main.rs:NNN` em centenas de lugares. Essas
citações continuam válidas — são âncoras para o arquivo naquele commit.

## Estado

| Fase | Descrição | Estado |
|---|---|---|
| F0 | Especificação executável e corpus dourado | **concluída** com ressalvas (D-11, D-15) |
| F1 | Fundação do repositório e cadeia de ferramentas | **concluída** — empacotamento não verificado (sem Docker no ambiente) |
| F2 | Configuração tipada e observabilidade | **concluída** |
| F3 | Núcleo de domínio e portas | **concluída** |
| F4 | Persistência | **concluída** |
| F5 | Extração de texto do PDF | **concluída** — 159/159 páginas idênticas |
| F6 | Normalização e tokenização | **concluída** — 1M casos sem divergência |
| F7 | Índice e busca | **concluída** — 1.378 combinações + 500 mil casos de propriedade |
| F8 | Pipeline e máquina de estados | **concluída** — sequência de chamadas fixada, 98,4% de cobertura |
| F9 | Camada HTTP | **concluída** — contrato byte a byte; D-07, D-08 e D-10 resolvidas por medição |
| F10 | Ciclo de vida e drenagem | **concluída** — sinais reais, drenagem por notificação, códigos de saída |
| F11 | Evoluções técnicas atrás de chaves | **concluída** — dez chaves, todas desligadas por padrão; D-21 aberta |
| F12 | Paridade e execução em sombra | **parcial** — verificação determinística completa e aprovada; sombra bloqueada por D-14. Recomendação: **não cortar** |
| F13 | Corte e descomissionamento | pendente |

Depois da F12 o repositório foi **enxugado**: ficou o que o serviço precisa para
rodar, mais toda a documentação. Ver a seção acima.

## Antes de continuar

Duas pendências bloqueantes devem ser solicitadas o quanto antes, porque o
prazo de resposta corre em paralelo às fases seguintes:

- **D-15** — `Cargo.toml` e `Cargo.lock` do serviço em produção.
- **D-11** — corpus de PDFs reais anonimizados e *dump* das tabelas de perfil.

Três achados são **defeitos de produto existentes**, não questões da migração, e
foram escalados: **D-17** (perfis com expressões acentuadas estão inertes),
**D-18** (PDFs truncados são registrados como processados com sucesso) e
**D-21** (o documento submetido não é arquivado, então nenhuma importação que
falha é recuperável).
