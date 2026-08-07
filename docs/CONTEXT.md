# CONTEXT — memória do projeto entre fases

> Registro acumulado de decisões, desvios e itens adiados. Cada fase acrescenta
> sua seção ao final. Este arquivo é anexado ao prompt de toda fase seguinte.

---

## F0 — Especificação executável e corpus dourado

**Status:** concluída com ressalvas · **Data:** 2026-08-05

### Entregáveis

| Artefato | Caminho | Estado |
|---|---|---|
| Referência normativa vendorizada | `reference/main.rs` | completo |
| Mapa de chamadas | `docs/MAPA-DE-CHAMADAS.md` | completo |
| Especificação executável | `docs/ESPECIFICACAO.md` | completo, com marcações `INFERIDO` |
| Invariantes | `docs/INVARIANTES.md` | 20 invariantes |
| Decisões abertas | `docs/DECISOES-ABERTAS.md` | 18 itens |
| Ferramenta de captura | `tools/capturar-corpus/` | compila e executa |
| Gerador de corpus sintético | `tools/gerar-corpus-sintetico/` | 23 documentos |
| Corpus | `test/testdata/corpus/` | **sintético** — o real é pendência D-11 |
| Saídas capturadas | `test/testdata/expected/` | 4 arquivos por documento |

### Decisões tomadas

1. **`reference/main.rs` é vendorizado no repositório.** A fase F4 exige um
   teste que carregue as consultas SQL do arquivo original e as compare
   caractere a caractere com os arquivos `.sql` — o que só é possível com o
   arquivo versionado. SHA-256:
   `4c5a7b4806a4e42cc80bef7f8ff4e63544f1b66482a0fc6353415bef828b9580`.

2. **A ferramenta de captura não acessa o banco.** As chaves de pesquisa entram
   por um arquivo JSON que reproduz a saída de `obter_chaves_pesquisa`,
   preservando a ordem do `ORDER BY`. Isso torna a captura executável sem
   infraestrutura e permite gerar corpus sintético. A ordem do arquivo é
   normativa e a responsabilidade de produzi-la corretamente passa para quem
   exporta o *dump*.

3. **A captura grava o texto pré-normalização** (`*.paginas-brutas.json`) além
   do normalizado. A fase F5 precisa de um oráculo isolado da F6; sem isso, uma
   falha de extração e uma falha de normalização seriam indistinguíveis.

4. **O corpus sintético foi criado como ponte, não como substituto.** Cada
   documento é nomeado pela invariante que exercita, para que uma falha de
   paridade aponte direto para a regra violada. Ele **não** satisfaz os
   critérios de saída de F12 — ver D-11.

5. **Determinismo garantido nos dois lados.** O gerador usa
   `rl_config.invariant = 1` (suprime data de criação e identificador aleatório
   do PDF) e a captura ordena a listagem de arquivos e serializa JSON de forma
   estável. Ambos verificados por dupla execução com `diff -r` sem saída.

### Desvios em relação ao prompt da fase

| Prompt pedia | O que foi feito | Motivo |
|---|---|---|
| 30 PDFs reais anonimizados | 23 documentos sintéticos | PDFs de produção não estão disponíveis nesta sessão. D-11 permanece **BLOQUEANTE** para F12. |
| Corpus com *dump* de perfis reais | 15 chaves sintéticas | Idem. As chaves sintéticas cobrem as invariantes, não a variedade real. |
| Binário reutilizando as funções do `main.rs` | Porte verbatim em módulos separados | O `main.rs` é um binário único sem biblioteca exportável; extrair as funções para um *crate* exigiria alterar o legado, o que está fora de escopo. Os portes citam a linha de origem de cada trecho. |

### Achados novos, não previstos no roadmap

Três invariantes foram descobertas durante a leitura e a execução, além das 16
antecipadas no documento de arquitetura:

- **INV-P17** — expressão contendo `"` aborta a importação inteira. Descoberta
  por leitura (`main.rs:375`).
- **INV-P19** — expressão de busca acentuada **nunca** casa, porque o texto é
  normalizado e a expressão não. Descoberta por leitura, **confirmada
  empiricamente**. Tem impacto de produto: perfis acentuados estão inertes em
  produção. Escalado em D-17.
- **INV-P20** — PDF truncado é aceito pelo MuPDF, produz zero páginas e a
  importação termina em `status = 5`, indistinguível de um diário sem
  ocorrências. Descoberta por execução. Escalado em D-18.

Além disso, o compilador confirmou de forma independente o achado do campo
morto: `warning: field 'text' is never read` em `modelo.rs` — ver
`MAPA-DE-CHAMADAS.md`, §4.1.

### Medições

| Medição | Valor | Como |
|---|---|---|
| Limite de comprimento de termo | **40 bytes**, descarta `≥ 40` | `12-inv-p03-termos-longos.pdf` |
| Unidade de medida do limite | **bytes**, não runas | `12b-inv-p04-bytes-contra-runas.pdf` — `α`×19 (38 B) indexado, `α`×20 (40 B) não |
| Comportamento com PDF truncado | 0 páginas, **sem erro** | `20-corrompido.pdf` |
| Comportamento com arquivo vazio | erro `cannot tell in file` | `21-vazio.pdf` |
| Comportamento com não-PDF | erro `no objects found` | `22-nao-e-pdf.pdf` |
| Tempo de captura, 23 documentos | ~2 s | execução local |

### Versões do ambiente de desenvolvimento

⚠ **Não confirmadas contra produção** — ver D-15.

| Item | Versão |
|---|---|
| `tantivy` | 0.22.1 |
| `mupdf` | 0.4.4 |
| `diacritics` | 0.2.2 |
| `cargo` / `rustc` | 1.94.1 |
| `go` | 1.24.7 |
| `reportlab` | instalado via pip nesta sessão |

### Riscos aceitos

| Risco | Justificativa |
|---|---|
| **D-09** — 401 revela se o cabeçalho foi enviado | Contrato observável; unificar é mudança de contrato, fora do escopo. |
| **Corpus sintético como base de F5–F8** | As invariantes conhecidas estão cobertas; a variedade real não. Mitigado por D-11 permanecer bloqueante para F12. |
| **Limite de 40 bytes adotado sem `Cargo.lock` de produção** | Medição reprodutível em minutos após D-15; constante isolada no código. |

### Pendências que entram na F1

1. **D-15** (`Cargo.lock` original) e **D-11** (corpus real) precisam ser
   solicitados **agora** — o prazo de resposta corre em paralelo às fases
   seguintes.
2. **D-14** (espelhamento de tráfego) deve ser confirmado em F1, não em F12 —
   é o risco R8 do roadmap.
3. **D-08** e **D-10** (respostas de 404/405 e `Content-Type`) exigem captura
   empírica contra o serviço em execução. Barato, e bloqueia F9.
4. Escalar **D-17** e **D-18** a produto — são defeitos existentes, não
   questões da migração.

### Dependências novas

| Dependência | Onde | Justificativa |
|---|---|---|
| `tantivy`, `mupdf`, `diacritics`, `regex` | `tools/capturar-corpus` | Mesmas do legado; a captura precisa reproduzir o comportamento exato. |
| `sha2`, `hex` | `tools/capturar-corpus` | Mesmas do legado (`main.rs:12`), para o resumo dos textos capturados. |
| `serde`, `serde_json`, `anyhow` | `tools/capturar-corpus` | Serialização das saídas e propagação de erro. `anyhow` já é usado no legado. |
| `reportlab` | `tools/gerar-corpus-sintetico` | Geração determinística de PDFs com controle de posicionamento de linha, necessário para exercitar a segmentação em blocos. |

Nenhuma dessas entra no serviço em Go — são ferramentas de apoio.

### Cobertura das invariantes pelo corpus da F0

O corpus tem documento **dedicado e nomeado** para 10 das 20 invariantes. As
demais não são exercitáveis por um PDF isolado — são de código, de banco ou de
orquestração — e sua cobertura chega nas fases indicadas.

| Invariante | Onde é exercitada hoje | Onde vira teste |
|---|---|---|
| INV-P01 | `08-inv-p01-e-comercial.pdf` + chave `ACME & FILHOS` | F7 |
| INV-P02 | `03-`, `04-`, `05-inv-p02-*.pdf` + chaves do perfil 15 | F6 |
| INV-P03 | `12-inv-p03-termos-longos.pdf` + chaves do perfil 16 | F6 |
| INV-P04 | `12b-inv-p04-bytes-contra-runas.pdf` | F6 |
| INV-P05 | `09-`, `10-`, `11-inv-p05-*.pdf` + chave `JOAO SILVA` | F7 |
| INV-P06 | chave `---` do perfil 14, contra todo o corpus | F7 |
| INV-P07 | `06-` e `07-inv-p07-*.pdf` + chaves do perfil 12 | F6 |
| INV-P08 | não exercitável por PDF — é ordem de chamada | F6, teste de unidade |
| INV-P09 | `02-inv-p09-duas-colunas.pdf` | F5 |
| INV-P10 | `13-inv-p10-espacos-especiais.pdf` | F5 |
| INV-P11 | todo o corpus, via comparação byte a byte | F5 |
| INV-P12 | `14-` e `15-inv-p12-*.pdf` + perfis 7 e 8 | F8 |
| INV-P13 | não exercitável por PDF — depende de D-01 | F8, teste de unidade |
| INV-P14 | não exercitável por PDF — exige falha de gravação | F8, com dublê |
| INV-P15 | não exercitável por PDF — exige estouro numérico | F4, teste de unidade |
| INV-P16 | não exercitável por PDF — é de banco | F4, teste de integração |
| INV-P17 | não exercitável por PDF — exige expressão com aspas | F7, teste de unidade |
| INV-P18 | não exercitável por PDF — exige > 2^31 recortes | F4, teste de unidade |
| INV-P19 | `17-inv-p19-expressao-acentuada.pdf` + perfil 13 | F7 |
| INV-P20 | `20-corrompido`, `21-vazio`, `22-nao-e-pdf` | F5 |

**Lacuna conhecida.** INV-P17 não tem chave com aspas no `chaves.json` porque
ela abortaria a captura de **todos** os documentos (é justamente o
comportamento que a invariante descreve). O caso fica para um arquivo de chaves
separado, a ser usado em um cenário próprio na fase F7.

---

## F1 — Fundação do repositório e cadeia de ferramentas

**Status:** concluída com ressalva de verificação · **Data:** 2026-08-05

### Entregáveis

| Artefato | Caminho | Estado |
|---|---|---|
| Módulo Go | `go.mod` (`go 1.24.7`, ajustado para `1.24.0` na F2) | sem dependências de terceiros |
| Árvore de pacotes | `internal/`, `cmd/`, `test/`, `db/`, `api/`, `deploy/` | 15 `doc.go`, cada um dizendo o que o pacote **não** faz |
| Ponto de entrada | `cmd/recorte-api/main.go` | sobe, responde 404, aceita `-versao` e `-healthcheck` |
| Verificação de arquitetura | `internal/arch_test.go` | falha nomeando o pacote infrator |
| Configuração de lint | `.golangci.yml` (esquema v2) | 0 problemas no repositório |
| Automação | `Makefile` | `ci` = `tidy` + `lint` + `test` + `build` |
| Imagem | `deploy/Dockerfile` | multiestágio, distroless, sem privilégio |
| Integração contínua | `.github/workflows/ci.yml` | dois trabalhos: verificação e imagem |

### Decisões tomadas

1. **`misspell` foi removido depois de medido.** Ele só conhece inglês e
   sinalizou `Comando`→`Commando`, `Eles`→`Eels`, `posicional`→`positional`.
   Não há como configurar dicionário pt-BR. A recusa está registrada em nota no
   próprio `.golangci.yml`, com o motivo — para que ninguém o reintroduza
   achando que foi esquecimento.

2. **`depguard` foi recusado por redundância** com `internal/arch_test.go`, que
   já verifica a mesma regra e produz mensagem melhor.

3. **`errcheck` com `check-blank: false`.** O idioma
   `defer func() { _ = x.Close() }()` é a forma padrão de documentar descarte
   deliberado; exigir tratamento ali só produziria `nolint` espalhado.

4. **A verificação de arquitetura usa `go/build` da biblioteca padrão**, não
   `golang.org/x/tools/go/packages`. O módulo continua sem dependências de
   terceiros, o que é ele próprio uma propriedade que a regra protege.

5. **Exceção estreita para arquivos de teste na regra de dependência.** Podem
   importar auxiliares de terceiros; **não** podem importar `adapter`,
   `platform` ou `cmd`. Exigir cobertura de 90% no domínio sem auxiliares seria
   restrição sem contrapartida, e é a segunda proibição que garante o
   isolamento do núcleo.

6. **A sonda de saúde é o próprio binário** (`-healthcheck`), não `curl`. A
   imagem distroless não tem shell nem utilitários de rede, e acrescentá-los
   pelo `HEALTHCHECK` desfaria a escolha da base.

7. **Nenhuma cópia de bibliotecas no estágio de distribuição.** Seria
   maquinário especulativo para a F5, não exercitado por código desta fase e
   portanto não verificável. Em seu lugar, uma linha `ldd` no estágio de
   construção registra a linha de base de dependências compartilhadas; a
   diferença quando o MuPDF entrar é exatamente o que a F5 precisará tratar.

### Estado do binário nesta fase

Responde **404 em qualquer rota**, inclusive `/ping` — é o estado esperado,
declarado nos critérios de aceite da fase. Consequência direta: o `HEALTHCHECK`
da imagem reporta o contêiner como **não saudável** até a fase F9 registrar a
rota. Documentado no `Dockerfile` e no comentário de `sondarSaude`.

Verificado localmente:

| Comando | Resultado |
|---|---|
| `-versao` | imprime a versão injetada, sai 0 |
| `GET /ping`, `/pdf`, `/qualquer` | 404 |
| `-healthcheck` | reporta 404 e sai 1 |
| `SIGTERM` | encerra gracioso, sai 0 |

### Verificação executada

```
make ci                                              lint 0 problemas, testes ok, build ok
go test ./internal -run TestRegraDeDependencia        PASS
  com violação de adaptador injetada                 FAIL, nomeando o pacote e a importação
  com dependência de terceiros injetada              FAIL, nomeando o pacote e a importação
```

### ⚠ Ressalva de verificação — Docker

**Não há daemon Docker neste ambiente.** As seguintes verificações da fase
**não foram executadas** e permanecem pendentes:

| Verificação | Estado |
|---|---|
| `docker build -f deploy/Dockerfile` | **não executada** |
| Imagem final abaixo de 120 MB | **não verificada** |
| Imagem roda sem privilégio | não verificada — declarado por `USER nonroot:nonroot` |
| `docker run recorte-api:f1 -versao` | **não executada** |
| `libmupdf-dev` e codecs existem em bookworm | **não verificado** |

O `Dockerfile` e o trabalho `imagem` da integração contínua estão escritos e
implementam essas verificações como portão — inclusive o limite de 120 MB e a
recusa de usuário privilegiado. **A primeira execução da integração contínua é
que vai validá-los.** Até lá, tratar o empacotamento como não verificado.

### Pendências que entram na F2

1. As de F0 continuam abertas: **D-15** e **D-11** (bloqueantes), **D-14**
   (confirmar espelhamento agora, não em F12), **D-08** e **D-10** (captura
   empírica barata que bloqueia F9).
2. Validar o `Dockerfile` na primeira execução da integração contínua.
3. `main.go` lê `SERVIDOR_IP` e `SERVIDOR_PORTA` diretamente, com padrões
   embutidos. A F2 substitui isso por `internal/config` — **inclusive o
   comportamento de porta inválida cair no padrão** (A19), que a leitura atual
   não reproduz: hoje um valor não numérico chega ao `net.JoinHostPort` e a
   escuta falha, em vez de cair em 6001.

### Dependências novas

Nenhuma. O módulo não tem dependências de terceiros ao fim da F1.

---

## F2 — Configuração tipada e observabilidade

**Status:** concluída · **Data:** 2026-08-05

### Entregáveis

| Artefato | Caminho | Cobertura |
|---|---|---|
| Configuração tipada | `internal/config/config.go` | 90,1% |
| Tipos de segredo | `internal/config/segredo.go` | — |
| Registrador estruturado | `internal/platform/observability/logger.go` | 92,2% |
| Propagação por contexto | `internal/platform/observability/contexto.go` | — |
| Métricas | `internal/platform/observability/metricas.go` | — |
| Rastreamento OTLP | `internal/platform/observability/tracing.go` | — |
| Ponto de entrada ligado | `cmd/recorte-api/main.go` | — |

### Divergência encontrada no passo 1 do procedimento

O prompt da F2 enumera **16** variáveis; o apêndice §8.1 do roadmap lista **20**
— faltam `VARREDURA_ORFAS`, `RATE_LIMIT_RPS`, `STATUS_ENDPOINT` e
`HEALTH_ENDPOINTS` na lista do prompt.

A divergência é **aditiva**: o apêndice é superconjunto. Implementadas as 20,
o que satisfaz as duas fontes e evita que a F11 reabra o encanamento de
configuração. Registrado em vez de bloquear, porque parar aqui não entregaria
nada e nenhuma interpretação leva a trabalho diferente.

### Decisões tomadas

1. **Segredos são tipos, não `string`.** `config.Segredo` e `config.URLSegredo`
   implementam `String`, `GoString`, `LogValue` e `MarshalText` devolvendo o
   marcador. A proteção passa a ser **estrutural**: `%v`, `%s`, `%#v`,
   `slog.Any` e `json.Marshal` não têm como vazar. O valor real só sai por
   `Revelar()`, que é auditável por busca textual.

2. **`MarcadorRedigido` é ASCII (`REDIGIDO`).** A primeira versão usava aspas
   angulares e saía como `%C2%ABredigido%C2%BB` dentro da URL, porque
   `url.String()` codifica não-ASCII. Sem vazamento, mas ilegível no
   diagnóstico.

3. **A URL redigida preserva esquema, usuário, host, porta e base.** São o que
   serve ao diagnóstico; só a senha e os parâmetros sensíveis viram marcador.
   URL malformada fica **totalmente opaca** — é preferível perder diagnóstico a
   ecoar uma cadeia que pode ter a senha em posição inesperada.

4. **`CONFIG_ESTRITA` só governa `SERVIDOR_PORTA`.** As demais variáveis são
   novas: não há comportamento de legado a preservar, então valor malformado é
   sempre erro, independentemente da chave.

5. **`SHUTDOWN_TIMEOUT` aceita inteiro simples como segundos**, além da notação
   do Go. É como operadores escrevem.

6. **Métricas com Prometheus, rastros com OpenTelemetry.** Combinação usual e
   previsível. O registro é **próprio**, nunca o global — teste comprova que
   duas instâncias coexistem.

7. **Métrica `recorte_documentos_sem_paginas_total` acrescentada**, cumprindo o
   encaminhamento de D-18: é a única forma de enxergar o caso do PDF truncado
   (INV-P20) sem alterar comportamento.

8. **`NovoLogger` recebe um `io.Writer`**, divergindo da assinatura do prompt
   (`NovoLogger(cfg)`). Sem isso não há como asserir a saída em teste, e o
   critério de aceite da fase exige exatamente essa asserção.

### Bug encontrado por cobertura

Ao cobrir o caminho do OTLP, `resource.Merge` falhou com *conflicting Schema
URL*: `resource.Default()` do SDK usa o esquema 1.43.0 e o `semconv` importado
era 1.26.0. **Só apareceria quando alguém ligasse `OTEL_EXPORTER_OTLP_ENDPOINT`
em produção.** Corrigido com `resource.NewSchemaless`, que elimina o
acoplamento de versão. A cobertura do pacote subiu de 66,7% para 92,2%.

### Incidente de toolchain — e a guarda que ficou

`go get` das dependências elevou a diretriz do `go.mod` de `1.24.7` para
`1.25.0`, porque as versões correntes de `prometheus/client_golang`, `otel` e
`grpc` exigem 1.25. Isso trocou o toolchain em tempo de compilação, e **as
toolchains que o Go baixa automaticamente neste ambiente vêm truncadas**:

```
$ ls .../toolchain@v0.0.1-go1.25.0.../pkg/tool/linux_amd64/
asm cgo compile cover link preprofile vet        ← sem covdata, pack, nm...
```

Resultado: `go test -coverprofile` passou a falhar com
`go: no such tool "covdata"`. As duas toolchains baixadas (1.25.0 e 1.25.12)
estão igualmente truncadas; só a 1.24.7 pré-instalada é completa.

**Resolução:** o módulo ficou em `go 1.24.0`, com as dependências fixadas nas
últimas versões compatíveis:

| Módulo | Versão | Última exige |
|---|---|---|
| `prometheus/client_golang` | v1.21.1 | v1.24.1 → go 1.25 |
| `go.opentelemetry.io/otel*` | v1.35.0 | v1.45.0 → go 1.25 |
| `grpc-gateway/v2` | v2.26.1 | v2.29.0 → go 1.25 |

**Guarda criada:** o alvo `make guarda-toolchain` falha se a diretriz `go` do
`go.mod` mudar ou se surgir uma linha `toolchain`, e integra o `make ci`. A
mensagem instrui a atualizar `Makefile`, `deploy/Dockerfile` e o fluxo de
integração contínua **na mesma mudança** — porque foi a divergência entre eles
que criou o problema.

Isso é dívida assumida: as dependências ficam uma geração atrás até que a
elevação para 1.25 seja feita deliberadamente, com os três arquivos alinhados.

### Correção de registro da F1

O registro da F1 dizia `go.mod (go 1.23.0)`. O valor efetivamente commitado era
`go 1.24.7` — a intenção declarada nunca foi aplicada ao arquivo. Corrigido
acima.

### Mapeamento das mensagens de log do legado

Identificadores saem do texto e viram atributos; a mensagem fica estável e
pesquisável. Emitidos a partir das fases indicadas.

| `reference/main.rs` | Mensagem do legado | Evento em Go | Atributos | Fase |
|---|---|---|---|---|
| 66 | `Servidor iniciando em: {servidor}` | `servidor iniciando` | `endereco`, `versao`, `revisao`, `config` | **F2** |
| 104 | `>>>>> <CTRL>-C recebido` | `encerrando` | `motivo=sinal` | F10 |
| 105 | `>>>>> Kill recebido` | `encerrando` | `motivo=sinal` | F10 |
| 72 | `Aguardando tarefas encerrarem.` | `drenando importações` | `em_andamento` | F10 |
| 136 | `[ID Requisição: {id} recebida` | `requisição recebida` | `id_requisicao` | F9 |
| 216 | `Parâmetros recebidos: {:?}` | `parâmetros recebidos` | `id_requisicao`, campos | F9 |
| 219 | `Críticas: {:?}` | `validação rejeitou a requisição` | `id_requisicao`, `criticas` | F9 |
| 240 | `Erro ao registrar o PDF: {e:?}` | `falha ao registrar importação` | `id_requisicao`, `erro` | F9 |
| 258 | `Processo de recorte INICIADO` | `processo de recorte iniciado` | `id_importacao` | F8 |
| 264 | `Processo de recorte FINALIZADO` | `processo de recorte finalizado` | `id_importacao`, `duracao` | F8 |
| 480 | `Paginando PDF` | `paginando documento` | `id_importacao` | F5 |
| 508 | `Há {} página(s) a indexar` | `documento paginado` | `id_importacao`, `paginas` | F5 |
| 533 | `PDF indexado` | `documento indexado` | `id_importacao`, `paginas` | F7 |
| 276 | `Há {} perfis/expressões a pesquisar` | `chaves de pesquisa carregadas` | `id_importacao`, `chaves` | F8 |
| 292 | `Há {} recorte(s) a filtrar para [Perfil …]` | `recortes encontrados` | `id_importacao`, `id_perfil`, `expressao`, `total` | F8 |
| 309 | `Há {} recorte(s) a registrar …` | `recortes a gravar` | idem + `total` | F8 |
| 311 | `Registrado(s) {} recorte(s) …` | `recortes gravados` | idem + `total` | F8 |
| 313 | `Erro ao registrar recorte …` | `falha ao gravar recortes` | idem + `erro` | F8 |
| 319 | `Erro a recortar [Perfil … ]` | `falha ao recortar` | idem + `erro` | F8 |
| 328 | `Erro ao obter perfis/expressões` | `falha ao carregar chaves de pesquisa` | `id_importacao`, `erro` | F8 |
| 333 | `Erro ao criar índice de buscas` | `falha ao indexar documento` | `id_importacao`, `erro` | F7 |
| 610 | `Erro ao salvar recorte: {e:?}` | `falha ao gravar recorte` | `id_importacao`, `erro` | F4 |
| 622 | `Erro ao salvar texto do recorte: {e:?}` | `falha ao gravar texto do recorte` | `id_importacao`, `erro` | F4 |

Duas observações. As linhas 104 e 105 usam `println!`, não o subsistema de
registro — a mensagem do legado não é estruturada nem sai pelo `tracing`; em Go
passam a ser eventos normais. E as linhas 313, 319 e 333 têm colchete não
fechado ou seta com espaço no legado (achado A21); as mensagens em Go são
regulares, o que é mudança de **texto de log**, não de contrato.

### Verificação executada

```
make ci                                        guarda ok, tidy ok, lint 0, testes ok, build ok
go test ./internal/config/... -race -cover     90,1%
go test ./internal/platform/... -race -cover   92,2%
bin/recorte-api sem segredos                   falha listando DATABASE_URL E API_KEY juntas, saída 1
bin/recorte-api com segredos                   sobe, registra JSON com config redigida, 404 em /ping
SIGTERM                                        encerra gracioso, saída 0
```

### Pendências que entram na F3

1. As de F0 seguem abertas: **D-15** e **D-11** (bloqueantes), **D-14**,
   **D-08** e **D-10**.
2. Elevar deliberadamente para go 1.25 quando o ambiente de construção tiver
   toolchain completa, atualizando os três arquivos juntos.
3. O `Dockerfile` continua **não verificado** — sem daemon Docker no ambiente.

### Dependências novas

| Dependência | Justificativa |
|---|---|
| `github.com/joho/godotenv` | Equivalente do `dotenvy` do legado; mesma precedência (ambiente vence o arquivo) e mesma tolerância à ausência. |
| `github.com/prometheus/client_golang` | Métricas. Padrão de mercado; expõe registro próprio, o que evita estado global. |
| `go.opentelemetry.io/otel` + `sdk` + `trace` + exportador OTLP | Rastreamento distribuído, desligado por padrão. |
| `github.com/prometheus/client_model` | Apenas em teste, para asserir tipo e rótulo das métricas coletadas. |

---

## F3 — Núcleo de domínio e portas

**Status:** concluída · **Data:** 2026-08-05

### Entregáveis

| Artefato | Caminho | Cobertura |
|---|---|---|
| Data pura, sem fuso | `internal/domain/data.go` | |
| Conversões estreitantes | `internal/domain/numeros.go` | |
| Máquina de estados | `internal/domain/status.go` | |
| Acumulador de críticas | `internal/domain/critica.go` | 96,6% |
| Validação da submissão | `internal/domain/validacao.go` | (pacote) |
| Entidades | `internal/domain/{importacao,recorte,perfil}.go` | |
| Erros sentinela | `internal/domain/errors.go` | |
| Portas | `internal/domain/ports.go` | |
| Dublês de teste | `internal/domain/domaintest/` | 66,7% |

`go list -deps ./internal/domain` não lista **nenhuma** dependência de
terceiros: apenas `context`, `errors`, `fmt`, `strconv`, `strings`, `time`,
`unicode` e `unicode/utf8`.

### Passo 1 do procedimento — conferência bloqueante

Os dez textos de crítica foram conferidos **linha a linha** contra
`reference/main.rs`. Todos conferem **byte a byte**. Uma divergência apareceu,
e era de citação: `PDF não possui nome` está na linha **207**, não 206 como a
`ESPECIFICACAO.md` afirmava. Corrigido.

### INV-P21 — a medição que refutou a especificação

A `ESPECIFICACAO.md` afirmava, marcado como `INFERIDO — confirmar`:

> `%Y-%m-%d` é estrito: exige quatro dígitos de ano e dois de mês e dia.
> `2024-3-15` **não** é aceito.

**Falso.** Medido contra chrono 0.4 com uma sonda descartável, o analisador é
bem mais permissivo. Seis formas que o legado aceita são rejeitadas por
`time.Parse("2006-01-02", …)`:

| Entrada | chrono | valor | `time.Parse` |
|---|---|---|---|
| `2024-3-15` | aceita | 2024-03-15 | rejeita |
| `2024-03-5` | aceita | 2024-03-05 | rejeita |
| `24-03-15` | aceita | **0024**-03-15 | rejeita |
| `  2024-03-15` | aceita | 2024-03-15 | rejeita |
| `+2024-03-15` | aceita | 2024-03-15 | rejeita |
| `-2024-03-15` | aceita | −2024-03-15 | rejeita |

Note `24-03-15`: não é só aceitar ou recusar — o **valor persistido** também
diverge, porque o ano é 24 e não 2024.

Um porte com `time.Parse` responderia 400 com
`Data do caderno é inválida` onde o legado responde 200. Catalogado como
**INV-P21** com a gramática completa; `domain.AnalisarData` a implementa, e
`TestAnalisarDataDivergeDeTimeParse` falha se alguém "simplificar" a função.

A mesma sonda mediu `str::parse::<i64>()` e `::<i32>()`: Rust e Go **concordam**
— ambos aceitam sinal explícito e rejeitam espaço, separador de milhar e parte
decimal. Equivalência agora medida, não presumida.

### Decisões tomadas

1. **A validação mora no domínio**, em `SubmissaoPDF.Validar()`. É regra de
   negócio, não de transporte, e é o que torna o critério de aceite exaustivo
   verificável sem HTTP. A F8 e a F9 apenas a acionam.

2. **`Data` é tipo próprio**, não `time.Time`. Evita, por construção, o
   deslocamento de um dia de INV-P16 — `time.Time` carrega fuso e convida ao
   erro.

3. **`Recorte` mantém `Texto` e `Destaque`**, apesar de `Texto` ser dado morto
   no legado. Em Go os dois compartilham o mesmo *backing array*, então o custo
   é um cabeçalho de string, não uma cópia do texto da página. Manter deixa a
   evolução da F11 (janela de contexto) como mudança de uma linha.

4. **`PodeTransicionarPara` não recusa gravação.** É rede de segurança de
   desenvolvimento: o legado não valida transições, e recusar seria
   comportamento novo. Serve para registrar anomalia, não para barrar.

5. **`StatusReservado` (4) declarado e sem uso**, com comentário explicando que
   o número fica reservado — se alguém precisar de um estado novo, deve escolher
   outro, porque linhas antigas do banco podem conter 4.

6. **Conversões estreitantes centralizadas** em `numeros.go`. Surgiram porque o
   `gosec` sinalizou `int32(v)` com G115 — exatamente a classe de bug de
   INV-P15. Em vez de suprimir o aviso, criei `ParaInt32`, `ParaInt64` e
   `TamanhoParaInt32`, que a F4 vai usar em `MarcarTermino` e `nr_pagina`.

### Corrida encontrada nos dublês

O detector de corrida acusou acesso concorrente ao estado dos falsos: o
`Diario` tinha trava, mas `Registradas`, `Status` e `Gravacoes` não. Como a F8
exercita o executor com importações simultâneas, os dublês precisam ser seguros.
Corrigido com trava por dublê e acessadores `StatusGravados()` e
`GravacoesObservadas()` que devolvem cópia.

### Verificação executada

```
make ci                                             guarda ok, tidy ok, lint 0, testes ok, build ok
go list -deps ./internal/domain                     nenhum terceiro
go test ./internal -run TestRegraDeDependencia      PASS
go test ./internal/domain/... -race -cover          96,6%
  TestValidacaoExaustiva                            243 subtestes (3^5 combinações)
  TestAnalisarDataReproduzChrono                    45 casos medidos
  TestTransicoes                                    49 pares (7×7), válidos e inválidos
```

### Pendências que entram na F4

1. As de F0 seguem abertas: **D-15** e **D-11** (bloqueantes), **D-14**,
   **D-08** e **D-10**.
2. O `Dockerfile` continua **não verificado** — sem daemon Docker no ambiente.
3. A F4 deve usar `domain.ParaInt32` em `MarcarTermino` (INV-P18) e
   `domain.ParaInt64` em `nr_pagina` (INV-P15).

### Dependências novas

Nenhuma. O núcleo importa apenas a biblioteca padrão.

---

## F4 — Persistência: pgx, consultas literais e unidade de trabalho

**Status:** concluída · **Data:** 2026-08-05

### Entregáveis

| Artefato | Caminho |
|---|---|
| Pool com parâmetros explícitos | `internal/adapter/postgres/pool.go` |
| Consultas embutidas | `internal/adapter/postgres/queries/*.sql` (7 arquivos) |
| Carregador e paridade textual | `internal/adapter/postgres/queries.go` |
| Executor resolvido por contexto | `internal/adapter/postgres/consultador.go` |
| Unidade de trabalho | `internal/adapter/postgres/uow.go` |
| Repositórios | `importacao_repo.go`, `perfil_repo.go`, `recorte_repo.go` |
| Migração de linha de base | `db/migrations/0001_baseline.sql` |
| Testes de integração | `integracao_test.go` — 16 testes |

Cobertura do pacote: **80,6%** (critério: acima de 75%).

### Divergência do passo 1

O prompt fala em "cinco consultas"; o `main.rs` tem **sete** — e a
`ESPECIFICACAO.md` §2.5 já as listava todas. Transcritas as sete. Divergência
aditiva, registrada em vez de bloquear.

### Paridade textual das consultas

As sete são **byte a byte idênticas** aos literais do `main.rs`, incluindo
espaços à direita (`recorte.tb_perfil_variacao tpv ` tem um) e a indentação da
linha de fechamento.

Os arquivos `.sql` têm cabeçalho explicativo separado por um marcador; o
carregador descarta tudo até ele, então o que chega ao PostgreSQL é o literal
puro. `TestCabecalhoNaoVazaParaOBanco` garante isso.

**Verificado que o teste morde:** alterei `ORDER BY tpv.expressao_nm` para
`DESC` e ele apontou o byte 687 com o contexto dos dois lados.

### Decisões tomadas

1. **Transação por CHAMADA de `Salvar`, não por importação.** É a decisão mais
   consequente da fase. O legado chama `salvar_recorte` uma vez por chave, e
   esse escopo preserva exatamente o que sobrevive a uma falha no meio
   (INV-P14): as chaves já gravadas permanecem, a que falhou não deixa nada, as
   seguintes não são processadas. Transação por importação reverteria as
   anteriores e mudaria o estado final — por isso é evolução da F11.

2. **A transação viaja no contexto**, resolvida por `base.consultador(ctx)`.
   Mantém as portas do domínio livres de qualquer conceito de banco.

3. **Aninhamento reaproveita a transação corrente** em vez de abrir outra. O
   escopo é decisão de quem chama `EmTransacao`; aninhar em silêncio mudaria o
   que sobrevive a uma falha.

4. **Nenhum `AfterConnect` define `search_path`.** Todas as consultas
   qualificam `recorte.` explicitamente e D-12 ainda não indicou configuração
   por papel ou banco. O ponto de extensão está comentado em `pool.go`.

5. **Migração de linha de base só verifica.** Emitir `CREATE`/`ALTER` a partir
   de uma reconstrução marcada como `INFERIDO` (D-13) arriscaria alterar um
   banco de produção com base em palpite. A migração lista as 7 tabelas e 30
   colunas e falha nomeando o que falta.

6. **`pgtype.Date` com `time.Date(..., time.UTC)`.** Nunca o fuso local — é o
   que evita o deslocamento de um dia de INV-P16.

### Desvio deliberado: sem testcontainers

O prompt pedia testcontainers-go. **Não há daemon Docker neste ambiente**, e
testcontainers exige um. Entregar testes que não podem ser executados seria
pior do que a alternativa.

Os testes leem `TEST_DATABASE_URL` e são pulados com mensagem explicativa
quando ela falta. O banco vem de onde estiver disponível:

| Contexto | Origem do banco |
|---|---|
| Este ambiente | `make pg-subir` — cluster PostgreSQL 16.13 local via `pg_ctl` |
| Integração contínua | serviço `postgres:16` do GitHub Actions |
| Máquina de quem desenvolve | qualquer PostgreSQL, via a variável |

Testcontainers é apenas uma forma de **prover** um banco; o que os testes
exercitam é idêntico. `make test-integration` sobe o cluster e roda tudo.

### Achado A03 — demonstrado, não apenas corrigido

`TestIntegracaoFalhaNoSegundoInsertNaoDeixaOrfao` cria um gatilho que faz o
`INSERT` em `tb_recorte_texto` falhar.

Para provar que o teste não passa por acaso, **substituí a transação pelo
comportamento do legado** (dois INSERT independentes) e reexecutei:

```
--- FAIL: TestIntegracaoFalhaNoSegundoInsertNaoDeixaOrfao
    ACHADO A03: 1 linha(s) órfã(s) em tb_recorte — a transação não reverteu
```

Com a transação: passa. O teste reproduz o defeito e comprova a correção.

### Código morto removido

Duas funções escritas por antecipação foram apagadas depois que os
verificadores as apontaram: `dataDoTempo` (nenhum repositório lê data de volta)
e `ErroEhNaoEncontrado` (nenhuma consulta pode devolver zero linhas hoje —
`GET /importacao/{id}` é da F11). É o mesmo padrão do estágio de distribuição
do `Dockerfile` na F1: maquinário especulativo não entra.

### Parâmetros do pool — origem de cada número

Reproduzem os padrões do SQLx, que é o que o legado usa via `Pool::connect`
(`main.rs:85`), para não mudar a pressão sobre o banco antes da medição da F12.

| Parâmetro | Valor | Origem |
|---|---|---|
| `MaxConns` | 10 | padrão do SQLx |
| `MinConns` | 0 | padrão do SQLx |
| `MaxConnLifetime` | 30 min | `max_lifetime` do SQLx |
| `MaxConnIdleTime` | 10 min | `idle_timeout` do SQLx |
| `ConnectTimeout` | 30 s | `acquire_timeout` do SQLx |
| `HealthCheckPeriod` | 1 min | padrão do pgx; SQLx não tem equivalente |

### Verificação executada

```
make ci                                             lint 0, testes ok, build ok
go test ./internal/adapter/postgres/                paridade textual das 7 consultas: PASS
  com ORDER BY adulterado                           FAIL apontando o byte 687
make test-integration                               16/16 PASS contra PostgreSQL 16.13
  com a transação removida                          FAIL: 1 linha órfã (reproduz A03)
TZ=UTC / Asia/Tokyo / America/Sao_Paulo             INV-P16: mesma data nos três
cobertura de internal/adapter/postgres              80,6%
```

### Pendências que entram na F5

1. **D-13** ganhou urgência: `testdata/esquema_inferido.sql` é uma
   reconstrução, e os testes de integração validam as consultas contra ela, não
   contra o esquema real. Quando D-13 for respondida, o arquivo deve ser
   trocado por um `pg_dump --schema-only` e qualquer divergência vira achado.
2. As demais de F0 seguem: **D-15** e **D-11** (bloqueantes), **D-14**,
   **D-08** e **D-10**.
3. O `Dockerfile` continua **não verificado** — sem daemon Docker.

### Dependências novas

| Dependência | Justificativa |
|---|---|
| `github.com/jackc/pgx/v5` | Driver e pool. É o padrão de mercado para PostgreSQL em Go e o único com suporte nativo a `pgtype.Date`, necessário para INV-P16. |

---

## F5 — Extração de texto do PDF

**Status:** concluída · **Data:** 2026-08-05

### Resultado

**159 de 159 páginas idênticas byte a byte** ao texto capturado do legado, em
26 documentos. Relatório completo em `docs/F5-AVALIACAO-EXTRACAO.md`.

### A avaliação encontrou um caminho que o roadmap não previa

O roadmap desenhava três caminhos: (A) ligação existente, (B) *shim* em C sobre
`fz_stext_page`, (C) extrator Rust como serviço lateral. A medição encontrou um
quarto, melhor que A e que dispensa B e C.

| Caminho | Páginas idênticas | Situação |
|---|---|---|
| A — `doc.Text()` | 20 de 154 | **reprovado** |
| **A′ — `doc.HTML()` remontado** | **159 de 159** | **adotado** |
| B — *shim* em C via cgo | — | desnecessário |
| C — serviço lateral em Rust | — | desnecessário |

**Por que `Text()` falha.** O `fz_print_stext_page_as_text` junta as linhas de
um bloco com espaço e aplica a de-hifenização própria do MuPDF. O legado emite
`\n` por linha. Não é cosmético: `conti-\n` + `nuacao` vira `continuacao` no
legado (um termo) e `conti-nuacao` com `Text()` (dois termos, hífen é
separador). A expressão `CONTINUACAO DO PROCESSO` gera recorte num caso e não
no outro.

**Por que `HTML()` funciona.** O escritor HTML do MuPDF emite **um `<p>` por
linha do `fz_stext_page`** — exatamente a granularidade do laço do legado. E
como o legado concatena as linhas sem separador entre blocos (INV-P09), a
fronteira de bloco não é observável, então só a de linha precisa bater.

### O corpus foi atacado antes de a estratégia ser aceita

O corpus da F0 é gerado por reportlab, que desenha cada linha como objeto
separado — a estratégia poderia funcionar por acidente. Cinco documentos foram
acrescentados **para tentar quebrá-la**, e todos passaram:

| Documento | Ataque |
|---|---|
| `23-f5-caracteres-de-marcacao` | `<`, `>`, `&`, aspas e entidades literais no texto |
| `24-f5-linhas-coladas` | espaçamento de 2, 4 e 6 pt |
| `25-f5-mesma-linha-varios-desenhos` | três `drawString` na mesma altura devem virar UMA linha |
| `26-f5-ordem-de-desenho-invertida` | linhas desenhadas de baixo para cima |
| `27-f5-tamanhos-mistos` | fontes de tamanhos diferentes na mesma linha |

O caso `25` é o mais informativo: confirma que `<p>` ⟺ linha do `stext` é
correspondência **estrutural**, não coincidência do gerador.

### Decisões tomadas

1. **Análise com tokenizador de HTML de verdade** (`golang.org/x/net/html`),
   não expressão regular. Atributos de estilo do MuPDF contêm `:` e `;`, e o
   texto da página pode conter `<`, `>` e `&` escapados. A ferramenta de
   avaliação usa regex por ser descartável; o adaptador, não.

2. **O extrator devolve texto BRUTO.** A normalização é a F6 e entra como
   decorador, não como passo escondido aqui. É o que mantém uma falha de
   extração distinguível de uma de normalização — no diagnóstico e no corpus
   (`*.paginas-brutas.json` contra `*.paginas.json`).

3. **`ExtratorTexto` ainda não está completo.** Até a F6, `ExtrairPaginas`
   devolve texto sem normalizar. Registrado no comentário do tipo.

### Empacotamento — a pendência da F1 está resolvida

O `go-fitz` embarca o MuPDF estático (~22 MB), então:

- o `Dockerfile` e a integração contínua **perderam** `libmupdf-dev` e os seis
  pacotes de codecs;
- o binário depende **só de libc** — nada a copiar para o estágio distroless;
- a rota (a) prevista na F1 se confirmou.

O `Dockerfile` ganhou uma verificação de `ldd` que falha a construção se o
binário adquirir qualquer dependência compartilhada além de libc.

### Armadilha registrada: `CGO_ENABLED=0` compila e quebra em execução

Testado, e vale registro porque é silencioso:

```
$ CGO_ENABLED=0 go build ./cmd/recorte-api   # compila, binário estático
$ ./recorte-api
panic: cannot load library: libmupdf.so: cannot open shared object file
```

Sem cgo o `go-fitz` recorre ao `purego` e exige um `libmupdf.so`
**compartilhado**. A falha não aparece na compilação. Documentado no
`Dockerfile` e no fluxo de integração contínua.

### Custo medido

| Métrica | Valor |
|---|---|
| Documento de 120 páginas | **17,6 ms** |
| Por página | ~0,15 ms |
| Alocações | 708 KB, 1.802 por documento |
| 200 extrações consecutivas | 3,17 s, heap estável (fator 0,73) |

Sem vazamento: o MuPDF é biblioteca C e um documento não fechado vazaria
memória que o coletor do Go não recupera. O `defer doc.Close()` é o que
impede, e `TestVazamento` é o que prova.

### Verificação executada

```
go run ./tools/avaliar-extracao            Text: 20/154 · HTML: 159/159
go test ./test/parity/ -run TestExtracao   159 de 159 páginas idênticas
  com a estratégia Text() no lugar         139 páginas apontadas como divergentes
go test ./internal/adapter/pdftext/...     entrada inválida, contexto, concorrência
  TestVazamento (200 iterações)            heap estável, fator 0,73
  BenchmarkExtrairPaginas                  17,6 ms / 120 páginas
CGO_ENABLED=0                              compila; pânico no arranque (registrado)
```

### Pendências que entram na F6

1. **D-11 segue bloqueante** e ganhou peso: a paridade da extração está provada
   contra corpus **sintético**. Diários reais têm digitalização, colunas
   irregulares, tabelas, texto rotacionado e camada de OCR.
2. **INV-P10 não foi exercitado**: nenhum documento do corpus tem glifo não
   mapeável, então "caractere irrecuperável vira espaço" continua sem
   verificação. Precisa de PDF real com fonte quebrada.
3. A versão do `go-fitz` precisa ficar **fixada**: o formato de saída do
   escritor HTML pode mudar entre versões. O teste de paridade na integração
   contínua é a rede.
4. As demais de F0: **D-15**, **D-14**, **D-08**, **D-10**, **D-13**.

### Dependências novas

| Dependência | Justificativa |
|---|---|
| `github.com/gen2brain/go-fitz` | Ligação Go para MuPDF, com a biblioteca embarcada estaticamente. É o mesmo motor C do legado, o que elimina a classe inteira de divergência de extração. |
| `golang.org/x/net/html` | Tokenizador de HTML para remontar as linhas. Analisar HTML com expressão regular seria frágil; já era dependência indireta. |

---

## F6 — Normalização e tokenização com paridade

**Status:** concluída · **Data:** 2026-08-05

### Resultado

| Verificação | Resultado |
|---|---|
| Normalização contra o corpus | **159 de 159** páginas idênticas byte a byte |
| Tokenização contra o corpus | **159 de 159** páginas, 3.359 termos conferidos |
| Teste de propriedade | **1.000.000** casos aleatórios, **zero** divergências |
| Cobertura de `searchidx` | 100% |

### Passo 1 — o limite confirmado NA FONTE

A F0 já o tinha medido empiricamente. A F6 exigia confirmação na fonte, e ela
veio de tantivy 0.22.1:

```
tokenizer_manager.rs:59-67   register("default",
                               SimpleTokenizer → RemoveLongFilter::limit(40)
                                               → LowerCaser)
remove_long.rs:36            token.text.len() < self.token_length_limit
remove_long.rs:28            "a limit in bytes of the UTF-8 representation"
simple_tokenizer.rs:46       if c.is_alphanumeric()
```

Três fatos que a fonte torna explícitos: o valor é **40**; a comparação é `<`,
então descarta `>= 40`; e `len()` conta **bytes**. Bate com a medição
independente da F0 (`α`×19 indexado, `α`×20 não).

### O teste de propriedade encontrou o que o corpus não pegou

Esta é a lição da fase. As 159 páginas do corpus passaram **byte a byte** com a
implementação errada. O teste de propriedade com um milhão de cadeias acusou
**320.142 divergências — 32% dos casos**.

Causa: `\w` do crate `regex` é `[\p{Alphabetic}\p{M}\p{Nd}\p{Pc}\p{Join_Control}]`,
e meu padrão `[\p{L}\p{N}_]` errava **nos dois sentidos**:

| Erro | Exemplo | Efeito |
|---|---|---|
| Perdia marcas combinantes | `"6"`+U+0327+`"-\n"` | legado junta, Go não juntava |
| Incluía `No`/`Nl` a mais | `"¼-\n93"` | legado não junta, Go juntava |

Catalogado como **INV-P22**. A recomendação que a INV-P02 fazia
(`[\p{L}\p{N}_]`) foi marcada como **refutada** no próprio documento.

### Três tabelas geradas, nenhuma escrita à mão

`tools/gerar-tabela-diacriticos` percorre as 63.488 runas do plano básico
multilíngue perguntando **ao próprio Rust** e emite só as divergências:

| Decisão | Como o Rust é consultado | Divergências |
|---|---|---|
| Diacríticos | `diacritics::remove_diacritics` | 2.205 |
| Segmentação de termos | `char::is_alphanumeric` | 1.265 |
| Caractere de palavra | motor de regex com `^\w$` | 9 |

Perguntar ao motor de expressões regulares em vez de reproduzir a definição por
escrito foi o que tornou a terceira tabela confiável — a definição do `\w` é
sutil o bastante para eu ter errado ao transcrevê-la.

Valores que não teriam como ser adivinhados: `ß→s` (um s, não "ss") e `Æ→A`
(não "AE").

### Decisões tomadas

1. **`JuntarHifens` não usa expressão regular.** O RE2 não expressa a classe
   `\w` do Rust, que mistura a propriedade `Alphabetic` com categorias. A
   varredura é manual e preserva a semântica de `replace_all`: busca da
   esquerda para a direita, e após uma substituição a varredura recomeça
   **depois** do trecho consumido — sem isso, `"a-\n-\nb"` daria resultado
   diferente do legado.

2. **A conversão de diacríticos é por runa**, como a da crate: consulta a
   tabela de exceções e, quando não está nela, usa a decomposição canônica.

3. **`Tokenizar` é uma só função** para indexar e para analisar consulta, como
   o `QueryParser` do legado faz. Duas implementações divergiriam em silêncio.

4. **O descarte por comprimento vem antes das minúsculas**, como na cadeia do
   Tantivy. Para alguns caracteres a conversão muda o número de bytes, então a
   ordem é observável.

### Custo medido

| Operação | Resultado |
|---|---|
| `Tokenizar` | **53,3 MB/s** — 13,6 ms por MiB, 57 mil alocações |

As alocações vêm de `strings.ToLower` nos termos que têm maiúscula; termos já
em minúsculas não alocam, pelo caminho rápido da biblioteca padrão.

### Verificação executada

```
go generate ./...                              tabelas reproduzidas sem alteração
go test ./test/parity/ -run TestNormalizacao   159/159 páginas
go test ./test/parity/ -run TestTokenizacao    159/159 páginas, 3.359 termos
go test ./test/parity/ -run TestPropriedade    1.000.000 casos, zero divergências
  com [\p{L}\p{N}_] no lugar                   320.142 divergências
go test ./internal/adapter/searchidx/ -cover   100%
go test -bench=Tokenizar                       53,3 MB/s
```

### Pendências que entram na F7

1. **D-15** ganhou peso de novo: as três tabelas e o limite de 40 bytes valem
   para tantivy 0.22.1 e diacritics 0.2.2. Alinhar o `Cargo.lock` de produção e
   reexecutar `go generate` é o procedimento de reconfirmação.
2. **D-11** segue bloqueante.
3. **INV-P10** continua sem exercício (herdado da F5).
4. As demais de F0: **D-14**, **D-08**, **D-10**, **D-13**.

### Dependências novas

| Dependência | Justificativa |
|---|---|
| `golang.org/x/text` | `norm.NFD`/`NFC` e `runes.Remove` para a decomposição canônica. Era dependência indireta; passou a direta. |

---

## F7 — Índice em memória e busca de frase

**Entregue.** `internal/adapter/searchidx` passou a satisfazer `domain.Indexador`
e `domain.Indice`: índice posicional em memória, busca de frase por interseção
de posições e o filtro do operador `&` com cache. O Tantivy saiu do caminho de
execução — continua existindo apenas dentro de `tools/capturar-corpus`, como
oráculo.

| Arquivo | Conteúdo |
|---|---|
| `internal/adapter/searchidx/indice.go` | `Indexador`, `indice`, `Frase`, interseção posicional |
| `internal/adapter/searchidx/extended.go` | `StripExtended` — a flag `x` do *crate* `regex` |
| `internal/adapter/searchidx/filtro.go` | `CompilarFiltro`, tradução de classes Perl, cache |
| `tools/capturar-corpus/src/bin/sonda-extended.rs` | sonda exploratória do modo `x` |
| `tools/capturar-corpus/src/bin/oraculo-extended.rs` | oráculo do filtro `&` para o teste de propriedade |
| `test/parity/busca_test.go` | harness contra `*.busca.json` |
| `test/parity/filtro_test.go` | dois testes de propriedade, 500.000 casos |

### O oráculo que faltava

`recortes.json` **não serve** para medir F7: ele já passou pela deduplicação por
perfil (INV-P12), que remove páginas que a busca devolveu, e portanto mede F7 e
F8 juntas. `capturar-corpus` ganhou um quinto arquivo, `<documento>.busca.json`,
com o resultado **cru** de `recortar` por expressão — 53 expressões × 26
documentos = **1.378 combinações**.

O arquivo registra **três** desfechos, não dois:

| Desfecho | Causa | Efeito no legado | Ocorrências |
|---|---|---|---|
| `ok` | `Ok(_)` | segue o laço | 1.291 |
| `erro` | `Err(_)` do analisador de consulta | status −1, importação encerra (INV-P17) | 78 |
| `panico` | `.unwrap()` na expressão regular do filtro `&` | a tarefa **morre** e o status fica preso em 3 (D-06) | 9 |

Capturar o pânico exigiu `catch_unwind` no ferramental. Valeu a pena: os 9
pânicos são a evidência empírica de **INV-P23**.

### Decisões

**D-F7-01 — a especificação da fase estava errada sobre classes de caracteres, e
foi corrigida.** O enunciado afirmava que o modo `x` preserva espaços dentro de
`[...]`, como PCRE e Python. A medição com `sonda-extended` refuta as duas
metades: `(?imx)[a b]` ≡ `(?im)[ab]`, e `(?imx)[a # b]` é **erro de sintaxe**,
porque o comentário engole o `]`. O Rust é o oráculo. A consequência é boa —
`StripExtended` não rastreia classes de caracteres.

**D-F7-02 — o `\s` injetado é traduzido por classe explícita, não por `\s`.** O
`\s` do RE2 tem 5 runas; o do Rust tem 25, e a diferença inclui `\v`, `NBSP` e o
espaço ideográfico, todos possíveis na saída do MuPDF. Traduzir `\s` por `\s`
faria o filtro perder recortes que hoje existem. A classe está escrita **sem
nenhum espaço literal**, porque ela mesma atravessa `StripExtended`.

**D-F7-03 — `\S` e `\D` também são traduzidos; `\w`, `\W`, `\b`, `\B` e classe
aninhada são RECUSADOS.** Aproximar criaria divergência **silenciosa**; recusar
troca isso por falha **alta**, no mesmo regime em que o RE2 já recusa `(?-x)`.
Registrado em D-06, com o conserto exato descrito caso alguma expressão real
precise.

**D-F7-04 — a compilação do filtro é preguiçosa, e isso é paridade, não
otimização.** No legado a expressão regular é compilada dentro do laço sobre os
acertos, então uma expressão inválida sem acerto nenhum jamais entra em pânico.
Antecipar a compilação — a ordem natural em Go — quebra 69 das 1.378
combinações. É **INV-P23**.

**D-F7-05 — o cache de filtros é otimização pura e não fica atrás de chave.** Não
muda resultado nenhum: resolve o achado A08, em que o legado recompila a
expressão uma vez por página que casou. Cresce sem limite de propósito — a chave
é `expressao_nm`, cujo número de valores distintos é limitado pela tabela.

**D-F7-06 — a ordem do resultado é por página crescente.** O legado devolve na
ordem do `TopDocs` (por pontuação, que ele ignora) e ordena por página na linha
seguinte, `main.rs:285`. Como uma página nunca aparece duas vezes, as duas ordens
convergem para a mesma sequência.

### Um erro que o teste de propriedade pegou, e um que ele quase não pegou

O corpus dourado passou **1.378 de 1.378** com uma implementação que tinha duas
divergências silenciosas. Quem as encontrou foi o teste de propriedade:

| Rodada | Divergências silenciosas | Causa |
|---|---|---|
| 1ª | 13 em 250.000 | `\S` não traduzido — o `\S` do RE2 aceita `NBSP`, `\v` e `U+3000`, o do Rust não |
| 2ª | 2 em 250.000 | classe aninhada `[a[bc]]` — o Rust lê união, o RE2 lê `[` literal |
| 3ª | 0 em 250.000 | — |

A segunda correção quase não pegou: o teste unitário `[a[bc]]` **passou**
indevidamente porque `traduzirClassesPerl` tinha um caminho rápido
`if !strings.Contains(padrao, "\\")` que pulava a varredura inteira em padrões
sem barra invertida. As duas divergências que o teste de propriedade viu tinham
`\S` por acaso. O caso de teste escrito à mão é que expôs o caminho rápido.

**Lição registrada:** um caminho rápido é uma segunda implementação da função, e
precisa de teste próprio.

### Correção de uma estimativa errada

`estimarTermosDistintos` dimensionava o mapa de ocorrências linearmente nos bytes
do texto. A medição mostrou superestimativa de **420 vezes** num documento de 500
páginas — 220.522 posições para 524 termos distintos —, o que sozinho respondia
por dezenas de megabytes de mapa ocioso.

O erro é **estrutural, não de calibração**: vocabulário cresce de forma sublinear
no volume de texto (lei de Heaps), então nenhum divisor constante serve.

| Corpus | Texto | Termos | Distintos |
|---|---|---|---|
| corpus real da F0 | 24 KB | 3.359 | 330 |
| 500 páginas sintéticas | 2,6 MB | 423.000 | 524 |

Substituída por capacidade inicial fixa de 4.096, com o crescimento amortizado do
mapa. Dimensionar de verdade depende de medir vocabulário em diário oficial real
— **D-11**.

### Medições

```
go test ./test/parity/ -run TestBuscaFrase       1.378 combinações, zero divergências
go test -run TestPropriedadeFiltroOperador       250.000 casos realistas, zero divergências
go test -run TestCaracterizacaoSintaxeDivergente 250.000 adversariais, zero divergências
                                                 DE RESULTADO
go test ./internal/adapter/searchidx/ -race -cover        97,6%
golangci-lint run ./...                                   0 issues
```

| Medida | Valor | Referência do legado |
|---|---|---|
| Memória **retida** pelo índice, 500 páginas / 2,5 MB de texto | **8,1 MB** | escritor de **500 MB** (`main.rs:519`, achado A04) — **1,6%** |
| Memória movimentada na construção | 43,9 MB | — |
| Construção do índice, 500 páginas | 61,4 ms | — |
| `Frase`, termo único | 26,5 µs | — |
| `Frase`, frase de dois termos | 189 µs | — |
| `Frase`, com filtro `&` sobre 500 acertos | 57,2 ms | o legado ainda **recompila** a expressão 500 vezes |
| `CompilarFiltro` sem cache | 7,13 µs | — |
| `CompilarFiltro` com cache | 21,1 ns | **338× mais rápido** — é o achado A08 |

### Como se provou que o arnês morde

Quatro sabotagens, cada uma revertida em seguida:

| Sabotagem | Detecção |
|---|---|
| busca de frase virando `strings.Contains` | 27 combinações divergentes de 1.378 |
| `StripExtended` virando identidade | 10 combinações; a página 2 de `08-inv-p01-e-comercial` (`ACME&FILHOS`) some |
| compilação do filtro antecipada | 69 combinações divergentes |
| `\s` nativo do RE2 no lugar da classe traduzida | `TestCompilarFiltroINVP01` falha em `NBSP`, `\v` e `U+3000` |

### Pendências que entram na F8

1. **D-06** deixou de bloquear F7 e passou a bloquear **F8**: a busca devolve
   `ErrExpressaoInvalida` e quem escolhe entre status −1 e o travamento em 3 é a
   máquina de estados.
2. **D-05** idem: reproduzir INV-P17 exige um teste de aspas **antes** da busca,
   no caso de uso. Se nenhuma expressão de produção tem `"`, esse teste é código
   morto e não deve ser escrito.
3. **D-11** segue bloqueante, agora também para dimensionar o vocabulário.
4. **D-15** segue bloqueante.
5. **INV-P10** continua sem exercício (herdado da F5).
6. As demais de F0: **D-14**, **D-08**, **D-10**, **D-13**.

### Dependências novas

Nenhuma em Go — o índice usa só a biblioteca padrão. No ferramental em Rust,
`regex-syntax` passou a dependência direta de `tools/capturar-corpus`: já vinha
transitivamente com `regex`, e fixá-la não muda a resolução, apenas torna a API
acessível à sonda do modo `x`.

---

## F8 — Pipeline de processamento e máquina de estados

**Entregue.** `internal/usecase` e `internal/platform/worker` deixaram de ser
apenas `doc.go`. A closure de 80 linhas que o legado dispara de dentro do
manipulador HTTP virou dois casos de uso e um executor, todos exercitáveis com
dublês — sem banco, sem PDF e sem rede.

| Arquivo | Conteúdo |
|---|---|
| `internal/usecase/processar.go` | `Pipeline`, o laço de recorte e a deduplicação por perfil |
| `internal/usecase/ingerir.go` | `Ingestao`, o caminho síncrono da submissão |
| `internal/usecase/portas.go` | portas de `Metricas` e `Executor`, definidas pelo consumidor |
| `internal/platform/worker/pool.go` | `Pool`: teto opcional, drenagem por notificação, recuperação de pânico |
| `internal/domain/errors.go` | `ErroDeValidacao`, que carrega as críticas até a camada HTTP |

### A sequência, transcrita antes de codificar

O procedimento da fase exige transcrever a sequência do legado como lista de
chamadas às portas e conferi-la com `ESPECIFICACAO.md` §3.4 e §5.1 antes de
escrever código. A lista virou o teste `TestSequenciaDeChamadas`, que compara
item a item:

```
Importacao.MarcarInicio(42)                 main.rs:260 — data_inicio ANTES do status
Importacao.AtualizarStatus(42, selecionado) main.rs:261
Importacao.AtualizarStatus(42, indexando)   main.rs:268 — ANTES de indexar
Extrator.ExtrairPaginas(bytes=15)           main.rs:479-506  ┐ criar_indice
Indexador.Construir(paginas=2)              main.rs:508-535  ┘
Importacao.AtualizarStatus(42, recortando)  main.rs:272
Perfil.ChavesPesquisa(42)                   main.rs:274
Indice.Frase("ALFA")                        main.rs:283      ┐
Recorte.Salvar(perfil=7, "ALFA", n=1)       main.rs:310      │ laço, uma chave
Indice.Frase("BETA")                        main.rs:283      │ de cada vez
Recorte.Salvar(perfil=7, "BETA", n=1)       main.rs:310      ┘
Importacao.AtualizarStatus(42, finalizado)  main.rs:325
Importacao.MarcarTermino(42, 2)             main.rs:326
Indice.Fechar()                             sem equivalente — o Tantivy cai com a tarefa
```

Uma conferência mudou o código: no legado o registro `Há N recorte(s) a filtrar`
sai **depois** do reinício do conjunto por perfil e usa a contagem **bruta**,
antes da deduplicação (`main.rs:291-293`). A ordem natural em Go seria filtrar
primeiro e registrar o resultado.

### Decisões

**D-F8-01 — o prompt da fase pedia −1 no pânico; a especificação pede o
contrário, e as duas coisas foram conciliadas.** O enunciado de F8 manda
"marcar a importação como −1" ao recuperar um pânico. `ESPECIFICACAO.md` §3.5 e
D-06 dizem que o legado deixa a importação **presa** no último status. A
contradição é aparente e some quando se separam dois casos:

| Situação | No legado | Em Go |
|---|---|---|
| Expressão `&` que não compila | `.unwrap()` → **pânico** → presa em 3 | `ErrExpressaoInvalida` → **presa em 3** (D-06) |
| Pânico de verdade (defeito nosso) | inalcançável | registra a pilha e grava **−1** |

O único pânico alcançável em Rust virou erro tipado na F7. Um pânico Go restante
é defeito de programação, sem comportamento legado a preservar — e travar a linha
em silêncio por causa dele seria a pior das opções. Os dois caminhos têm teste.

**D-F8-02 — D-01 saiu da lista de pendências.** O sentinela explícito (booleano
de primeira iteração) foi implementado e `TestINVP13PerfilZeroNaoAlteraOResultado`
prova a equivalência com o zero literal. A resposta continua útil para inventário,
mas não muda mais nenhuma linha.

**D-F8-03 — INV-P17 é reproduzido no caso de uso, não no índice.** A busca em Go
não tem analisador de consulta e não falharia com aspas; a verificação está em
`Pipeline.buscar`, antes da consulta. É o padrão provisório de D-05 —
reproduzir a falha. São seis linhas, marcadas para remoção conjunta se a
resposta for "não existem expressões com aspas".

**D-F8-04 — a drenagem acorda por notificação, mas o aviso mantém a cadência do
legado.** O laço de `main.rs:71-74` sonda a cada 100 ms e registra a cada volta.
`Pool.Drenar` usa `sync.WaitGroup` e retorna assim que a última tarefa termina;
o aviso `drenando importações` continua saindo a cada 100 ms para não mudar o
volume de registro no encerramento. Contexto sem prazo reproduz a espera
indefinida — que é o `DEFEITO PRESERVADO` de §6.3.

**D-F8-05 — a tarefa recebe um contexto desligado do da requisição.**
`Submeter` tem dois: um governa a espera por vaga, outro a execução. No legado a
tarefa sobrevive à resposta HTTP; cancelar o processamento porque o cliente
desconectou seria comportamento novo. `TestIngestaoTarefaSobreviveAoCancelamentoDaRequisicao`
e `TestContextoDaTarefaEIndependenteDoDaSubmissao` fixam isso.

**D-F8-06 — a porta de métricas foi definida no caso de uso, não importada de
`observability`.** A regra de dependência proíbe `internal/usecase` de importar
`internal/platform`. A interface `usecase.Metricas` tem quatro métodos e um
padrão nulo; ligá-la ao registro Prometheus é trabalho da raiz de composição,
na fase F10.

### Como se provou que os testes mordem

Seis sabotagens, cada uma revertida em seguida:

| Sabotagem | Detecção |
|---|---|
| status `indexando` gravado depois de indexar | `TestSequenciaDeChamadas`, chamadas 2, 3 e 4 |
| conjunto de páginas reiniciado a cada chave | `TestINVP12MesmoPerfilMesmaPagina` (2 gravações em vez de 1) e o teste de três perfis |
| laço de chaves paralelizado | 3 corridas de dados sob `-race` mais falha de resultado |
| falha de gravação não encerrando a importação | `TestINVP14` (4 gravações permaneceram em vez de 2) |
| `WaitGroup.Add` movido para dentro da goroutine | `TestDrenarAguardaTodasAsTarefas` (43 de 50 tarefas) |
| expressão inválida gravando −1 | `TestD06...DeixaAImportacaoPresa`, nos três critérios |

### Medições

```
go test ./internal/usecase/... -race -cover -shuffle=on           98,4%
go test ./internal/platform/worker/... -race -cover -shuffle=on   98,2%
golangci-lint run ./...                                           0 issues
```

Ambas acima do mínimo de 85% exigido pela fase, sem nenhum banco e nenhum PDF
real. O que não está coberto são as duas guardas de estouro numérico, que exigem
mais de dois bilhões de recortes.

### Uma exclusão de linter acrescentada

`internal/domain/domaintest/` passou a ser excluído do `forbidigo`: o dublê de
`RepositorioRecorte` precisa **entrar em pânico sob demanda**, que é como se
verifica que o pipeline e o executor sobrevivem a um defeito de programação. O
pacote não vai para produção — nada fora de `_test.go` o importa.

### Pendências que entram na F9

1. **D-08** e **D-10** (captura empírica de 404/405 e do `Content-Type` exato)
   passam a ser o caminho crítico: são de F9 e continuam sem resposta.
2. **D-07** (ramo morto de `main.rs:226-230`) é de F9.
3. **D-05** e **D-06** continuam abertas, agora com o padrão provisório
   implementado e testado. D-06 passou a apontar para F11.
4. **D-11** e **D-15** seguem bloqueantes.
5. **INV-P10** continua sem exercício (herdado da F5).
6. As demais de F0: **D-13**, **D-14**.

### Dependências novas

Nenhuma. O caso de uso usa apenas a biblioteca padrão e o domínio; o executor
usa `context`, `sync` e `log/slog`.

---

## F9 — Camada HTTP e contrato de resposta

**Entregue.** `internal/adapter/httpapi` deixou de ser `doc.go`: roteador,
cadeia de middleware, análise de multipart, camada de resposta com duas
implementações e servidor com tempos limite explícitos.

| Arquivo | Conteúdo |
|---|---|
| `httpapi/router.go` | rotas, cadeia, manipuladores, normalização de caminho |
| `httpapi/catcher.go` | 404 e 405 do Salvo, com negociação de conteúdo |
| `httpapi/multipart.go` | análise fiel das partes, incluindo a divergência do `filename` |
| `httpapi/servidor.go` | `http.Server` com os cinco tempos limite |
| `httpapi/middleware/middleware.go` | identificador, registro, recuperação, limite, autenticação |
| `httpapi/resposta/resposta.go` | texto (padrão) e problem+json (atrás de chave) |
| `tools/sonda-http/` | **a sonda que resolveu D-07, D-08 e D-10** |

### A sonda, e por que ela mudou a fase

D-08 e D-10 estavam abertas desde a F0 com o rótulo "captura empírica" e
bloqueavam esta fase. Em vez de adotar o padrão provisório, `tools/sonda-http`
reconstrói o roteador de `main.rs:52-60` **com o Salvo de verdade** e pergunta.

O que a medição devolveu foi bem mais específico do que "o padrão do roteador",
e três achados mudaram o código:

**1. O catcher negocia conteúdo.** O corpo do 404 e do 405 depende do `Accept`:
HTML de 905/944 bytes por padrão, JSON, texto ou XML conforme o cabeçalho. O
`http.NotFound` do Go devolveria `404 page not found\n` em `text/plain` para
todos os casos — corpo, tipo e negociação errados de uma vez.

**2. O caminho é normalizado por segmentos.** `/ping/`, `/ping//`, `//ping`,
`/ping///` e `/./ping` respondem `pong`; `/ping/x` não. O `ServeMux` do Go
devolveria 404 para as cinco primeiras. **Sem essa descoberta, seis formas que
hoje funcionam passariam a falhar.**

> ⚠ **CORRIGIDO DEPOIS.** Deste parágrafo, a sonda da F9 mediu **`/ping/`**. As
> outras quatro formas foram inferidas da leitura do Salvo e escritas aqui como
> se tivessem sido medidas — e **`/./ping` estava errada: responde 404**. A
> segunda rodada de medição, no fim deste documento, mediu 45 casos e corrigiu
> 11 divergências. O parágrafo fica como está por ser o registro do que a F9
> concluiu; a conclusão certa está em "Segunda rodada de medição do caminho".

**3. `GET /pdf` sem chave devolve 405, não 401.** O método perde antes da
autenticação — o hoop nem roda.

### A divergência que só apareceu porque a sonda foi ao multipart

Medindo a classificação da parte `pdf`, os dois analisadores discordam:

| `Content-Disposition` | Salvo | `ParseMultipartForm` do Go |
|---|---|---|
| `filename="diario.pdf"` | arquivo | arquivo |
| `filename=""` | **arquivo**, nome `""` → **200** | **valor** → 400 `PDF não enviado` |
| sem `filename` | valor → 400 | valor → 400 |
| `filename=" "` | arquivo | arquivo |

`Part.FileName()` do Go devolve a cadeia vazia nos dois casos que precisam ser
distinguidos. A camada HTTP analisa as partes à mão e testa a PRESENÇA do
parâmetro `filename`, que é a regra do Salvo.

E daí saiu um achado de espeficicação: **`PDF não possui nome` (main.rs:207) é
inalcançável** — sem `filename` não há arquivo, e com `filename` sempre há nome.
É o segundo ramo morto do manipulador, ao lado do de 226-230.

### Decisões

**D-F9-01 — o HTML do catcher é reproduzido BYTE A BYTE, rodapé do Salvo
incluído.** O serviço em Go passa a anunciar um arcabouço em Rust que não usa
mais. É correto pela regra do projeto — preservar corpo de resposta — e estranho
na prática. Registrado como **D-20**, com a observação de que convém decidir
junto com D-15: se a versão de produção do Salvo tiver outro HTML, o byte a byte
atual está errado de qualquer forma.

**D-F9-02 — o teste de tempo constante NÃO garante o achado A02, e isso foi
medido.** O critério de aceite pedia 10.000 medições com prefixos de 0, 8, 16 e
32 bytes corretos. O teste existe e passa — mas passa **também com `==` de
cadeia**, verificado por sabotagem: as medianas ficam em 743, 791, 786 e 739 ns
nos dois casos. A comparação inteira de 36 bytes custa poucos nanossegundos
dentro de uma requisição de ~750 ns, e o `==` do Go usa `memequal`, palavra a
palavra. O sinal fica duas ordens de grandeza abaixo do ruído.

Quem garante A02 é `TestComparacaoDeChaveUsaTempoConstante`, **estrutural**:
analisa a árvore sintática do middleware e exige a chamada a
`subtle.ConstantTimeCompare`, recusando comparação por igualdade da credencial.
Esse **falha** com a sabotagem. O teste de tempo fica como detector de regressão
grosseira e como registro da medição.

**D-F9-03 — os tempos limite de leitura e escrita nascem em ZERO.** Qualquer
valor finito cortaria o envio de um diário grande por enlace lento, que é
mudança de comportamento observável — e o relógio de escrita do Go começa a
contar na leitura do cabeçalho, então limitar a escrita limitaria também o
envio. `ReadHeaderTimeout` fica em 10 s, que fecha a porta ao cabeçalho lento
sem tocar no corpo. Fechar os dois zeros depende de **D-04**.

**D-F9-04 — pânico vira 500, que é comportamento NOVO.** No Salvo o pânico
derruba a tarefa da conexão e o cliente recebe a conexão fechada, sem resposta.
Responder 500 com corpo VAZIO é deliberado: qualquer texto seria invenção, e a
alternativa é o cliente não distinguir "o serviço caiu" de "a rede caiu".

**D-F9-05 — o identificador de requisição é ECOADO na resposta.** O Salvo só o
define na requisição. Devolvê-lo não altera corpo nem código e é o que permite a
quem chamou correlacionar sua requisição com o registro do serviço.

### Risco aceito, registrado

**Os dois textos de 401 continuam distintos** (D-09). `Faltou a X-API-KEY` e
`X-API-KEY inválida` revelam se a chave existe — é divulgação de informação, e é
contrato existente. Unificá-los quebraria clientes que hoje distinguem os casos.
Fica como está, e este parágrafo é o registro do risco que o prompt da fase pede.

### Como se provou que os testes mordem

Cinco sabotagens, cada uma revertida em seguida:

| Sabotagem | Detecção |
|---|---|
| catcher trocado por `http.NotFound`/`http.Error` | 24 divergências de corpo e tipo |
| normalização de barras removida | 4 rotas que hoje respondem `pong` viram 404 |
| `ParseMultipartForm` no lugar da análise manual | `filename=""` vira 400 em vez de 200 |
| os dois textos de 401 unificados | corpo diverge byte a byte, 18 contra 19 bytes |
| `subtle.ConstantTimeCompare` trocado por `==` | **o teste de tempo NÃO pega**; o estrutural pega |

A quinta linha é o achado da fase: uma sabotagem que o teste "óbvio" não detecta.

### Medições

```
go test ./internal/adapter/httpapi/... -race -cover -shuffle=on    92,2%
golangci-lint run ./...                                            0 issues
```

Tabela de contrato verificada byte a byte, oito respostas:

| resposta | origem | status | Content-Type |
|---|---|---|---|
| `GET /ping` | main.rs:112-115 | 200 | `text/plain; charset=utf-8` |
| R1 sem `X-API-KEY` | main.rs:125-128 | 401 | `text/plain; charset=utf-8` |
| R2 chave diferente | main.rs:120-123 | 401 | `text/plain; charset=utf-8` |
| R3 críticas | main.rs:218-224 | 400 | `text/plain; charset=utf-8` |
| R4 falha ao registrar | main.rs:239-244 | 422 | `text/plain; charset=utf-8` |
| R5 sucesso | main.rs:339-340 | 200 | `text/plain; charset=utf-8` |
| R6 rota inexistente | catcher, medido | 404 | `text/html` |
| R7 método não permitido | catcher, medido | 405 | `text/html` |

### Um teste da fase F8 estava errado, e o `-shuffle=on` pegou

`TestSemTetoNaoBloqueia` media o "pico de concorrência" observado e exigia pelo
menos 2. Era um mau substituto da propriedade: sem teto o executor não IMPEDE o
paralelismo, mas também não o GARANTE — o escalonador pode rodar as goroutines
em sequência, e aí o pico é 1.

Reescrito com uma BARREIRA: cada uma das 200 tarefas só termina depois que todas
as 200 chegaram. Com teto menor que 200 isso travaria; sem teto, conclui. Agora
o teste mede a propriedade em vez de um efeito colateral dela, e passa em 15
execuções seguidas sob `-race -shuffle=on`.

### Decisões abertas que fecharam

- **D-07** resolvida: o ramo morto não é portado, e um SEGUNDO ramo morto foi
  descoberto (`PDF não possui nome`).
- **D-08** resolvida por medição, com muito mais detalhe do que a pergunta pedia.
- **D-10** resolvida por medição; a inferência da especificação estava certa.
- **D-20** aberta: o rodapé "salvo" nas páginas de erro.

### Pendências que entram na F10

1. **D-11** e **D-15** seguem bloqueantes. D-15 ganhou peso: a versão do Salvo
   determina se o HTML do catcher está certo.
2. **D-04** passou a ter consequência de código: é ela que permite fechar os
   tempos limite de leitura e escrita.
3. **D-05**, **D-06**, **D-20**: abertas, com padrão provisório implementado.
4. **INV-P10** continua sem exercício (herdado da F5).
5. As demais de F0: **D-13**, **D-14**.

### Dependências novas

Nenhuma em Go — a camada HTTP usa só a biblioteca padrão. No ferramental,
`tools/sonda-http` acrescenta `salvo 0.95` e `tokio`, isolados num módulo
próprio que não entra no binário do serviço.

---

## F10 — Ciclo de vida, sinais e drenagem

**Entregue.** `internal/app` e `internal/platform/shutdown` deixaram de ser
`doc.go`, e `cmd/recorte-api/main.go` ficou sem lógica: carrega a configuração,
monta, executa e traduz o erro em código de saída.

| Arquivo | Conteúdo |
|---|---|
| `internal/app/app.go` | montagem das 15 peças, `Executar`, `Encerrar`, adaptadores da raiz |
| `internal/platform/shutdown/shutdown.go` | escuta de sinais, primeiro e segundo |
| `cmd/recorte-api/main.go` | configuração → montagem → execução → código de saída |
| `test/e2e/ciclo_de_vida_test.go` | o binário de verdade, com sinais reais |

### O grafo de construção, apresentado antes de codificar

```
 1. registrador                observability.NovoLogger
 2. rastreamento               observability.IniciarTracing
 3. métricas                   observability.NovasMetricas
 4. pool de conexões           postgres.NovoPool          ← primeiro recurso externo
 5. unidade de trabalho        postgres.NovaUnidadeDeTrabalho
 6. repositório de importação  postgres.NovoRepositorioImportacao
 7. repositório de perfil      postgres.NovoRepositorioPerfil
 8. repositório de recorte     postgres.NovoRepositorioRecorte
 9. extrator de texto          pdftext.NovoExtrator
10. indexador                  searchidx.NovoIndexador
11. pipeline                   usecase.NovoPipeline
12. executor                   worker.NovoPool
13. ingestão                   usecase.NovaIngestao
14. roteador                   httpapi.NovoRouter
15. servidor                   httpapi.NovoServidor
```

E a ordem de encerramento, que **não é a inversa**:

```
1. servidor HTTP   para de aceitar; o que está em curso termina
2. importações     drena o executor
3. pool do banco   fecha
4. telemetria      esvazia
```

O servidor para **primeiro** de propósito: enquanto ele aceitar requisições,
importações novas continuam entrando e a drenagem pode nunca acabar.

### Decisões

**D-F10-01 — o segundo sinal força a saída, e é comportamento NOVO.** O legado
chama `stop_graceful(None)` no primeiro sinal e ignora os seguintes: diante de
uma importação travada, o operador só tem SIGKILL. Aceitar o segundo dá a ele
uma saída ordenada, com registro do que ficou pendente. Só é observável em
emergência — quando quem opera já decidiu não esperar.

**D-F10-02 — códigos de saída distintos.** O legado tem dois desfechos: sai
limpo ou entra em pânico. Aqui: `0` limpo, `1` falha de arranque, `2` teto de
encerramento estourado, `130` segundo sinal. É o que permite a um supervisor
decidir se reinicia.

**D-F10-03 — `SHUTDOWN_TIMEOUT` continua em zero.** Zero é espera indefinida,
que é o `DEFEITO PRESERVADO` de §6.3. Definir a chave é evolução.

**D-F10-04 — a drenagem acorda por notificação, o aviso mantém a cadência.**
Herdado da F8: `sync.WaitGroup` no lugar da sondagem de 100 ms, com o aviso
`drenando importações` ainda a cada 100 ms para não mudar o volume de registro.

**D-F10-05 — o motivo do encerramento nomeia o sinal.** O legado distingue
SIGINT de SIGTERM com mensagens diferentes (§6.2). `shutdown.Escuta.Motivo` é
injetado no `App`, e o registro sai com `motivo="sinal SIGTERM"`.

### Três defeitos meus que os testes pegaram

**1. Corrida no endereço efetivo.** `Executar` sobrescrevia `servidor.Addr` com
a porta que o sistema escolheu, enquanto `Endereco()` a lia de outra goroutine.
O `-race` acusou. Corrigido com `atomic.Pointer` num campo próprio: o
`http.Server` não é mutado depois de entregue às suas goroutines.

**2. O segundo sinal deixava o ouvinte aberto.** `Executar` devolvia
`ErrEncerramentoForcado` direto, sem passar por `Encerrar` — e o servidor
continuava escutando num processo que estava saindo. Agora a força atravessa
`Encerrar`, que corta o prazo de cada etapa mas ainda fecha o ouvinte.

**3. A goroutine do servidor sobrevivia ao retorno.** `Executar` não esperava
`Serve` terminar. O `goleak` acusou.

**4. O ouvinte de sinais perdia o segundo sinal.** Depois do primeiro, o laço
selecionava em `ctx.Done()` — que acabara de fechar — e saía antes de o segundo
chegar. Corrigido com um canal de encerramento próprio, separado do contexto.

### Uma sabotagem que o teste "óbvio" não pegava

Inverter as duas primeiras etapas do encerramento — drenar antes de parar o
servidor — **passava** em todos os testes de ordem. A razão é que o teste
observava a sequência de anotações, e a anotação do servidor não estava lá.

`TestServidorParaANTESDaDrenagem` monta o cenário real: um manipulador que
submete importação a cada requisição, e uma requisição disparada **durante** a
janela de drenagem. Com a ordem certa a conexão é recusada; com a invertida ela
é atendida e a drenagem ganha trabalho novo. Esse teste **falha** com a
sabotagem.

### Uma sabotagem que continua sem teste que morda, e por quê

Remover a espera pela goroutine do servidor (`<-erros`) **não** é detectado pelo
`goleak`: ele tenta de novo com recuo por algumas centenas de milissegundos, de
propósito, para não acusar goroutine que está justamente terminando. A goroutine
do `Serve` termina nessa janela.

A espera continua no código porque é correta — `main` chama `os.Exit` logo
depois, e sair com uma goroutine de servidor em andamento é cortá-la no meio.
Mas é justo registrar que ela está justificada por raciocínio, não por teste que
morde. É a mesma classe do teste de tempo constante da fase F9.

### Medições

```
go test ./internal/app/... -race -cover -shuffle=on              79,1%
go test ./internal/app/... -tags integration -race -cover        94,6%
go test ./internal/platform/shutdown/... -race -cover           100,0%
go test ./test/e2e/... -tags integration                        7 testes, todos passando
golangci-lint run ./...                                          0 issues
```

Os testes de ponta a ponta **compilam o binário, enviam sinais reais e conferem
o código de saída** — SIGTERM e SIGINT encerram com 0, banco inalcançável sai
com 1, configuração inválida sai com 1, e a sonda `-healthcheck` da imagem
responde contra um serviço vivo.

### Uma dependência nova, e a justificativa

| Dependência | Justificativa |
|---|---|
| `go.uber.org/goleak` | Só de teste. É o critério de aceite explícito da fase — "goleak não detecta goroutine remanescente" — e está instalado no `TestMain` do pacote inteiro, não em um teste só: qualquer teste que deixe goroutine viva derruba a execução. Não entra no binário. |

### Um alvo do Makefile consertado

`make pg-subir` parou de funcionar: o PostgreSQL sobe, tenta criar o arquivo de
trava do soquete em `/var/run/postgresql` — que o usuário sem privilégio não
escreve — e morre. Corrigido com `-k /tmp/pgsock-recorte`. Sem isso, nenhum
teste de integração da F4 em diante roda neste ambiente.

### Pendências que entram na F11

1. **D-11** e **D-15** seguem bloqueantes.
2. **D-04** continua sendo o que permite fechar os tempos limite de leitura e
   escrita do servidor, hoje em zero.
3. **D-05**, **D-06**, **D-20**: abertas, com padrão provisório implementado.
4. **INV-P10** continua sem exercício (herdado da F5).
5. As demais de F0: **D-13**, **D-14**.
6. As chaves de evolução da F11 — `IDEMPOTENCIA_POR_HASH`, `GRAVACAO_EM_LOTE`,
   `VALIDAR_ASSINATURA_PDF`, `VARREDURA_ORFAS`, `RATE_LIMIT_RPS`,
   `STATUS_ENDPOINT`, `HEALTH_ENDPOINTS` — já existem na configuração, são lidas
   e ainda não têm efeito. É o trabalho da próxima fase.

---

## F11 — Evoluções técnicas atrás de chaves

**Objetivo.** Endereçar os achados restantes como capacidades opcionais. Toda
evolução nasce desligada, e o serviço com todas as chaves no padrão continua
indistinguível do legado.

### As dez chaves, e onde cada uma vive

| Chave | Camada | Arquivo |
|---|---|---|
| `MAX_IMPORTACOES_CONCORRENTES` | executor | `platform/worker/pool.go` (F8) + observador de fila (F11) |
| `MAX_UPLOAD_BYTES` | HTTP | `httpapi/middleware` + `httpapi/multipart.go` |
| `VALIDAR_ASSINATURA_PDF` | **caso de uso** | `usecase/ingerir.go` |
| `GRAVACAO_EM_LOTE` | persistência | `postgres/recorte_repo.go` |
| `IDEMPOTENCIA_POR_HASH` | caso de uso + persistência | `usecase/ingerir.go`, `postgres/consulta_repo.go` |
| `VARREDURA_ORFAS` | caso de uso + plataforma | `usecase/varrer.go`, `platform/periodico` |
| `RESPOSTA_PROBLEM_JSON` | HTTP | `httpapi/resposta` (F9, corrigida aqui) |
| `RATE_LIMIT_RPS` | HTTP | `httpapi/middleware/taxa.go` |
| `STATUS_ENDPOINT` | HTTP + persistência | `httpapi/adicoes.go`, `postgres/consulta_repo.go` |
| `HEALTH_ENDPOINTS` | HTTP | `httpapi/adicoes.go` |

A fiação condicional passa toda por **uma** função na raiz de composição,
`app.seSim`: chave desligada devolve o ZERO do tipo, e porta nula é o que
desliga a evolução. Espalhar esse `if` por sete pontos de montagem seria sete
chances de ligar algo por acidente.

### Três coisas que a fase mudou no que já existia

**1. `RESPOSTA_PROBLEM_JSON` estava trocando o corpo do SUCESSO.** O escritor da
F9 era aplicado a toda resposta, então ligar a chave fazia `/ping` responder
`{"type":"about:blank","title":"OK","status":200,"detail":"pong"}` em vez de
`pong`. A RFC 7807 descreve documento de **problema**; um 200 não é um. Corrigido
com `resposta.EhProblema`, que corta em 400.

O defeito só apareceu no teste de montagem com **todas** as chaves ligadas — a
única configuração em que `RESPOSTA_PROBLEM_JSON` se cruza com uma verificação
do corpo de `/ping`. O teste da F9 afirmava o comportamento errado e foi
invertido.

**2. O 422 de `MAX_UPLOAD_BYTES` virou 400 com crítica.** A F9 deixou 422 como
provisório, antes de D-16 existir. Agora é a opção B da decisão.

**3. `NovoRepositorioRecorte` e `NovoPool` ganharam opções variádicas.** A
chamada sem opção continua válida e continua significando "modo do legado".

### O que a especificação da fase pedia e não pôde ser feito

> "6. VARREDURA DE ÓRFÃS … as **reprocessa** ou marca como −1, conforme política
> configurável."

**Reprocessar é impossível: o serviço não guarda o documento.** `tb_importacao`
tem `nome_original_pdf` e `hash`; os bytes do PDF vivem em memória durante o
processamento e são descartados com a tarefa. Não há gravação em disco, em
objeto nem em coluna binária — nem no legado nem no porte.

Isso foi registrado como **D-21**, com as opções de arquivamento e o custo de
cada uma. As políticas implementadas são `observar` (padrão) e `erro`;
`reprocessar` é recusada pela configuração com mensagem que aponta para a
decisão.

A descoberta é maior que a varredura: **toda importação que falha é perda
definitiva de trabalho**, para qualquer causa — PDF corrompido, expressão
inválida (D-06), queda do processo. A única recuperação é o cliente reenviar.

### Decisões de desenho que merecem registro

**A assinatura de PDF é verificada DEPOIS da validação.** Uma submissão que erra
a data e manda um arquivo que não é PDF continua recebendo o 400 com as
críticas, em vez de trocá-lo por um 422 menos informativo. O efeito da chave se
limita ao que o legado **aceitaria**. E a resposta usa o texto que já existe —
a especificação da fase é explícita: "a MESMA 422 do legado, sem texto novo".

**A idempotência só reaproveita a importação FINALIZADA.** Uma em curso faria o
cliente herdar uma tarefa que ainda pode falhar; uma em −1 impediria justamente
o reenvio que corrige o erro. E falha na consulta **não derruba a submissão**:
banco fora do ar para o `SELECT` segue o caminho do legado. A evolução é
otimização, não regra de negócio.

**A chave de idempotência tem três campos**, não só o hash: o mesmo arquivo pode
ser submetido legitimamente para outro caderno ou outra data.

**O lote confere a ordem em vez de presumi-la.** `WITH ORDINALITY` mais
`ORDER BY` faz o PostgreSQL inserir na ordem da entrada e o `RETURNING` sair na
ordem de inserção — mas a página volta junto e é **comparada** com a esperada.
Uma reordenação futura vira erro na hora, em vez de texto de recorte associado à
página errada, que é corrupção silenciosa.

**A varredura busca e trata na MESMA transação.** A trava de
`FOR UPDATE SKIP LOCKED` só vale até o commit; soltá-la antes de gravar abriria
a janela em que outra instância pega a mesma linha.

**A política padrão da varredura é `observar`.** Uma varredura que já nasce
escrevendo pode marcar como erro um lote de importações que estavam apenas
lentas — e não há como desfazer.

**`/health/live` não consulta nada.** Uma sonda de vivacidade que dependa do
banco faz o orquestrador REINICIAR o serviço quando o banco cai: não conserta o
banco e derruba as importações em andamento.

**O limitador de taxa usa a chave de API, não o IP.** Atrás de balanceador o IP
é o do balanceador. O balde é por processo, então com N instâncias o limite
efetivo é `N × RATE_LIMIT_RPS` — está em `OPERACAO.md`.

### As sabotagens que provaram os testes

| Sabotagem | Teste que pegou |
|---|---|
| Remover `SKIP LOCKED` da consulta de órfãs | `TestIntegracaoDuasInstanciasNaoPegamAMesmaImportacao` — a segunda instância bloqueia e o prazo de 15 s a converte em falha, não em travamento |
| Trocar `ORDER BY entrada.ordem` por `ORDER BY nr_pagina DESC` no lote | `TestIntegracaoLoteProduzOMesmoEstadoQueLinhaALinha` e `TestIntegracaoLotePreservaAOrdemComPaginasEmbaralhadas` — a conferência de pareamento acusa antes de gravar |

O teste de concorrência recebeu um **prazo por goroutine** exatamente para que a
sabotagem produza falha limpa: sem ele, um `FOR UPDATE` simples faria a segunda
instância esperar pela primeira, que espera na barreira, e o teste penduraria
até o tempo limite do `go test`.

Um detalhe do próprio teste teve de ser corrigido: com lote **igual** ao acervo,
a primeira instância leva tudo e a segunda volta vazia — comportamento correto,
mas que não exercita disputa alguma. O lote é metade, como em produção.

### Migração

`db/migrations/0002_idempotencia_indice.{up,down}.sql`. **Nenhum dos dois roda
dentro de transação** — `CREATE INDEX CONCURRENTLY` e `DROP INDEX CONCURRENTLY`
são recusados em bloco transacional.

O índice **não é único**: um banco de produção já tem duplicatas, porque o
legado nunca deduplicou. Um índice único falharia na criação e, passando,
recusaria reenvios que hoje são aceitos — mudança de comportamento com a chave
**desligada**.

### Um alvo do Makefile consertado

`make test-integration` roda `./...`, e os pacotes de teste do Go rodam **em
paralelo entre si**. `internal/adapter/postgres` faz `DROP SCHEMA recorte
CASCADE` a cada teste, no MESMO banco que `internal/app` e `test/e2e` consultam.

Até esta fase ninguém notava: o teste de montagem da F10 só exercitava `/ping` e
o 401, que não tocam em tabela. O teste novo consulta `tb_importacao` — e uma
execução falhou, sem se reproduzir nas onze seguintes. A janela é curta e o
sintoma seria um 500 onde o teste espera 404.

Corrigido com `-p 1`, que serializa os pacotes. Um banco por pacote seria a
alternativa; serializar custa alguns segundos e não exige infraestrutura nova.

### Medições

```
go build ./...                                                   OK
golangci-lint run ./...                                          0 issues
go test ./... -race                                              todos os pacotes passando
go test ./... -tags integration                                  todos os pacotes passando
npx @redocly/cli lint api/openapi.yaml                           válido, 0 avisos
test/parity (todas as chaves no padrão)                          passando
```

Nenhuma dependência nova.

### Documentação entregue

| Documento | Conteúdo |
|---|---|
| `api/openapi.yaml` | Contrato atual e adições, com `x-chave` marcando o que depende de configuração |
| `docs/OPERACAO.md` | Tabela de chaves, ordem de ativação, valores por porte de carga, o cálculo de memória do teto, métricas e alarmes |

### Pendências que entram na F12

1. **D-11** e **D-15** seguem bloqueantes — e agora bloqueiam a própria F12.
2. **D-04** governa os valores recomendados de `OPERACAO.md` e continua aberta.
3. **D-16** implementada com a opção B; falta a confirmação de produto, e o
   enunciado da decisão foi corrigido (a crítica sai sozinha, não ao final da
   lista das cinco).
4. **D-21** aberta, e é a de maior alcance das novas: sem arquivar o documento,
   nenhuma importação falha é recuperável.
5. **D-05**, **D-06**, **D-13**, **D-14**, **D-18**, **D-20**: abertas.
6. **INV-P10** continua sem exercício (herdado da F5).

---

## F12 — Verificação de paridade e execução em sombra

**Objetivo.** Provar equivalência com evidência, não com confiança. É o portão
do projeto.

**Resultado.** A verificação determinística passou — depois de encontrar e
corrigir **dois defeitos graves**. A execução em sombra **não aconteceu**: o
insumo é D-14, e ele nunca chegou. A recomendação registrada em
`docs/RELATORIO-PARIDADE.md` é **não cortar**.

### Os dois defeitos, e por que seis fases não os pegaram

**1. A normalização não era aplicada em produção.** A raiz de composição
injetava `pdftext.NovoExtrator()`, que devolve texto BRUTO. O serviço indexava
sem junção de hífens e sem remoção de diacríticos, e `pdftext.Normalizar` — com
um milhão de casos de teste de propriedade na F6 — era **código morto no
caminho de produção**. INV-P02 e INV-P07 quebrados; INV-P19 **invertida**.

Cinco dos 28 documentos do corpus divergiam.

**2. O termo longo não deixava buraco na numeração de posições.** No Tantivy a
posição é do tokenizador e o descarte por comprimento é um filtro posterior que
não renumera. O porte numerava pela lista já filtrada, então a frase casava por
cima do termo longo — encontrando ocorrências que o legado nunca encontrou.

**A causa comum é estrutural, e é a lição da fase.** Cada teste de camada se
alimentava do ORÁCULO da anterior, nunca da saída da anterior em Go:

```
F5  extração                      → compara com paginas-brutas.json
F6  normaliza o ORÁCULO bruto     → compara com paginas.json
F7  busca sobre o ORÁCULO         → compara com busca.json
```

O isolamento é correto — ele localiza a causa —, mas deixa a **costura** sem
cobertura. E o oráculo `recortes.json`, que mede o pipeline inteiro, existia
desde a **F0** e **nenhum teste o consumia**.

Um conjunto de testes de unidade todos verdes não diz nada sobre a montagem.

### O que foi construído

| Artefato | O que faz |
|---|---|
| `test/parity/pipeline_test.go` | camadas 4 e 5 — o pipeline REAL contra `recortes.json`; foi o teste que expôs o defeito 1 |
| `tools/comparador` | comparador das cinco camadas, relatório em texto e JSON, redução automática ao menor caso |
| `tools/capturar-corpus` (lib + `oraculo-laco`) | o crate ganhou `lib.rs` para que o oráculo do laço reuse os portes verbatim em vez de duplicá-los |
| `test/parity/propriedade_laco_test.go` | 10.000 documentos gerados contra o oráculo do laço; expôs o defeito 2 |
| `test/e2e/caos_test.go` | SIGKILL por estágio, queda do banco, morte durante a drenagem |
| `test/carga/carga_test.go` | correção sob concorrência e memória por importação |
| `tools/sombra` | comparador de dois bancos e verificação de isolamento |
| `docs/RELATORIO-PARIDADE.md` | a evidência e a recomendação |
| `app.extratorDeProducao` | função nomeada só para existir alvo de teste da MONTAGEM |

### Decisões de desenho que merecem registro

**O portão olha só as camadas 4 e 5.** Uma diferença de termo que não chega a
mudar recorte não altera o conteúdo do banco. As camadas 1 a 3 localizam a
causa; barrar por elas confundiria sintoma com consequência.

**O comparador tem três códigos de saída, não dois.** `2` é "não consegui
medir". Sem essa distinção, um corpus que deixou de ser gerado passaria por
"sem divergência" — e `go run` não serve para invocá-lo, porque engole o código
e devolve 1 para qualquer valor não nulo.

**Na sombra, divergência de TEMPO não reprova.** As duas instâncias processam o
mesmo documento em momentos diferentes por construção; reprovar por isso
reprovaria toda execução e tornaria o portão inútil.

**A verificação de isolamento TENTA escrever.** Uma restrição que ninguém
verifica é uma intenção. E tenta dentro de transação revertida, para não causar
o dano que procura.

**O teste de caos lê o estado ANTES do SIGKILL.** Se a drenagem concluiu a
importação nos milissegundos anteriores, o status 5 é correto e não há morte
súbita a medir. Sem essa guarda o teste acusaria defeito onde houve corrida.

### O que foi medido e contradisse a documentação

`docs/OPERACAO.md` §5 usa a régua de **8× o tamanho do PDF** para memória por
importação. O medido foi **14,3×**. O documento do corpus tem ~100 KiB e o custo
FIXO por importação domina nessa escala, então o número não se transfere para um
diário real — **mas isso não valida os 8×**. A régua ficou marcada como
provisória em `OPERACAO.md`, com remedição pendente de D-11.

### As sabotagens que provaram os testes

| Sabotagem | Quem pegou |
|---|---|
| Raiz de composição volta ao extrator cru | `TestExtratorDeProducaoNormaliza` |
| Índice volta a numerar pela lista filtrada | `TestPropriedadeDoLaco` — 19 de 9.357 casos |
| Comparador aponta para o extrator cru | o próprio relatório, com classe e caso mínimo |

### Desvios do enunciado, declarados

**O caos não afirma que a queda do banco leva a −1.** O legado descarta o erro
das gravações de status com `let _ =` (ESPECIFICACAO §3.6); o −1 vem da falha na
LEITURA das chaves. Derrubar as conexões atinge as duas e qual falha primeiro
depende do instante. O teste afirma o que observa: o processo **sobrevive**.

**"O dobro do pico histórico" pressupõe o pico**, que é D-04 e nunca foi
respondido. O teste usa 500/h como suposição NOMEADA, sobrescritível por
`PICO_HISTORICO_POR_HORA`.

**A latência p99 comparada ao legado não foi medida** — exige os dois serviços
lado a lado em produção.

### Medições

```
make parity                                    APROVADO, 5 camadas, 0 divergência
go test ./test/parity/... -run TestPropriedade 9.357 casos, 0 divergência
make load-test                                 64 simultâneas, 0 divergência
make chaos-test                                todos os cenários equivalentes ao legado
make lint                                      0 issues
golangci-lint --build-tags=carga,integration   0 issues
```

Nenhuma dependência nova.

### Pendências que entram na F13

1. **D-11**, **D-15**, **D-14** — os três insumos que bloqueiam o corte. Nenhum
   é trabalho de engenharia.
2. **D-04** — a régua de memória segue sem validação em documento real.
3. **D-13**, **D-02** — esquema real e `COLLATE`.
4. **INV-P10** continua sem exercício (herdado da F5).
5. A F13 **não pode começar** enquanto o portão não fechar: é a definição de
   portão.

---

## Enxugamento do repositório — depois da F12

**Pedido.** Três coisas: que rodar local não dependa de preparo, que a API KEY
fique fixa como era no Rust, e que saia do repositório tudo o que não é
necessário para o serviço funcionar — mantendo a documentação.

### O que saiu

`reference/`, `tools/` inteiro (captura de corpus, oráculos em Rust, sonda HTTP,
comparador, sombra, geradores) e `test/` inteiro (corpus dourado, oráculos,
paridade, ponta a ponta, carga). Também o job de paridade do CI e os alvos
`parity`, `oraculos`, `corpus`, `sonda-http`, `load-test` e `chaos-test`.

Tudo está no commit **`2febb7a`** e volta com
`git checkout 2febb7a -- test tools reference`.

### O que foi PRESERVADO da remoção, e por quê

**Os testes unitários de `internal/`.** Não foram removidos: rodam com
`go test ./...` sem insumo externo, e são a única rede de segurança que sobra
para quem mexer no serviço depois. Apagá-los é o único item da limpeza difícil
de desfazer — e é um comando, se for o desejado.

**Seis PDFs, como fixture de pacote.** `internal/adapter/pdftext/testdata/`
(cinco) e `internal/app/testdata/` (um). Sem eles, dois testes passariam a
PULAR em silêncio — incluindo `TestExtratorDeProducaoNormaliza`, que é o guarda
do defeito mais caro já encontrado no projeto. Um teste que pula não protege
nada.

**A verificação byte a byte das sete consultas literais.** Ela comparava com
`reference/main.rs` e passou a comparar com **resumos SHA-256 congelados**
(`somaDaConsulta`). O oráculo mudou; a propriedade protegida — nenhuma consulta
pode ser reescrita, reformatada ou "otimizada" — continua exatamente a mesma.
Uma mensagem de falha aponta para `git show 2febb7a:reference/main.rs`.

### Três mudanças de comportamento, todas para destravar a execução local

| Antes | Agora | Por quê |
|---|---|---|
| `SERVIDOR_IP` padrão `192.168.42.1` | `0.0.0.0` | Aquele endereço só existe na rede do serviço original; fora dela o processo NÃO SOBE. `0.0.0.0` é superconjunto — atende também nele quando existe. `config.ServidorIPDoLegado` guarda o valor, e `SERVIDOR_IP` o restaura. |
| `API_KEY` obrigatória | padrão `01956cb2-…` | É o MESMO valor que o Rust trazia em `const API_KEY`. Lá era constante de código; aqui é padrão, e o ambiente vence. |
| `DATABASE_URL` obrigatória | padrão local | Aponta para o banco do `docker-compose.yml`. Fora dali falha ruidosamente no arranque — não causa dano silencioso. |

As duas últimas contrariam a invariante do projeto "nenhum segredo tem padrão
embutido em código", e isso foi **decisão explícita**: o pedido era reproduzir o
que o Rust fazia. O `gosec` sinaliza as duas com G101 e a supressão fica ao lado
da constante, com a justificativa — a decisão está registrada num lugar só, não
espalhada.

**A chave está em claro no repositório.** Não é regressão em relação ao
original, que a embutia no binário, mas também não vira segredo por estar num
YAML. `README.md`, `docker-compose.yml`, `config.APIKeyPadrao`, `OPERACAO.md`
§1.1 e `api/openapi.yaml` dizem isso, nos cinco lugares onde alguém pode
tropeçar nela.

### Execução local, verificada

Com o binário rodando sem NENHUMA variável definida além do endereço do banco de
teste deste ambiente:

```
GET  /ping                    → 200 "pong"
POST /pdf  com a chave padrão → 200 "PDF carregado com sucesso"
POST /pdf  sem chave          → 401 "Faltou a X-API-KEY"
banco: importação 2, status 5, total_recortes 1
       recorte: página 2, perfil 7, "ALFA CONSTRUCOES", texto normalizado
```

O `docker-compose.yml` sobe PostgreSQL e serviço juntos; `db/init/` cria o
esquema (o INFERIDO — D-13 segue aberta, e o arquivo diz isso em letras
grandes) e semeia os perfis que casam com `exemplos/diario-de-exemplo.pdf`.

### Pendência

`docker compose up` **não foi executado** — este ambiente não tem daemon Docker.
O que foi verificado é o binário rodando direto contra PostgreSQL local, que
exercita o mesmo código. O compose é a mesma configuração declarada de outra
forma, mas isso não é o mesmo que tê-lo visto subir.

---

## Segunda rodada de medição do caminho — depois do enxugamento

**Motivada por um relato de campo, não por um teste.** Um `curl` disparado com
espaço sobrando na URL virou `GET /ping%20` e recebeu 404. O log do serviço
mostrava a pista inteira: `"rota":"/ping "`, com espaço ao final.

### O primeiro achado: o 404 estava certo

O legado também responde 404 a `/ping%20`. O relato não era defeito — era um
espaço a mais na linha de comando. Mas a pergunta expôs que
`normalizarCaminho` **afirmava ter medido o que não tinha**: o comentário dizia
"MEDIDO por `tools/sonda-http`" e listava seis formas equivalentes, das quais a
sonda da F9 havia medido **duas**. As outras vieram de ler o Salvo.

É a mesma causa dos dois defeitos da F12, em outra roupa: **inferência escrita
como medição**. A resposta foi a mesma: medir.

### O que foi medido

`tools/sonda-http --bin sonda-caminho` — binário novo no mesmo *crate* da sonda
da F9. Sobe um servidor Salvo de verdade e escreve a linha de requisição **byte
a byte num socket**, sem cliente HTTP no meio: qualquer cliente que passe por
`Url::parse` normalizaria a URL antes de ela chegar ao servidor, e é justamente
a forma crua que interessa. 45 casos — 40 com GET, 5 com POST.

A regra medida, em uma frase:

> parta em `/`, descarte os segmentos **vazios**, decodifique **cada segmento**
> que sobrou, e junte de volta com `/`.

### As três correções

1. **`/./ping` responde 404, não 200.** O Salvo descarta os segmentos vazios e
   só eles; `.` e `..` são segmentos comuns. A implementação anterior descartava
   `.` junto com os vazios.

2. **`GET /` responde 405, não 404** — em qualquer método, e em qualquer forma
   que colapse para zero segmentos (`/`, `//`, `///`). No legado a raiz é rota
   de verdade: `Router::new()` casa o caminho vazio e não tem método.
   `catcher.go` **já registrava isso desde a F9**; o roteador nunca fez. Duas
   partes do mesmo porte discordavam e nenhum teste as confrontava.

3. **A ordem da decodificação.** O porte roteava por `r.URL.Path`, que em Go já
   vem **decodificado** — então `%2F` virava barra antes do fatiamento e
   `/ping%2F` colapsava para `/ping`, 200 onde o legado dá 404. O roteamento
   passou a ser sobre `r.URL.EscapedPath()`, decodificando **segmento a
   segmento**, que é a ordem do Salvo.

A terceira é a de fundo: as outras duas são casos, esta é a regra. O efeito
colateral é de segurança e é o lado bom — nenhuma forma codificada alcança rota
que a forma literal não alcançaria.

### A ordem foi a certa

O teste novo foi escrito contra a tabela medida e rodado **antes** da correção:
acusou **14 divergências** no roteador então em produção — 11 consertáveis e 3
não. Depois da correção, as 11 zeraram. Estender a sonda ao fragmento revelou
mais 2, também não consertáveis, que entraram na mesma D-24.

### O que não tem conserto proporcional — D-24

| Caso | Legado | Porte |
|---|---|---|
| `/ping%` `/ping%2` `/ping%zz` | 404 + catcher | **400** do `net/http` |
| `/ping#f` | 200 `pong` | **404** |
| `/pdf#x` | 405 | **404** |

Percentual inválido: `url.ParseRequestURI` recusa e o `net/http` responde 400
**antes de qualquer manipulador** — nenhum middleware vê a requisição.
Fragmento cru: MEDIDO, `EscapedPath()` devolve `/ping%23f` tanto para `/ping#f`
quanto para `/ping%23f`, que no legado respondem 200 e 404 — são
indistinguíveis depois da análise de URL do Go.

Nenhuma é alcançável por cliente conforme: pela RFC 3986 §3.5 o fragmento não é
enviado ao servidor. Nenhuma perde função, e as duas de fragmento deixam o porte
**mais restritivo** que o legado. Recomendação registrada: **aceitar**.

Ficaram **assertadas** em `divergenciasConhecidas`, não apenas anotadas: se uma
versão futura do Go responder outra coisa — inclusive a coisa certa —, o teste
falha e diz o que fazer.

### A sonda saiu do repositório de novo

Mesmo tratamento que o oráculo da F12 recebeu no enxugamento: a medição fica
congelada em `caminho_test.go`, e o aparato é recuperável do histórico. Ela foi
**comitada primeiro** (227c97c) justamente para ser citável por SHA:

```sh
git checkout 227c97c -- tools/sonda-http
cargo run --release --manifest-path tools/sonda-http/Cargo.toml --bin sonda-caminho
```

Manter um *crate* Rust na árvore contradiz o enxugamento a que este repositório
foi submetido a pedido — o serviço não o compila, não o distribui e não precisa
dele para rodar.

### Duas linhas de teste antigas estavam erradas

`TestRoteamentoMedido` afirmava `/./ping` → 200 e `/` → 404. Corrigidas. Elas são
a razão de a divergência ter sobrevivido a três fases de teste: o teste
codificava a mesma inferência que o código.

### Medições

| O que | Valor |
|---|---|
| Casos medidos contra o Salvo | 45 (40 GET, 5 POST) |
| Divergências na primeira passada | 14 — 11 corrigidas, 3 aceitas |
| Divergências que a sonda estendida achou depois | 2 de fragmento, aceitas |
| Diferença só de corpo | 1 (`/ping\tx`: 400 nos dois, corpo diferente) |
| Linhas em `divergenciasConhecidas` | 6 |
| Testes do pacote `httpapi` | todos passam |

---

## Validação com um Diário Oficial REAL

**A pedido**, com um PDF trazido de fora: `exemplos/dou-secao1-2026-07-08.pdf`,
Diário Oficial da União, Seção 1, nº 126 de 8 de julho de 2026, página 177 —
deliberações do MPT. Uma página, 17.307 caracteres depois da normalização.

### Por que isso não é redundante com o corpus dourado

O corpus dourado da F0 tem 28 documentos, e todos os 28 **nós escrevemos**. A
F12 registrou isso como a maior ressalva do relatório de paridade: são
sintéticos, e sintético não sabe produzir o que documento real produz — texto
posicionado com espaço entre as letras, abreviação, número de processo colado,
caixa alta acentuada.

Isto **não fecha D-11**, que continua bloqueante: um documento não é corpus, e
não há oráculo em Rust para compará-lo. O que ele responde é uma pergunta menor
e que estava aberta: **o serviço processa um Diário Oficial de verdade?**

Responde: sim. `status 5`, 5 recortes, 59 ms.

### A matriz medida

Oito expressões, **uma por perfil** — dentro de um mesmo perfil a primeira a
encontrar a página consome a página (INV-P12) e o resultado seria correto e
ilegível.

| Perfil | Expressão | Resultado |
|---|---|---|
| 301 | `BR BPO TECNOLOGIA E SERVICOS` | casou |
| 302 | `CASAMAX COMERCIAL E SERVICOS LTDA` | casou |
| 303 | `LEI GERAL DE PROTECAO DE DADOS` | casou |
| 304 | `DEBORAH DA SILVA FELIX` | casou |
| 305 | `HOMOLOGACOES DE ARQUIVAMENTO` | casou |
| 306 | `EMPRESA BRASILEIRA DE CORREIOS E TELEGRAFOS` | **não** |
| 307 | `LEI GERAL DE PROTEÇÃO DE DADOS` | **não** |
| 308 | `PREFEITURA MUNICIPAL DE SAO PAULO` | **não** |

**As três que não casaram valem mais que as cinco que casaram.**

**306** — a extração devolve `TELEG R A FO S`, com espaço entre as letras,
porque é assim que o texto está posicionado no documento. Cinco termos onde a
expressão espera um. O serviço está certo; o legado também não casaria. Nenhum
documento sintético nosso teria produzido esse caso.

**307** — a MESMA expressão do 303, com acento. Não casa, porque o texto
indexado perdeu os acentos e a expressão cadastrada não passa pela mesma
normalização. É INV-P19, DEFEITO PRESERVADO. O par 303/307 é a demonstração
viva: a única diferença entre as duas linhas é o acento.

### A matriz virou teste, não afirmação

`internal/app/diario_real_test.go` submete o PDF pelo HTTP de produção e confere
as duas direções — toda expressão que deve casar tem recorte, e **nenhuma** das
que não devem tem. Roda com as chaves no padrão.

**Verificado por sabotagem**, e o resultado é o mais instrutivo da rodada.
Trocando `extratorDeProducao` pelo extrator cru — o defeito exato que a F12
encontrou:

```
perfil 303 "LEI GERAL DE PROTECAO DE DADOS"  NÃO gerou recorte; deveria casar
perfil 305 "HOMOLOGACOES DE ARQUIVAMENTO"    NÃO gerou recorte; deveria casar
perfil 307 "LEI GERAL DE PROTEÇÃO DE DADOS"  gerou recorte; NÃO deveria casar
```

A terceira linha é a **inversão de INV-P19** que o relatório da F12 descreveu em
prosa, agora observada sobre entrada real: sem a normalização, a expressão
acentuada — inerte no legado — passa a casar, e a sem acento para. O teste pega
a inversão nas duas direções.

A sabotagem foi desfeita por edição, não por `git checkout` — a lição da F12.

### O que entrou no repositório

| Arquivo | Papel |
|---|---|
| `exemplos/dou-secao1-2026-07-08.pdf` | o documento real, 162 KB, publicação oficial pública |
| `db/init/03-perfis-do-dou-real.sql` | os oito perfis, com o porquê de cada linha |
| `internal/app/diario_real_test.go` | a matriz como asserção |
| `README.md` § "Validando com um diário real" | o procedimento, para DBeaver ou linha de comando |

O compose monta `./db/init` inteiro, então `03-` é aplicado sozinho na criação
do volume. `prepararEsquema` dos testes de integração também o aplica.

### Medições

| O que | Valor |
|---|---|
| Páginas | 1 |
| Caracteres depois da normalização | 17.307 |
| Expressões medidas | 8 — 5 casam, 3 não |
| Recortes gerados | 5 |
| Tempo de processamento | 59 ms |
| Sabotagem do extrator | 3 linhas da matriz + 4 do texto acusam |

---

## De onde vem o PDF — e o teto de 64 KiB que ninguém tinha visto

**A pergunta veio de fora e era simples:** "onde estaria o arquivo PDF quando a
API estiver em produção?". A resposta que dei primeiro citava D-21 e estava
**parcialmente errada**. Medir corrigiu a resposta e destapou algo maior.

### A resposta à pergunta

O PDF **chega na própria requisição**. Não há de onde buscá-lo: nem disco
vigiado, nem fila, nem armazenamento de objeto. Quem submete é o cliente que
chama `POST /pdf`. Isso vale para os dois lados.

O que difere é **onde os bytes ficam durante a requisição**, e aqui a
documentação estava errada.

### O que D-21 afirmava, e o que a medição mostrou

D-21 dizia: *"os bytes do PDF vivem em memória durante o processamento (...) Não
há gravação em disco, nem no legado nem no porte."*

`main.rs:233` não lê o corpo da requisição — lê um ARQUIVO:

```rust
let conteudo = tokio::fs::read(&arquivo.path()).await.unwrap();
```

`arquivo.path()` só existe porque o Salvo **já gravou** aquela parte do
multipart em disco. MEDIDO por `tools/sonda-http --bin sonda-arquivo`:

| | Legado (Salvo 0.95.2) | Porte (Go) |
|---|---|---|
| Onde | `/tmp/salvo_http_multipartXXXXXX/{nonce}.pdf` | só memória |
| Existe durante a requisição? | **sim** | não |
| Sobrevive a ela? | **não** — `Drop` de `FilePart` apaga | — |

A conclusão de D-21 continua inteira: nada persiste, não há de onde
reprocessar. O que mudou é o alcance da afirmação — e isso importa para quem
dimensiona `/tmp` ou monta o contêiner com sistema de arquivos somente leitura.

O porte usa `r.MultipartReader()`, não `ParseMultipartForm`. A escolha foi feita
na F9 por outro motivo — paridade no caso `filename=""` —, mas o efeito colateral
é que o Go nunca grava temporário. `ParseMultipartForm` gravaria.

### O achado maior: o arcabouço impõe um teto que o `main.rs` não pede

A mesma sonda, ao tentar submeter o DOU real de 162 KiB, recebeu
**`400 PDF não enviado`**. Varrendo o tamanho:

```
 32.768 bytes  →  200 PDF carregado com sucesso
 65.136 bytes  →  400 PDF não enviado
165.447 bytes  →  400 PDF não enviado     ← o DOU real deste repositório
```

A causa está no fonte do arcabouço:

```rust
// salvo_core-0.95.2/src/http/request.rs:33
static GLOBAL_SECURE_MAX_SIZE: AtomicUsize = AtomicUsize::new(64 * 1024);
```

Vale para o **corpo inteiro**. Estourado, `req.file("pdf")` devolve `None`, o
`main.rs:210` acumula `PDF não enviado` e responde 400 — **indistinguível de uma
requisição que realmente não trouxe arquivo**. As 730 linhas do `main.rs` não
configuram nada disso.

Também medido, baixando os *crates* e conferindo o fonte: o teto entrou entre
**0.70 e 0.75** e nunca mudou de valor.

| Versão | Tem o teto? |
|---|---|
| 0.65.0 · 0.70.0 | **não** |
| 0.75.0 até 0.95.2 | sim, 64 KiB |

### Por que isso não virou código

Um serviço que existe para ingerir Diários Oficiais e recusa tudo acima de
64 KiB não processaria diário nenhum. As duas leituras — legado em Salvo < 0.75
(aceita tudo, porte correto) ou ≥ 0.75 (recusa diários reais **hoje**) — não
podem ser separadas sem o `Cargo.lock`, que é **D-15**, bloqueante desde a F0.

Registrado como **D-25**, bloqueante, padrão provisório **não impor limite** —
que é o comportamento atual e o único sob o qual o serviço cumpre a função que
visivelmente cumpre. `MAX_UPLOAD_BYTES=65536` reproduz o teto se a resposta for
"sim", mas não o corpo da resposta: hoje o porte devolve crítica de corpo
grande, e o legado devolve `PDF não enviado`.

**D-04 deixou de ser curiosidade de capacidade.** "Qual o maior PDF já
processado" agora é evidência direta: qualquer resposta acima de 64 KiB prova a
leitura (a) e fecha D-25 sem escrever uma linha.

### A lição, de novo

Duas rodadas seguidas — o caminho com espaço, agora o teto de tamanho — vieram
de **perguntas de fora**, não de testes. E as duas encontraram documentação que
afirmava mais do que havia sido medido. A F12 já tinha nomeado esse padrão; ele
continua aparecendo.

### Medições

| O que | Valor |
|---|---|
| Caminho do temporário no legado | `/tmp/salvo_http_multipartXXXXXX/{nonce}.pdf` |
| Sobrevive à requisição? | não |
| Teto do corpo no Salvo ≥ 0.75 | 65.536 bytes |
| Versões varridas | 0.65.0 · 0.70.0 · 0.75.0 · 0.80.0 · 0.85.0 · 0.88.0 · 0.89.0 · 0.90.0 · 0.94.0 · 0.95.0 · 0.95.1 · 0.95.2 |
| Resposta do legado ao DOU real | `400 PDF não enviado` |
| Resposta do porte ao DOU real | `200 PDF carregado com sucesso`, 5 recortes |
