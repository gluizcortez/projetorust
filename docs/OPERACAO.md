# Operação

Guia de configuração do serviço em produção. Entregue na fase **F11**.

Este documento responde a três perguntas: **o que cada chave faz**, **por que o
padrão é o que é**, e **em que ordem ligá-las**.

---

## 1. A regra que governa todas as chaves

> **Nenhuma chave liga por padrão.** Com todas nos padrões, o serviço em Go é
> indistinguível do serviço original em Rust — mesmas respostas, mesmos textos,
> mesmos efeitos no banco.

Isso não é conservadorismo decorativo. É o que permite **cortar primeiro e
melhorar depois**: a migração pode ir a produção sem carregar junto nenhuma
mudança de comportamento, e cada melhoria entra sozinha, com rollback de uma
variável de ambiente.

Uma consequência prática: **um problema em produção logo após o corte não é
causado por uma chave**, porque nenhuma está ligada. Isso reduz muito o espaço
de busca.

---

## 2. Tabela de chaves

> Os testes nomeados na última coluna vivem em `internal/`, exceto os de
> paridade — `TestPropriedadeDoLaco`, `TestIntegracaoLote…` e afins —, que
> saíram do repositório junto com o corpus dourado e estão no commit
> `2febb7a`. Ver `docs/RELATORIO-PARIDADE.md`.

| Chave | Padrão | O que muda quando ligada | Como se prova que desligada não muda nada |
|---|---|---|---|
| `MAX_IMPORTACOES_CONCORRENTES` | `0` (sem teto) | Semáforo no executor. Acima do teto a submissão **enfileira**; a resposta HTTP não muda. Alimenta as métricas de fila. | `TestSemTetoNaoProduzAmostraDeEspera`: sem teto, nenhuma amostra de espera e fila sempre zero. |
| `MAX_UPLOAD_BYTES` | `0` (sem limite) | Corpo acima do limite → **400** com a crítica `PDF excede o tamanho máximo`. | `TestLimiteDeCorpoDesligadoPorPadrao`; com `0`, `LimiteDeCorpo` devolve o próximo manipulador **sem envolver nada**. |
| `VALIDAR_ASSINATURA_PDF` | `false` | Arquivo que não começa com `%PDF-` → **422**, com o texto que o serviço já emite. Nenhuma importação é registrada. | `TestAssinaturaDesligadaAceitaQualquerArquivo`: um CSV é aceito com 200 e registrado. |
| `GRAVACAO_EM_LOTE` | `false` | Dois comandos por chave de pesquisa em vez de dois por recorte. | `TestIntegracaoLoteProduzOMesmoEstadoQueLinhaALinha` compara as duas modalidades linha a linha no banco. |
| `IDEMPOTENCIA_POR_HASH` | `false` | Reenvio de documento **finalizado** devolve o id existente sem reprocessar. | `TestIdempotenciaDesligadaRegistraOReenvio`: duas submissões idênticas produzem duas importações. |
| `VARREDURA_ORFAS` | `false` | Tarefa periódica que encontra importações paradas em 1–3. | `TestVarreduraDesligadaNaoEhMontada`: o laço não é sequer construído. |
| `RESPOSTA_PROBLEM_JSON` | `false` | Respostas de **erro** (≥ 400) em `application/problem+json`. Sucessos continuam em texto. | `TestProblemJSONSoTrocaOsErros`. |
| `RATE_LIMIT_RPS` | `0` (desligado) | Balde de fichas por chave de API, com `Retry-After`. | `TestTaxaDesligadaNaoRecusa` dispara 200 requisições; o middleware nem entra na cadeia. |
| `STATUS_ENDPOINT` | `false` | Registra `GET /importacao/{id}`. Adição pura. | `TestStatusEndpointDesligadoNaoRegistraARota`: 404 com a página HTML de rota inexistente. |
| `HEALTH_ENDPOINTS` | `false` | Registra `/health/live` e `/health/ready`. | `TestHealthDesligadoNaoRegistraAsRotas`, e `TestPingNaoMudaComHealthLigado` para o outro lado. |

Ajustes que só têm efeito com `VARREDURA_ORFAS` ligada:

| Chave | Padrão | Papel |
|---|---|---|
| `VARREDURA_ORFAS_INTERVALO` | `15m` | De quanto em quanto tempo a varredura roda. |
| `VARREDURA_ORFAS_LIMIAR` | `1h` | Idade mínima de `data_inicio` para considerar uma importação presa. |
| `VARREDURA_ORFAS_POLITICA` | `observar` | `observar` registra e conta; `erro` grava status −1. |
| `VARREDURA_ORFAS_LOTE` | `100` | Quantas linhas uma passagem tranca. |

E as chaves que **não** são evolução de comportamento — infraestrutura de
processo, entregues nas fases F2 e F10:

| Chave | Padrão | Papel |
|---|---|---|
| `CONFIG_ESTRITA` | `false` | Transforma valor de porta malformado em falha de arranque. Ver A19. |
| `SHUTDOWN_TIMEOUT` | `0` (espera indefinida) | Teto do encerramento ordenado. |
| `HTTP_TEMPO_LIMITE_LEITURA` / `_ESCRITA` | `0` | Sem limite. Fechá-los depende de **D-04**. |
| `INDEX_MEMORIA_BYTES` | `500000000` | Orçamento herdado do índice original. |

---

## 3. Ordem de ativação recomendada

Ligar tudo de uma vez desperdiça a principal vantagem do desenho: quando algo
muda, você sabe exatamente o quê.

**Etapa 1 — observabilidade, risco nulo.**

```sh
HEALTH_ENDPOINTS=true
STATUS_ENDPOINT=true
```

Ambas são adição pura. `/ping` continua idêntico. Nenhuma rota existente muda.

**Etapa 2 — proteção, risco baixo.**

```sh
MAX_UPLOAD_BYTES=…      # ver §4
VALIDAR_ASSINATURA_PDF=true
RATE_LIMIT_RPS=…        # ver §4
```

As três **recusam** requisições que antes eram aceitas. Antes de ligar, meça:
o painel de tamanho de corpo e o volume por cliente dizem se algum legítimo
seria cortado.

**Etapa 3 — desempenho, risco médio.**

```sh
GRAVACAO_EM_LOTE=true
```

Não muda o estado final do banco — há teste que compara as duas modalidades
linha a linha. Muda o perfil de carga: menos idas ao banco, transações mais
curtas.

**Etapa 4 — controle de carga, exige medição.**

```sh
MAX_IMPORTACOES_CONCORRENTES=…   # ver §5
```

Ligar cedo demais transforma pico em fila e fila em latência. Só depois de a
métrica `recorte_estagio_duracao_segundos` ter uma linha de base.

**Etapa 5 — mudança de semântica, exige decisão de produto.**

```sh
IDEMPOTENCIA_POR_HASH=true   # DEPOIS da migração 0002
VARREDURA_ORFAS=true         # comece com política `observar`
```

Estas duas mudam **o que o serviço faz**, não só como. Ambas precisam de
concordância de quem opera o negócio, não só de quem opera a infraestrutura.

---

## 4. Valores recomendados por porte de carga

Os números abaixo são **pontos de partida**, não verdades. Os valores
definitivos dependem de **D-04** — o maior PDF e o maior número de recortes já
processados em produção —, que ainda não foi respondida.

| | Pequeno (≤ 50 diários/dia) | Médio (50–500/dia) | Grande (> 500/dia) |
|---|---|---|---|
| `MAX_IMPORTACOES_CONCORRENTES` | `0` (sem teto) | `4` | `2 × núcleos`, ver §5 |
| `MAX_UPLOAD_BYTES` | `104857600` (100 MiB) | `104857600` | `209715200` (200 MiB) |
| `RATE_LIMIT_RPS` | `0` | `20` | `50` por instância |
| `VARREDURA_ORFAS_INTERVALO` | `1h` | `15m` | `5m` |
| `VARREDURA_ORFAS_LIMIAR` | `2h` | `1h` | `30m` |
| `VARREDURA_ORFAS_LOTE` | `50` | `100` | `200` |
| `SHUTDOWN_TIMEOUT` | `0` | `120s` | `120s` |

### Por que 100 MiB para `MAX_UPLOAD_BYTES`

Um diário oficial estadual com centenas de páginas fica na casa das dezenas de
megabytes. 100 MiB dá folga de ordem de grandeza sobre o caso típico e ainda
assim barra o engano óbvio — um vídeo, um dump, um arquivo compactado.

**O limite não substitui `VALIDAR_ASSINATURA_PDF`**: um arquivo de 2 MiB que não
é PDF passa pelos dois limites e só falha no processamento assíncrono.

### Por que `RATE_LIMIT_RPS` é por instância

O balde vive **em memória**, por processo. Com N instâncias atrás de um
balanceador, o limite efetivo é `N × RATE_LIMIT_RPS`. Um limite global exigiria
estado compartilhado — Redis ou equivalente —, que é dependência nova e não se
justifica para o volume esperado.

Consequência: **ao escalar horizontalmente, o limite por instância deve cair**,
ou o limite efetivo sobe junto sem ninguém perceber.

---

## 5. O cálculo de memória de `MAX_IMPORTACOES_CONCORRENTES`

Cada importação em andamento segura, ao mesmo tempo:

| Componente | Ordem de grandeza | Por quê |
|---|---|---|
| Bytes do PDF | tamanho do arquivo | Lido **inteiro** em memória antes do resumo, e mantido até o fim do processamento. |
| Texto extraído | ~2× a 4× o tamanho do PDF | Uma página de texto normalizado costuma ocupar mais que a página comprimida. |
| Índice em memória | ~1× a 2× o texto | Vocabulário mais listas de posições. |
| Recortes acumulados | nº de recortes × tamanho da página | O texto **integral** de cada página com ocorrência. |

Regra de bolso:

```
memória por importação ≈ 8 × tamanho do PDF
```

> **ESTA RÉGUA NÃO ESTÁ VALIDADA.** A fase F12 mediu **14,3×** sobre o maior
> documento do corpus (~100 KiB) — quase o dobro. O fator medido não se
> transfere direto para um diário real, porque sobre um documento pequeno o
> custo FIXO por importação domina; mas isso não valida os 8×, apenas explica
> por que a medição não os contradiz de forma conclusiva.
>
> **Remedir assim que D-11 entregar documentos reais.** Até lá, trate os 8×
> como provisório e arredonde o teto de concorrência para baixo com folga: um
> teto derivado de régua errada é como o processo morre por falta de memória,
> levando junto todas as importações em andamento. Ver
> `docs/RELATORIO-PARIDADE.md` §5.3.

Um diário de 30 MiB consome cerca de **240 MiB** no pico. Daí:

```
MAX_IMPORTACOES_CONCORRENTES ≈ (memória disponível × 0,6) / (8 × maior PDF)
```

O fator 0,6 deixa margem para o coletor de lixo do Go, o pool de conexões e o
próprio servidor HTTP. Em um contêiner com 4 GiB e diários de até 30 MiB:

```
(4096 × 0,6) / 240 ≈ 10
```

Arredonde **para baixo**: o custo de um teto folgado é fila; o custo de um teto
apertado demais é o processo morto por falta de memória, que derruba **todas**
as importações em andamento, não só a que estourou.

### O teto não rejeita — enfileira

Acima do teto, a submissão **espera vaga**. A resposta HTTP não muda, e é
proposital: rejeitar mudaria o contrato. O preço é que a espera aparece na
latência do `POST /pdf`.

Duas métricas governam o ajuste:

- `recorte_fila_profundidade` — quantas submissões estão aguardando **agora**;
- `recorte_fila_espera_segundos` — quanto tempo esperaram.

Profundidade alta com espera baixa é fila saudável. **Espera na casa dos
segundos significa que o cliente está pagando o teto na latência** — e aí a
resposta certa é mais instância, não teto maior.

---

## 6. `IDEMPOTENCIA_POR_HASH` — ordem obrigatória

1. Aplique `db/migrations/0002_idempotencia_indice.up.sql` **em autocommit**
   (`CREATE INDEX CONCURRENTLY` é recusado dentro de transação);
2. confirme que o índice está **válido**:

   ```sql
   SELECT c.relname, i.indisvalid
   FROM pg_index i JOIN pg_class c ON c.oid = i.indexrelid
   WHERE c.relname = 'ix_importacao_idempotencia';
   ```

   `indisvalid = false` significa criação interrompida: aplique o `.down.sql` e
   repita;
3. só então ligue `IDEMPOTENCIA_POR_HASH=true`.

Ligar a chave antes do índice faz **cada submissão varrer `tb_importacao`
inteira**.

### O que conta como "o mesmo documento"

Os três campos juntos: `hash`, `id_cadernos` e `data_caderno`. O hash sozinho
não basta — o mesmo arquivo pode ser submetido legitimamente para outro caderno
ou outra data, e tratá-los como repetição **perderia importações**.

A data de **disponibilização** fica de fora de propósito: reenviar o mesmo
caderno com a disponibilização corrigida é correção de metadado.

### Só a importação FINALIZADA é reaproveitada

| Status do equivalente | O que acontece |
|---|---|
| `5` finalizado | Devolve o id existente, sem reprocessar. |
| `0`–`3` em curso | Registra normalmente. Devolver o id faria o cliente herdar uma tarefa que ainda pode falhar. |
| `−1` erro | Registra normalmente. Reenviar depois de um erro é exatamente o que o operador precisa poder fazer. |

Falha na consulta de idempotência **não derruba a submissão**: ela segue o
caminho normal. A evolução é otimização, não regra de negócio.

---

## 7. `VARREDURA_ORFAS` — comece observando

### O problema

Uma tarefa de segundo plano que morre — pânico, queda do processo, contêiner
reciclado — deixa a linha no último status gravado, **para sempre**. Nada a
reexamina. Ver `docs/ESPECIFICACAO.md` §3.5 e o achado A15.

### Não existe política de reprocessar

O serviço **não guarda o documento**. `tb_importacao` tem o nome original e o
resumo SHA-256; os bytes do PDF vivem em memória durante o processamento e são
descartados com a tarefa. Reprocessar exigiria buscar o arquivo em algum lugar,
e não há lugar.

Passar a arquivar o documento submetido é a decisão aberta **D-21**, e é
pré-requisito de qualquer reprocessamento automático.

### As duas políticas

`observar` (padrão) registra e conta, sem tocar no banco. `erro` grava −1.

**Comece sempre com `observar`, por pelo menos um ciclo semanal completo.**
Uma varredura que já nasce escrevendo pode marcar como erro um lote inteiro de
importações que estavam apenas lentas — e não há como desfazer, porque o
documento não existe mais.

Acompanhe `recorte_varredura_orfas_total` por status. Se ela contar importações
que depois terminam sozinhas, o **limiar está curto demais**.

### O limiar tem de ser maior que o pior tempo de processamento

O padrão de 1 hora é muito acima do pior caso conhecido, deliberadamente. Para
apertá-lo, use o percentil 99 de `recorte_estagio_duracao_segundos{estagio="importacao"}`
e multiplique por 3.

Importações em status 1–3 **sem** `data_inicio` nunca são varridas: elas
travaram antes do carimbo e não há nelas como medir idade.

### Com várias instâncias

A consulta usa `SELECT … FOR UPDATE SKIP LOCKED` dentro de uma transação, então
duas instâncias nunca pegam a mesma importação. Não é preciso eleger líder nem
desligar a varredura nas réplicas.

---

## 8. Métricas que importam

Expostas pelo registro do Prometheus. Todas com o prefixo `recorte_`.

| Métrica | Para quê |
|---|---|
| `importacoes_total{estado}` | Taxa de erro. Um salto em `estado="erro"` é o primeiro sinal. |
| `importacoes_em_andamento` | Trabalho ativo. Deve voltar a zero. |
| `fila_profundidade` | Só sobe com teto ligado. Fila persistente = teto apertado. |
| `fila_espera_segundos` | Quanto o cliente paga pelo teto na latência. |
| `estagio_duracao_segundos{estagio}` | Onde o tempo é gasto. Base do limiar da varredura. |
| `documentos_sem_paginas_total` | **INV-P20**: PDF truncado é aceito, produz zero páginas e termina em status 5 — indistinguível no banco de um diário sem ocorrências. Esta é a única forma de enxergá-lo. Ver D-18. |
| `paginas_por_documento` | Dimensiona memória; entra no cálculo de §5. |
| `recortes_por_importacao` | Um pico anormal antecede o estouro de `total_recortes` (INV-P18). |
| `varredura_orfas_total{status}` | Quantas presas existem, por status de origem. |
| `varredura_tratadas_total` | Fica em zero com a política `observar`. |

### Alarmes sugeridos

```
# Importações presas acumulando — o serviço está perdendo trabalho em silêncio.
increase(recorte_varredura_orfas_total[1h]) > 0

# Documentos truncados chegando (D-18).
increase(recorte_documentos_sem_paginas_total[1h]) > 0

# Fila persistente: o teto está apertado ou falta instância.
avg_over_time(recorte_fila_profundidade[15m]) > 0

# Trabalho ativo que não volta a zero: importação travada.
min_over_time(recorte_importacoes_em_andamento[30m]) > 0
```

---

## 9. Encerramento

O `SHUTDOWN_TIMEOUT` padrão é **`0` — espera indefinida**, que é o
comportamento original: uma importação travada segura o processo para sempre.
É defeito preservado, documentado em `docs/ESPECIFICACAO.md` §6.3.

Em orquestrador com prazo de encerramento (o `terminationGracePeriodSeconds` do
Kubernetes, por exemplo), defina `SHUTDOWN_TIMEOUT` **abaixo** desse prazo. Sem
isso, o orquestrador mata o processo com `SIGKILL` no meio da drenagem, e o
encerramento ordenado não acontece.

Ordem do encerramento, normativa:

1. para a varredura de órfãs (a única etapa que abre transação por conta
   própria);
2. para de aceitar conexões; as requisições em curso terminam;
3. drena as importações em andamento;
4. fecha o pool do banco;
5. esvazia a telemetria.

Códigos de saída: `0` limpo, `1` falha, `2` teto de encerramento estourado com
trabalho pendente, `130` segundo sinal.

---

## 10. Decisões que ainda faltam

Duas são **bloqueantes** para o corte:

- **D-11** — corpus real de diários para a suíte de paridade da fase F12;
- **D-15** — `Cargo.lock` de produção, para saber a versão exata do arcabouço
  original e confirmar o HTML das páginas de 404 e 405.

E estas afetam diretamente os valores deste guia:

- **D-04** — maior PDF e maior número de recortes já processados. Governa
  `MAX_UPLOAD_BYTES`, `MAX_IMPORTACOES_CONCORRENTES` e os tempos limite de
  leitura e escrita do HTTP;
- **D-18** — se PDFs truncados chegam em produção;
- **D-21** — se o documento submetido passa a ser arquivado. Pré-requisito de
  reprocessamento automático na varredura.

Lista completa em `docs/DECISOES-ABERTAS.md`.
