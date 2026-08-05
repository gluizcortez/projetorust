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
