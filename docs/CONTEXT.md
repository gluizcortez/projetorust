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
