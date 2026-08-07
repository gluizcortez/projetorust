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
go run ./cmd/recorte-api
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
| `cmd/recorte-api/` | Ponto de entrada |
| `internal/` | O serviço: domínio, casos de uso, adaptadores e raiz de composição |
| `db/init/` | Esquema e perfis de exemplo para execução local |
| `db/migrations/` | Migrações de banco |
| `deploy/Dockerfile`, `docker-compose.yml` | Empacotamento e execução local |
| `exemplos/` | Um diário de exemplo, para o `curl` acima |

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
