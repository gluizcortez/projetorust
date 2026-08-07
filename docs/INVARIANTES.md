# Invariantes de paridade

> **Fase F0.** Cada entrada é uma asserção nomeada, verificável, sobre um
> comportamento sutil do serviço legado. São a base dos testes normativos das
> fases F4 a F12.
>
> Formato: **ID · descrição · caso positivo · caso negativo · como um porte
> ingênuo quebraria**.
>
> A coluna "fase" indica onde a invariante é provada. O mapeamento completo
> está em `docs/roadmap-migracao-rust-go.html`, §8.3.

| ID | Tema | Fase | Severidade |
|---|---|---|---|
| INV-P01 | Modo *extended* do motor de expressões regulares | F7 | Crítica |
| INV-P02 | `\w` Unicode contra ASCII | F6 | Crítica |
| INV-P03 | Cadeia do analisador léxico do Tantivy | F6 | Crítica |
| INV-P04 | Comprimento de termo medido em bytes | F6 | Alta |
| INV-P05 | Busca de frase, não de subcadeia | F7 | Crítica |
| INV-P06 | Expressão que tokeniza para vazio | F7 | Alta |
| INV-P07 | Remoção de diacríticos além da decomposição canônica | F6 | Crítica |
| INV-P08 | Ordem hífen → diacríticos | F6 | Alta |
| INV-P09 | Estrutura de blocos, linhas e quebras | F5 | Crítica |
| INV-P10 | Caractere irrecuperável vira espaço | F5 | Alta |
| INV-P11 | Granularidade da ligação com o MuPDF | F5 | Crítica |
| INV-P12 | Deduplicação por perfil, atravessando expressões | F8 | Crítica |
| INV-P13 | Contador de perfil corrente inicia em zero | F8 | Média |
| INV-P14 | Trabalho parcial sobrevive ao erro | F8 | Alta |
| INV-P15 | Estouro numérico é erro, não truncamento | F4 | Alta |
| INV-P16 | Datas sem fuso horário | F4 | Alta |
| INV-P17 | Aspas na expressão abortam a importação | F7 | Alta |
| INV-P18 | Estouro de `total_recortes` não grava `data_fim` | F4 | Média |
| INV-P19 | Expressão acentuada nunca casa | F7 | Alta |
| INV-P20 | PDF truncado conclui com sucesso e zero recortes | F5 | Alta |
| INV-P21 | Análise de data do chrono é mais permissiva que a do Go | F3 | Crítica |
| INV-P22 | Classe de caracteres do Rust diverge da do Go nos dois sentidos | F6 | Crítica |
| INV-P23 | O filtro do operador `&` só é compilado se houver acerto de frase | F7 | Alta |

---

## INV-P01 — O modo *extended* não existe no motor de Go

**Descrição.** O filtro do operador `&` monta o padrão com a flag `x`, que faz o
motor do Rust ignorar todo espaço em branco literal do padrão e tratar `#` como
início de comentário. As expressões vêm do banco e contêm espaços, então esse
pré-processamento **altera o padrão efetivo**.

**Origem.** `main.rs:392–398`.

```rust
let exp = format!("(?imx){}", key.replace('&', r"\s*&\s*"));
```

**Padrão efetivo** para a expressão `ACME & FILHOS`:

| Etapa | Resultado |
|---|---|
| Expressão de origem | `ACME & FILHOS` |
| Após `replace('&', "\s*&\s*")` | `ACME \s*&\s* FILHOS` |
| Após o pré-processamento da flag `x` | `ACME\s*&\s*FILHOS` |

**Caso positivo** — expressão `ACME & FILHOS` deve casar com os três textos:

| Texto | Casa? |
|---|---|
| `ACME & FILHOS` | sim |
| `ACME&FILHOS` | sim |
| `ACME   &   FILHOS` | sim |
| `acme & filhos` | sim (flag `i`) |

**Caso negativo** — a mesma expressão **não** casa com:

| Texto | Casa? |
|---|---|
| `ACME FILHOS` | não — falta o `&` |
| `ACME E FILHOS` | não |

**Como um porte ingênuo quebraria.** `regexp.Compile("(?imx)...")` devolve erro
em Go — RE2 não aceita a flag `x`. Removendo apenas a letra `x`, o padrão vira
`ACME \s*&\s* FILHOS`, que exige espaços literais **adicionais** ao redor dos
`\s*`. Contra o texto `ACME & FILHOS` (um espaço de cada lado), o padrão exige
espaço + `\s*` + `&`, ou seja, no mínimo um espaço — casa por acidente. Contra
`ACME&FILHOS` **não casa**, e hoje casa. O resultado é perda silenciosa de
recortes exatamente nas expressões corporativas, que são as que mais usam `&`.

**Verificação.** O teste deve falhar se a função de pré-processamento for
removida. Um teste que só exercite `ACME & FILHOS` contra `ACME & FILHOS`
passa nas duas implementações e **não** serve.

### As regras exatas do modo `x`, medidas na fase F7

O comportamento foi **medido**, não deduzido da documentação do *crate*:
`tools/capturar-corpus/src/bin/sonda-extended.rs` compara o `Hir` de `(?imx)X`
com o de `(?im)Y` — dois padrões com o mesmo `Hir` são o mesmo autômato.

1. **Espaço em branco é descartado.** O conjunto é exatamente as **25 runas** da
   propriedade Unicode `White_Space`, o mesmo que o `\s` do Rust aceita —
   verificado runa a runa em todo o Unicode, e os dois conjuntos coincidem.
   Inclui `\v`, `NBSP`, `U+1680`, `U+2000`–`U+200A`, `U+2028`, `U+2029`,
   `U+202F`, `U+205F` e `U+3000`. **Não** inclui o espaço de largura zero
   `U+200B`.
2. **`#` inicia comentário** até o próximo `\n` — apenas `\n`, nunca `\r` — ou
   até o fim do padrão.
3. **`\` escapa a runa seguinte**, que sobrevive: `\ ` é espaço literal e `\#` é
   `#` literal. A barra dupla `\\` é barra literal, e o `#` seguinte **volta** a
   iniciar comentário.

### Duas correções à especificação da fase

O enunciado de F7 afirmava que o modo `x` **preserva** espaços dentro de classes
de caracteres `[...]`, como fazem o PCRE e o Python. A medição **refuta as duas
metades** no motor do Rust:

| Padrão | Resultado medido |
|---|---|
| `(?imx)[a b]` | ≡ `(?im)[ab]` — o espaço **é** descartado dentro da classe |
| `(?imx)[a # b]` | **erro de sintaxe**: `unclosed character class` |

O `#` também inicia comentário dentro da classe, e o comentário engole o `]` que
a fecharia. **O Rust é o oráculo: a especificação foi corrigida.** A
consequência prática é boa — `StripExtended` não precisa rastrear classes de
caracteres, o que elimina toda uma família de erros de borda.

### O `\s` injetado também diverge

A substituição `key.replace('&', r"\s*&\s*")` injeta um `\s` que **não pode ser
traduzido por `\s`**:

| Motor | `\s` |
|---|---|
| *crate* `regex` | as 25 runas de `White_Space` |
| RE2 do Go | `[\t\n\f\r ]` — cinco runas, **sem sequer o `\v`** |

Um `&` cercado de espaço não quebrável, espaço ideográfico ou tabulação vertical
— todos possíveis na saída do MuPDF (INV-P10) — casa hoje e deixaria de casar. A
tradução usa a classe explícita `intervalosDeEspacoDoRust`, verificada runa a
runa contra o `\s` do Rust em todo o Unicode.

### Divergências residuais, conhecidas e limitadas

`StripExtended` é uma transformação **léxica** sobre a cadeia inteira; no Rust o
descarte acontece **durante** a análise sintática. As construções em que isso é
observável, mais as que o RE2 simplesmente não tem, estão em `DECISOES-ABERTAS.md`,
**D-06**. Todas **falham alto** — o padrão é recusado e a importação não conclui.
Nenhuma é alcançável por uma expressão de perfil que não contenha sintaxe de
expressão regular.

**Medição.** 250.000 pares expressão/texto no espaço realista (razão social com
`&`): **zero divergências**. 250.000 no espaço adversarial: **zero divergências
de resultado** — só divergências de aceitação, que são ruidosas.

---

## INV-P02 — `\w` é Unicode no Rust e ASCII em Go

**Descrição.** O padrão de junção de hífens usa `\w`. No *crate* `regex`, `\w`
é a classe Unicode; no RE2 do Go, é exatamente `[0-9A-Za-z_]`. Como a remoção
de diacríticos ocorre **depois** da junção, o texto ainda está acentuado nesse
ponto.

**Origem.** `main.rs:487`, `498–500`. Padrão: `(?imx)(\w+)(-\n)` → `$1`.

**Caso positivo** — junção que acontece hoje:

| Entrada | Saída |
|---|---|
| `"institui-\n"` + `"ção\n"` | `"instituição\n"` → após diacríticos: `"instituicao\n"` |
| `"conti-\n"` + `"nuacao\n"` | `"continuacao\n"` |
| `"José-\n"` + `"Maria\n"` | `"JoséMaria\n"` → `"JoseMaria\n"` |

**Caso negativo** — junção que **não** deve acontecer:

| Entrada | Saída |
|---|---|
| `"-\n"` + `"texto\n"` | inalterada — `\w+` exige ao menos um caractere de palavra antes do hífen |
| `"fim -\n"` + `"texto\n"` | inalterada — o espaço quebra a sequência `\w+` imediatamente antes do `-` |
| `"palavra\n"` + `"outra\n"` | inalterada — não há hífen |

**Como um porte ingênuo quebraria.** O padrão `(?im)(\w+)(-\n)` em Go só falha
quando o caractere **imediatamente anterior ao hífen** é acentuado — porque
`\w+` precisa casar ao menos esse caractere. Em `"institui-\n"` o caractere
anterior é `i`, ASCII, e o porte ingênuo casa por acidente; em
`"instituiçã-\n"` ou `"açõ-\n"` ele **não** casa, a palavra permanece partida
com a quebra de linha no meio, e uma expressão de busca que atravesse a emenda
deixa de casar.

Essa é a característica traiçoeira desta invariante: um teste montado com
palavras terminadas em consoante ASCII passa nas duas implementações. O corpus
sintético cobre o caso real em `04-inv-p02-hifen-acentuado.pdf`.

**Padrão correto em Go.** ⚠ **NÃO EXISTE.** Esta seção recomendava
`(?im)([\p{L}\p{N}_]+)(-\n)` até a fase F6, e o teste de propriedade a
**refutou** em 320.142 de um milhão de casos: `\p{L}\p{N}` perde as marcas
combinantes e inclui as categorias `No`/`Nl` a mais. Ver **INV-P22**.

A junção não usa expressão regular na implementação: o RE2 não tem como
expressar a classe `\w` do Rust, que mistura a propriedade `Alphabetic` com
categorias. A varredura é manual, com a classe medida em `palavra_table.go`.

---

## INV-P03 — A cadeia do analisador léxico tem três estágios

**Descrição.** O campo é declarado `TEXT | STORED` (`main.rs:513`), o que aplica
o analisador `default` do Tantivy. Ele é composto de três estágios, e o mesmo
analisador é aplicado à consulta pelo `QueryParser`.

**Composição.**

1. **Segmentação simples** — um termo é a maior sequência de caracteres
   alfanuméricos Unicode; todo caractere não alfanumérico é separador.
2. **Descarte de termos longos** — termos com comprimento a partir de um limite
   são **removidos do fluxo**, não truncados. O limite é medido em **bytes**.
3. **Minúsculas.**

**O termo descartado DEIXA UM BURACO na numeração de posições.** Esta é a parte
da invariante que faltava, acrescentada na fase **F12** depois de o teste de
propriedade do laço encontrar a divergência.

No Tantivy, a posição é atribuída pelo **tokenizador** (estágio 1); o descarte
por comprimento é um **filtro** que roda depois e **não renumera** o que sobra.
Em `alfa <termo-de-40-bytes> beta`, `alfa` fica na posição 0 e `beta` na 2 — e a
busca de frase `"alfa beta"`, que exige posições consecutivas, **NÃO casa**.

A regra vale nos dois lados: o mesmo analisador roda sobre a consulta, então um
termo longo no meio da EXPRESSÃO também deixa buraco, e a frase passa a exigir a
mesma distância no documento.

| Texto da página | Expressão | Casa? |
|---|---|---|
| `alfa beta` | `alfa beta` | sim — posições 0 e 1 |
| `alfa <40 bytes> beta` | `alfa beta` | **não** — posições 0 e 2 |
| `alfa <40 bytes> beta` | `alfa <40 bytes> beta` | sim — buraco dos dois lados |

**Como um porte ingênuo quebraria.** Numerando as posições pelo índice na lista
JÁ FILTRADA, `beta` cairia na posição 1 e a frase passaria a casar por cima do
termo longo — encontrando ocorrências que o serviço atual nunca encontrou. Era
exatamente o que o porte fazia até a F12, e os 28 documentos do corpus dourado
não pegavam: nenhum deles tem termo longo ENTRE dois termos de uma expressão
cadastrada. Quem pegou foi `TestPropriedadeDoLaco`, em 10.000 casos gerados.

**Valor do limite — MEDIDO.** Determinado empiricamente contra **tantivy
0.22.1** com o corpus sintético (`12-inv-p03-termos-longos.pdf` e
`12b-inv-p04-bytes-contra-runas.pdf`):

| Token | Bytes | Indexado? |
|---|---|---|
| `A`×30 | 30 | sim |
| `B`×39 | 39 | **sim** |
| `C`×40 | 40 | **não** |
| `D`×45 | 45 | não |
| `E`×80 | 80 | não |

**Limite = 40 bytes; a regra é descartar comprimento ≥ 40.** A fronteira 39/40
está coberta por caso de teste nos dois lados.

⚠ Resta confirmar que **a versão em produção é a 0.22.1**. O valor acima vale
para a versão resolvida no ambiente de desenvolvimento, não necessariamente
para a de produção — ver `DECISOES-ABERTAS.md`, D-03 e D-15. O método de
medição é reprodutível: basta reexecutar a captura após alinhar o `Cargo.lock`.

**Caso positivo** — termos que existem no índice:

| Texto da página | Termos |
|---|---|
| `PREFEITURA MUNICIPAL` | `prefeitura`, `municipal` |
| `Art. 5º, inciso II` | `art`, `5º`, `inciso`, `ii` |
| `JOAO-CARLOS` | `joao`, `carlos` |
| `2024/03/15` | `2024`, `03`, `15` |

**Caso negativo** — o que **não** vira termo:

| Texto da página | Efeito |
|---|---|
| sequência de 60 caracteres sem separador | **descartada por inteiro** — não é truncada em 40 |
| `---` | nenhum termo |
| `   ` | nenhum termo |

**Como um porte ingênuo quebraria.** Qualquer implementação que indexe os
termos longos passa a encontrar ocorrências que o serviço atual **nunca
encontrou** — PDFs de diário contêm rotineiramente códigos de autenticação,
tabelas coladas e lixo de reconhecimento óptico que caem nessa faixa. O efeito
é recorte a mais, não a menos, o que é igualmente uma divergência de produto.

---

## INV-P04 — O comprimento do termo é medido em bytes

**Descrição.** O estágio de descarte do INV-P03 mede o comprimento do termo em
**bytes**, não em runas. **Confirmado empiricamente** contra tantivy 0.22.1.

**Por que o corpus comum não distingue as duas hipóteses.** A remoção de
diacríticos (passo 7 do pipeline) converte praticamente todo o texto para
ASCII, onde bytes e runas coincidem. Testes montados com texto em português
**passam nas duas implementações** e não provam nada.

**Discriminador.** É preciso um termo que **sobreviva** à remoção de
diacríticos com caracteres multibyte. Caracteres gregos servem: são
alfanuméricos, formam termos, e não têm mapeamento na tabela do *crate*
`diacritics`.

**Medição** (`12b-inv-p04-bytes-contra-runas.pdf`):

| Termo | Runas | Bytes | Indexado? | Se o limite fosse em runas |
|---|---|---|---|---|
| `α`×19 | 19 | 38 | **sim** | sim |
| `α`×20 | 20 | 40 | **não** | *seria sim* |
| `α`×39 | 39 | 78 | não | *seria sim* |
| `α`×40 | 40 | 80 | não | não |

O maior termo grego indexado tem **19 runas / 38 bytes**. Um limite medido em
runas manteria `α`×39 (78 bytes). Portanto **o limite é em bytes**.

**Caso positivo.** `α`×19 (38 bytes) está no índice.

**Caso negativo.** `α`×20 (40 bytes, apenas 20 runas) **não** está — e é
exatamente o termo que uma implementação com contagem de runas manteria.

**Como um porte ingênuo quebraria.** Filtros de comprimento das bibliotecas Go
de busca costumam contar runas. A divergência só aparece em texto que sobrevive
à normalização com caracteres multibyte — raro, mas presente em diários que
citam nomes, termos técnicos ou trechos em outros alfabetos. A implementação
deve usar `len(termo)` em bytes, explicitamente, com comentário citando esta
invariante e a medição acima.

---

## INV-P05 — A busca é de frase, não de subcadeia

**Descrição.** A consulta é montada como `"\"{expressao}\""` (`main.rs:375`),
produzindo uma busca de **frase** sobre posições de termos, com distância zero.
Não é comparação de subcadeia sobre o texto.

**Caso positivo** — expressão `joao silva` casa com:

| Texto da página | Casa? | Por quê |
|---|---|---|
| `JOAO SILVA` | sim | termos consecutivos |
| `joao   silva` | sim | o separador não gera termo |
| `JOAO\nSILVA` | sim | quebra de linha é separador |
| `Joao, Silva` | sim | vírgula é separador |
| `joao - silva` | sim | hífen é separador |

**Caso negativo** — a mesma expressão **não** casa com:

| Texto da página | Casa? | Por quê |
|---|---|---|
| `JOAOSILVA` | **não** | um único termo `joaosilva` |
| `SILVA JOAO` | não | ordem invertida |
| `JOAO DA SILVA` | não | há um termo entre os dois |
| `JOAO SILVEIRA` | não | `silveira` ≠ `silva` |

**Como um porte ingênuo quebraria.** `strings.Contains(pagina, expressao)` é o
atalho mais tentador da migração e erra nos **dois sentidos**: passa a casar
`JOAOSILVA` (que hoje não casa) e deixa de casar `joao   silva` e
`JOAO\nSILVA` (que hoje casam). Como o texto vem de PDF, quebras de linha no
meio de nomes são comuns — a perda seria significativa.

---

## INV-P06 — Expressão que tokeniza para vazio devolve zero acertos

**Descrição.** Uma `expressao_nm` composta apenas de caracteres separadores
produz consulta vazia no Tantivy, que retorna zero acertos **sem erro**.

**Origem.** `main.rs:375`, `384–385`.

**Caso positivo** — as expressões abaixo devolvem lista vazia e nenhum erro:

| Expressão | Termos | Acertos |
|---|---|---|
| `---` | nenhum | 0 |
| `...` | nenhum | 0 |
| `   ` | nenhum | 0 |
| `&` | nenhum | 0 |

**Caso negativo.** Nenhuma dessas expressões pode produzir erro (que levaria a
importação a −1) nem correspondência universal (que geraria um recorte por
página do documento).

**Como um porte ingênuo quebraria.** Duas falhas simétricas e igualmente
plausíveis: (a) devolver erro porque a lista de termos está vazia, abortando a
importação; (b) tratar frase vazia como "casa com tudo", gerando um recorte por
página para todo perfil que tenha uma expressão degenerada cadastrada.

---

## INV-P07 — Remoção de diacríticos não é decomposição canônica

**Descrição.** `diacritics::remove_diacritics` (`main.rs:501`) usa tabela
própria. A decomposição canônica seguida de remoção das marcas resolve a maior
parte do português, mas **não decompõe** caracteres que a tabela do *crate*
mapeia.

**Caso positivo** — coberto pela decomposição canônica:

| Entrada | Saída |
|---|---|
| `á é í ó ú` | `a e i o u` |
| `ã õ` | `a o` |
| `â ê ô` | `a e o` |
| `ç` | `c` |
| `ü` | `u` |
| `ñ` | `n` |

**Caso negativo** — **não** decomposto pela normalização canônica, e portanto o
ponto onde as implementações divergem:

| Entrada | Saída esperada | Decomposição canônica sozinha |
|---|---|---|
| `ø` | `o` | `ø` — inalterado |
| `đ` | `d` | `đ` — inalterado |
| `ß` | ⚠ confirmar | `ß` — inalterado |
| `æ` | ⚠ confirmar | `æ` — inalterado |
| `ł` | `l` | `ł` — inalterado |
| `ħ` | `h` | `ħ` — inalterado |

Os valores marcados com ⚠ dependem da tabela real do *crate* e da versão em
uso — não podem ser adivinhados. Ver `DECISOES-ABERTAS.md`, D-03.

**Relevância.** Nomes estrangeiros aparecem em diários oficiais: contratos com
fornecedores, nomes de pessoas naturalizadas, títulos de obras.

**Como um porte ingênuo quebraria.** Usar apenas decomposição canônica deixa
`ø` intacto no índice. Uma expressão cadastrada como `SORENSEN` — já sem acento,
como o cadastro tende a estar — não casa com um índice que contém `søRENSEN`
normalizado para `søRENSEN`. O recorte simplesmente não aparece.

**Método de verificação.** Tabela **gerada**, não escrita à mão: percorrer todas
as runas do plano básico multilíngue, comparar a saída das duas implementações
e emitir apenas as divergências.

---

## INV-P08 — A ordem hífen → diacríticos é significativa

**Descrição.** O pipeline aplica a junção de hífens **antes** da remoção de
diacríticos (`main.rs:498–501`). Inverter muda o resultado, porque a junção
depende de `\w` casar caracteres acentuados (INV-P02).

**Caso positivo** — ordem correta:

| Entrada | Após junção | Após diacríticos |
|---|---|---|
| `"acentuaçã-\n"` + `"o final\n"` | `"acentuaçã"` + `"o final\n"` | `"acentuacao final\n"` |

**Caso negativo** — ordem invertida produz resultado diferente **apenas se** a
implementação de junção usar uma classe de caracteres ASCII. Com `\w` Unicode
correto (INV-P02), as duas ordens coincidem para este exemplo. O teste
normativo é: **a ordem declarada é hífen → diacríticos**, e a suíte deve conter
um caso que falhe se a ordem for trocada em conjunto com um `\w` ASCII — a
combinação de erros que um porte ingênuo produz naturalmente.

**Como um porte ingênuo quebraria.** Um porte que remova acentos primeiro
"conserta" acidentalmente o problema do `\w` ASCII, mascarando INV-P02 nos
testes e produzindo divergência apenas nos caracteres que a decomposição
canônica não cobre (INV-P07). É o pior tipo de defeito: silencioso e raro.

---

## INV-P09 — Estrutura de blocos, linhas e quebras

**Descrição.** A montagem do texto da página é literal (`main.rs:492–505`):

- iteração blocos → linhas → caracteres;
- cada linha é **aparada** e recebe **exatamente um** `\n`;
- as linhas são concatenadas **sem separador adicional**;
- **não há** fronteira entre blocos no texto final.

**Caso positivo** — página com dois blocos, dois parágrafos cada:

```
Entrada estrutural:            Texto resultante:
bloco A                        "linha 1\nlinha 2\nlinha 3\nlinha 4\n"
  linha "  linha 1  "
  linha "linha 2"
bloco B
  linha "linha 3"
  linha "  linha 4"
```

**Caso negativo** — o texto resultante **não** contém:

| Artefato | Presente? |
|---|---|
| linha em branco entre blocos | não |
| `\n\n` em qualquer posição | não, salvo se uma linha aparada for vazia |
| espaço no início ou fim de linha | não — removido pelo aparo |
| `\r\n` | não |

**Como um porte ingênuo quebraria.** Serializadores de texto de bibliotecas de
PDF costumam inserir uma linha em branco entre blocos ou preservar espaços de
posicionamento. Como a quebra de linha é separador de termos, uma linha em
branco a mais **não** altera a busca de frase; mas espaços preservados no início
da linha também não alteram. O que altera é a **segmentação de linhas em si**:
uma biblioteca que junte duas linhas visuais em uma só faz uma frase passar a
casar; uma que quebre onde o legado não quebra faz uma frase deixar de casar.

---

## INV-P10 — Caractere irrecuperável vira espaço

**Descrição.** `main.rs:496`: `.map(|c| c.char().unwrap_or(' '))`. Todo caractere
que o MuPDF não consegue converter vira **espaço** `U+0020`.

**Caso positivo.** Uma página com um glifo não mapeável entre duas palavras
produz `palavra espaco palavra`, e portanto **dois termos distintos**.

**Caso negativo.** O caractere **não** é descartado. Se fosse, as duas palavras
virariam um único termo colado.

| Sequência original | Comportamento correto | Comportamento se descartado |
|---|---|---|
| `JOAO` + glifo inválido + `SILVA` | termos `joao`, `silva` | termo único `joaosilva` |

**Como um porte ingênuo quebraria.** Uma ligação que pule caracteres não
mapeáveis cola os termos vizinhos. O efeito é duplo: a frase `joao silva` deixa
de casar, e o termo colado `joaosilva` pode ultrapassar o limite de comprimento
do INV-P03 e sumir do índice.

---

## INV-P11 — Granularidade da ligação com o MuPDF

**Descrição.** O legado percorre a estrutura de texto em três níveis
(`main.rs:492–496`). Uma ligação Go que exponha apenas "o texto da página"
produz uma segmentação **próxima, não igual**.

**Caso positivo.** Para todos os documentos do corpus, o texto por página
produzido pela implementação Go é **idêntico byte a byte** ao capturado do
legado.

**Caso negativo.** Qualquer divergência — inclusive de um único espaço ou de uma
única quebra — reprova. Não existe "quase igual" nesta invariante, porque a
segmentação determina quais frases atravessam quebras (INV-P05, INV-P09).

**Como um porte ingênuo quebraria.** Adotar a primeira ligação disponível sem
comparar contra o corpus. A fase F5 exige um relatório de avaliação **antes** da
implementação, justamente para tornar essa escolha uma decisão informada.

---

## INV-P12 — Deduplicação por perfil, atravessando expressões

**Descrição.** O conjunto de páginas já vistas é reiniciado **apenas quando o
identificador de perfil muda** (`main.rs:287–290`), acumulando ao longo de todas
as expressões daquele perfil.

**Caso positivo** — perfil `7` com as expressões `ALFA` e `BETA`, ambas presentes
na página 3:

| Chave processada | Páginas encontradas | Recortes gravados |
|---|---|---|
| `(7, ALFA)` | `{3}` | 1 recorte: página 3, expressão `ALFA` |
| `(7, BETA)` | `{3}` | **nenhum** — página já consumida |

Total: **um** recorte, atribuído a `ALFA` (alfabeticamente anterior).

**Caso negativo** — as mesmas expressões em perfis diferentes:

| Chave processada | Recortes gravados |
|---|---|
| `(7, ALFA)` | 1 recorte: página 3, perfil 7 |
| `(8, BETA)` | 1 recorte: página 3, perfil 8 |

Total: **dois** recortes. Perfis não interferem entre si.

**Dependência da ordenação.** "Alfabeticamente anterior" é determinado pelo
`ORDER BY tp.id_perfil, tpv.expressao_nm` (`main.rs:560–562`) e portanto pelo
`COLLATE` do banco. Ver `DECISOES-ABERTAS.md`, D-02.

**Como um porte ingênuo quebraria.** Três formas, todas naturais:
(a) reiniciar o conjunto a cada expressão, gerando recortes duplicados;
(b) processar as chaves em paralelo para ganhar desempenho, tornando o resultado
não determinístico;
(c) alterar ou remover o `ORDER BY`, mudando qual expressão é registrada.

**Restrição derivada:** o processamento das chaves **não pode ser
paralelizado**.

---

## INV-P13 — O contador de perfil corrente inicia em zero

**Descrição.** `main.rs:281`: `let mut id_perfil: i64 = 0;`. Se existir um perfil
com `id_perfil = 0`, a comparação em `287` é falsa na primeira chave e o
conjunto de páginas **não é reiniciado** — herdando o estado inicial (vazio, o
que é inofensivo) mas também não atualizando a variável de controle.

**Análise.** Com o conjunto já vazio no início, o efeito prático é nulo: a
primeira chave se comporta igual. A divergência só apareceria se o conjunto
fosse reutilizado entre importações, o que não ocorre.

**Caso positivo.** Não existindo perfil `0`, a primeira chave sempre dispara o
reinício e a semântica é a do INV-P12.

**Caso negativo.** Existindo perfil `0`, a primeira chave **não** dispara o
reinício — comportamento idêntico na prática, mas a implementação Go deve
reproduzir a estrutura para não depender dessa análise.

**Decisão pendente.** Ver `DECISOES-ABERTAS.md`, D-01. Padrão provisório: usar
sentinela explícito (ponteiro nulo ou booleano de primeira iteração), que é
equivalente nos dois cenários, e documentar.

**Como um porte ingênuo quebraria.** Não quebraria — esta invariante existe para
registrar que a análise foi feita e para impedir que alguém "conserte" a
variável para `-1` sem entender que a equivalência precisa ser demonstrada.

---

## INV-P14 — Trabalho parcial sobrevive ao erro

**Descrição.** Falha ao gravar um recorte marca a importação como −1 e
**retorna**, abandonando as chaves restantes. Tudo que já foi gravado
**permanece**. Não há compensação.

**Origem.** `main.rs:310–316` (falha na gravação), `318–322` (falha no recorte).

**Caso positivo** — cinco chaves, falha ao gravar a terceira:

| Chave | Resultado |
|---|---|
| 1 | recortes gravados, **permanecem** |
| 2 | recortes gravados, **permanecem** |
| 3 | falha ⇒ status −1, importação encerrada |
| 4 | **não processada** |
| 5 | **não processada** |

Estado final: `status = -1`; recortes das chaves 1 e 2 presentes; `data_fim` e
`total_recortes` **não gravados**.

**Caso negativo.** O estado final **não** é: banco limpo, nem recortes das cinco
chaves, nem `status = 5`.

**Como um porte ingênuo quebraria.** Envolver a importação inteira em uma
transação — a "correção" mais óbvia — reverte os recortes das chaves 1 e 2,
mudando o estado final observável. Por isso a transação por importação é uma
evolução atrás de chave (F11), e a fase F4 usa **uma transação por chamada de
gravação**, que preserva exatamente este comportamento.

### DIVERGÊNCIA CONHECIDA — o que acontece DENTRO da chave que falha

A tabela acima é silenciosa sobre a chave 3, e há uma diferença real ali.

`salvar_recorte` do legado (`main.rs:573–631`) **não abre transação nenhuma** —
o próprio código traz o comentário `// TODO: Implementar controle de transação?`.
Cada par de `INSERT` é confirmado sozinho. Se a chave 3 tem cinco recortes e a
gravação falha no terceiro, os **dois primeiros permanecem**.

O porte usa uma transação por CHAMADA, o que torna a chave inteira atômica: na
mesma situação, a chave 3 não deixa **nenhum** recorte.

| | Legado | Porte |
|---|---|---|
| Chaves 1 e 2 | permanecem | permanecem |
| Chave 3 (falha no 3º de 5 recortes) | **2 recortes permanecem** | **0 recortes** |
| Linha órfã em `tb_recorte` sem texto | **possível** | impossível |
| Chaves 4 e 5 | não processadas | não processadas |
| Status final | −1 | −1 |

**Por que é assim.** É consequência direta do achado **A03**: não há como tornar
o par `tb_recorte`/`tb_recorte_texto` atômico sem transação, e a transação por
chamada arrasta a chave inteira junto.

**Como reproduzir o legado com exatidão sem reintroduzir a linha órfã.** Uma
transação **por par de recortes**, em vez de por chamada — o par fica atômico
(A03 corrigido) e o trabalho parcial da chave que falha sobrevive (INV-P14
exato). O custo é uma transação por recorte em vez de uma por chave.

**Alcance.** Só o caminho de FALHA de gravação, só a chave que falha, e só
quando ela falha depois de já ter gravado algo. O caminho feliz é idêntico.
Registrado em `docs/DECISOES-ABERTAS.md`, **D-22**.

---

## INV-P15 — Estouro numérico é erro, não truncamento

**Descrição.** As conversões estreitantes do legado usam `try_from`, que
**falha** em vez de truncar.

**Origem.** `main.rs:603` (`i64::try_from(recorte.page)`), `main.rs:712`
(`i32::try_from(nr_recortes)`).

**Caso positivo:**

| Valor | Conversão | Resultado |
|---|---|---|
| `2_147_483_647` | `i32::try_from` | sucesso |
| `2_147_483_648` | `i32::try_from` | **erro** |
| `u64::MAX` | `i64::try_from` | **erro** |

**Caso negativo.** A conversão **não** produz um valor truncado, negativo ou
qualquer outro número. Ela produz erro.

**Como um porte ingênuo quebraria.** Go converte silenciosamente:
`int32(2147483648)` devolve `-2147483648` sem aviso do compilador nem do tempo
de execução. Um total de recortes acima do limite seria gravado como número
negativo em vez de deixar a coluna intocada (ver INV-P18).

---

## INV-P16 — Datas não têm fuso horário

**Descrição.** `chrono::NaiveDate` (`main.rs:419–421`) não carrega fuso e mapeia
para `date` do PostgreSQL. A análise usa `%Y-%m-%d` (`main.rs:152`, `165`).

**Caso positivo.** A entrada `2024-03-15` é gravada e lida como `2024-03-15`,
**independentemente** do fuso do processo, do fuso do servidor de banco e da
hora do dia.

**Caso negativo.** A data gravada **não** pode ser `2024-03-14` nem `2024-03-16`
sob nenhuma configuração de fuso.

**Como um porte ingênuo quebraria.** Em Go, `time.Parse("2006-01-02", s)` devolve
um `time.Time` em UTC; se o código usar `time.ParseInLocation` com o local do
servidor, ou se o driver converter para `timestamptz` no caminho, uma data pode
deslocar um dia. O teste normativo executa a mesma gravação com o fuso do
processo definido para uma zona a leste e outra a oeste de Greenwich.

---

## INV-P17 — Aspas na expressão abortam a importação inteira

**Descrição.** A consulta é montada por interpolação sem escape
(`main.rs:375`): `format!(r#""{key}""#)`. Uma expressão contendo `"` produz
consulta sintaticamente inválida, `parse_query` devolve `Err`, o `?` propaga,
`recortar` devolve `Err`, e o ramo de erro em `318–322` grava −1 e **encerra a
importação**.

**Caso positivo** — uma única expressão malformada no cadastro de **qualquer**
perfil associado ao caderno derruba o processamento do documento inteiro:

| Expressão cadastrada | Efeito |
|---|---|
| `EMPRESA "X" LTDA` | importação vai a −1 no momento em que essa chave é alcançada |

As chaves processadas **antes** dela mantêm seus recortes gravados (INV-P14).

**Caso negativo.** Expressões com outros caracteres especiais do analisador de
consulta permanecem literais dentro das aspas e **não** abortam:

| Expressão | Efeito |
|---|---|
| `ACME + FILHOS` | busca de frase normal |
| `ACME (LTDA)` | busca de frase normal |
| `ACME: FILHOS` | ⚠ confirmar — `:` tem significado de campo no analisador |

**Como um porte ingênuo quebraria.** Uma implementação Go que escape as aspas
ou que trate a expressão como literal **não abortaria** — e passaria a produzir
recortes para importações que hoje falham. É divergência para mais.

**Decisão pendente.** Ver `DECISOES-ABERTAS.md`, D-05: reproduzir a falha é o
padrão, mas é preciso confirmar se existem expressões com aspas em produção.

---

## INV-P18 — Estouro de `total_recortes` não grava `data_fim`

**Descrição.** `registrar_termino_importacao` converte com `i32::try_from`
**antes** de executar o UPDATE (`main.rs:712`). Falhando a conversão, a função
devolve `Err` sem executar comando algum, e o resultado é descartado por
`let _ =` em `main.rs:326`.

**Caso positivo** — importação com mais de `2 147 483 647` recortes:

| Coluna | Valor final |
|---|---|
| `status` | `5` — já gravado em `main.rs:325` |
| `data_fim` | **não gravado** |
| `total_recortes` | **não gravado** |

**Caso negativo.** O valor **não** é gravado truncado, negativo ou saturado no
máximo. A importação **não** vai a −1: ela fica em `5` com as colunas de término
vazias.

**Como um porte ingênuo quebraria.** Duas formas: truncar silenciosamente (ver
INV-P15) gravando um número negativo; ou propagar o erro e levar a importação a
−1, mudando o status final observável.

**Relevância prática.** O cenário é improvável — exige mais de dois bilhões de
recortes em um documento — mas o caminho de código existe e é barato de cobrir.
Ver `DECISOES-ABERTAS.md`, D-04, sobre o volume real observado.

---

## INV-P19 — Expressão de busca acentuada nunca casa

**Descrição.** O texto indexado passa por `remove_diacritics` (`main.rs:501`).
A **expressão de busca não passa**: ela vai do banco direto para a montagem da
consulta (`main.rs:283` → `375`), e o analisador do Tantivy aplicado à consulta
faz segmentação, descarte por comprimento e conversão para minúsculas — mas
**não** remove acentos.

Consequência: uma `expressao_nm` que contenha qualquer diacrítico produz termos
que **não existem no índice** e, portanto, **jamais gera recorte**.

**Origem.** Assimetria entre `main.rs:501` (texto) e `main.rs:283, 375`
(expressão).

**Caso positivo** — página contendo `MARIA DA CONCEIÇÃO SOUZA`, indexada como
`maria da conceicao souza`:

| Expressão cadastrada | Termos da consulta | Casa? |
|---|---|---|
| `CONCEICAO SOUZA` | `conceicao`, `souza` | **sim** |
| `conceicao souza` | `conceicao`, `souza` | **sim** |

**Caso negativo** — a mesma página, expressões com acento:

| Expressão cadastrada | Termos da consulta | Casa? |
|---|---|---|
| `CONCEIÇÃO SOUZA` | `conceição`, `souza` | **não** |
| `CONCEIÇÃO` | `conceição` | **não** |
| `AÇÃO` | `ação` | **não** |

**Efeito colateral no filtro do operador `&`.** O filtro de `main.rs:392–398`
compara a expressão **acentuada** contra o texto **já normalizado**. Uma
expressão como `AÇÃO & CIA` falha duas vezes: na busca de frase e no filtro.

**Relevância.** Isto significa que **perfis com expressões acentuadas estão
silenciosamente inertes em produção hoje** — nunca produziram um recorte. É um
defeito de produto com impacto real, não uma curiosidade técnica.

**Tratamento.** `DEFEITO PRESERVADO`. A reescrita reproduz a assimetria: o
texto é normalizado, a expressão não. Corrigir dentro da migração alteraria o
conjunto de recortes de forma massiva e inesperada.

**Ação recomendada, fora do escopo da migração.** Levantar quantos perfis estão
afetados e escalar para produto:

```sql
SELECT tp.id_perfil, tc.id_cliente, tpv.expressao_nm
FROM recorte.tb_perfil_variacao tpv
  JOIN recorte.tb_perfil tp ON tp.id_perfil = tpv.id_perfil
  JOIN recorte.tb_cliente tc ON tc.id_cliente = tp.id_cliente
WHERE tpv.expressao_nm ~ '[^\x00-\x7F]'
  AND tc.status = 'A'
ORDER BY tp.id_perfil;
```

Ver `DECISOES-ABERTAS.md`, D-17.

**Como um porte ingênuo quebraria.** Duas formas, ambas naturais: (a) aplicar a
normalização de texto também à expressão, "consertando" o defeito e fazendo
aparecer recortes que hoje não existem — para clientes que não os esperam;
(b) normalizar a expressão apenas no filtro do `&` e não na busca de frase,
produzindo comportamento inconsistente entre expressões com e sem `&`.

---

## INV-P20 — PDF truncado conclui com sucesso e zero recortes

**Descrição.** Entrada inválida **não** produz um comportamento único. Medido
empiricamente com o corpus sintético, o MuPDF trata três formas de arquivo
inválido de três maneiras diferentes, e apenas duas levam a importação a −1.

**Medição** (mupdf 0.4.4, documentos `20`, `21` e `22` do corpus):

| Documento | Erro do MuPDF | Páginas extraídas | Status final | Recortes |
|---|---|---|---|---|
| PDF truncado a 1/3 do tamanho | **nenhum** | **0** | **`5` finalizado** | 0 |
| Arquivo de 0 bytes | `cannot tell in file` | — | `-1` erro | 0 |
| Texto puro com extensão `.pdf` | `no objects found` | — | `-1` erro | 0 |

**Caso positivo.** Um PDF truncado é aceito por
`mupdf::Document::from_bytes` (`main.rs:482`), `documento.pages()` devolve zero
páginas, o laço de indexação não executa, o índice fica vazio, todas as buscas
devolvem zero acertos, e a importação chega a `status = 5` com
`total_recortes = 0` e `data_fim` preenchido.

Do ponto de vista do banco, **é indistinguível de um diário legítimo em que
nenhum perfil teve ocorrência**.

**Caso negativo.** O arquivo vazio e o arquivo que não é PDF **não** seguem esse
caminho: erram na abertura e vão a `-1`.

**Relevância operacional.** Um PDF corrompido em trânsito — truncado por falha
de rede ou de disco no cliente — é registrado como processado com sucesso. Não
há alarme, não há status de erro, e os recortes daquele diário simplesmente não
existem. É um modo de falha silenciosa em produção **hoje**.

**Tratamento.** `DEFEITO PRESERVADO`. A reescrita reproduz os três caminhos. A
detecção (por exemplo, alertar quando um documento produz zero páginas) é
candidata a evolução em F11, atrás de chave, e deve ser escalada para produto
independentemente da migração.

**Como um porte ingênuo quebraria.** Duas formas: (a) validar a assinatura
`%PDF-` ou a integridade do arquivo por conta própria e levar o truncado a −1,
mudando o status observável; (b) tratar "zero páginas" como erro. As duas
"melhoram" o comportamento e divergem do legado.

**Ressalva.** A fronteira exata entre "truncado e aceito" e "truncado e
rejeitado" depende de **onde** o corte ocorre — um arquivo cortado antes do
cabeçalho falha, um cortado após a tabela de referências pode abrir
parcialmente. O corpus fixa um caso reprodutível (corte em 1/3); o
comportamento para outros pontos de corte não é especificado e não deve ser
assumido.

---

## INV-P21 — A análise de data do chrono é mais permissiva que a do Go

**Descrição.** As duas datas do formulário são analisadas com
`chrono::NaiveDate::parse_from_str(v, "%Y-%m-%d")`
(`main.rs:152` e `165`). O analisador do chrono é **substancialmente mais
permissivo** que `time.Parse("2006-01-02", s)` do Go.

A consequência é de contrato: uma entrada que o legado aceita e o Go rejeita
passa a receber a crítica `Data do caderno é inválida`, o que **muda o corpo da
resposta 400**.

**Origem da medição.** Executada na fase F3 contra chrono 0.4 e Go 1.24.7. A
gramática abaixo foi medida, não deduzida — a especificação afirmava o oposto
(`INFERIDO — confirmar`) e foi **refutada**.

```
data  := ws* ano '-' ws* mes '-' ws* dia FIM
ano   := sinal? digitos      // com sinal: até 6 dígitos; sem sinal: até 4
mes   := digitos             // 1..2 dígitos
dia   := digitos             // 1..2 dígitos
```

**Caso positivo — aceitos pelo legado, rejeitados por `time.Parse`:**

| Entrada | chrono | valor | `time.Parse` |
|---|---|---|---|
| `2024-3-15` | aceita | 2024-03-15 | **rejeita** |
| `2024-03-5` | aceita | 2024-03-05 | **rejeita** |
| `24-03-15` | aceita | **0024**-03-15 | **rejeita** |
| `  2024-03-15` | aceita | 2024-03-15 | **rejeita** |
| `+2024-03-15` | aceita | 2024-03-15 | **rejeita** |
| `-2024-03-15` | aceita | −2024-03-15 | **rejeita** |
| `+12345-03-15` | aceita | 12345-03-15 | **rejeita** |

Note `24-03-15`: o valor gravado é o ano **24**, não 2024. Não é só uma questão
de aceitar ou recusar — o valor persistido também diverge.

**Caso negativo — rejeitados pelos dois:**

| Entrada | Motivo |
|---|---|
| `2024-03-15 ` | espaço à **direita** não é tolerado |
| `2024-03-15\n` | idem |
| `2024 -03-15` | espaço antes do `-` literal |
| `2024-03-15T00:00:00` | sobra |
| `20240315` | sem separador |
| `2024/03/15` | separador errado |
| `12345-03-15` | cinco dígitos **sem** sinal |
| `2024-13-01`, `2024-00-15`, `2024-03-00` | fora do intervalo |
| `2023-02-29`, `1900-02-29` | não bissexto |

**Assimetria do espaço em branco.** É ignorado **antes de cada campo
numérico** e não antes do `-` literal. Daí `2024- 03-15` passar e
`2024 -03-15` não.

**Como um porte ingênuo quebraria.** `time.Parse("2006-01-02", s)` é a tradução
óbvia e está errada em seis das dez formas testadas. O efeito é uma resposta
400 onde hoje há 200, para submissões que algum cliente já envia.

**Implementação.** `domain.AnalisarData` reproduz a gramática medida. O teste
`TestAnalisarDataDivergeDeTimeParse` falha se alguém "simplificar" a função
para `time.Parse`, e explica o motivo na mensagem.

---

## INV-P22 — As classes de caracteres do Rust divergem das do Go nos dois sentidos

**Descrição.** Duas decisões por caractere governam o pipeline, e **nenhuma das
duas** tem equivalente direto em Go. Pior: as diferenças vão nos **dois
sentidos**, então nenhuma aproximação simples acerta.

### 22.1 — `\w` da junção de hífens

O legado usa `(?imx)(\w+)(-\n)` (`main.rs:487`). No crate `regex`, `\w` é
`[\p{Alphabetic}\p{M}\p{Nd}\p{Pc}\p{Join_Control}]`.

| Candidato em Go | Erro |
|---|---|
| `\w` do RE2 | ASCII puro — perde toda letra acentuada (INV-P02) |
| `[\p{L}\p{N}_]` | **perde marcas combinantes** e **inclui `No`/`Nl` a mais** |

**Caso positivo** — o legado JUNTA e `[\p{L}\p{N}_]` não juntaria:

| Entrada | Resultado no legado |
|---|---|
| `"6"` + U+0327 (cedilha combinante) + `"-\n"` + `"x"` | junta → `6̧x` |
| U+0303 (til combinante) + `"-\n"` + `"l"` | junta → `̃l` |

`\p{M}` faz parte de `\w` no Rust; `\p{L}` e `\p{N}` do Go não cobrem marcas.

**Caso negativo** — o legado NÃO junta e `[\p{L}\p{N}_]` juntaria:

| Entrada | Resultado no legado |
|---|---|
| `"¼"` + `"-\n"` + `"93"` | **não junta** → `¼-\n93` |
| `"½"` + `"-\n"` + `"x"` | **não junta** |

`\p{N}` do Go inclui `No` (frações e expoentes); o `\w` do Rust só inclui `Nd`.

### 22.2 — `is_alphanumeric` da segmentação de termos

O `SimpleTokenizer` do Tantivy quebra em `!c.is_alphanumeric()`
(`simple_tokenizer.rs:46`). No Rust isso é `Alphabetic ∪ {Nd, Nl, No}`; em Go,
`unicode.IsLetter || unicode.IsDigit` é `L ∪ Nd`.

**Caso positivo** — um único termo no legado:

| Texto | Termos no legado | Termos com `IsLetter\|\|IsDigit` |
|---|---|---|
| `m²` | `m²` | `m` |
| `½kg` | `½kg` | `kg` |
| `capítuloⅧ` | `capítuloⅷ` | `capítulo` |

**Relevância prática.** Área em `m²` é comum em extratos de contrato e editais
de obra. Uma expressão de perfil cadastrada como `350 M2` já não casaria, mas
o termo indexado muda de `m²` para `m` — o que altera o índice inteiro.

### Como foram resolvidas

**Nenhuma das duas foi escrita à mão.** `tools/gerar-tabela-diacriticos`
percorre todas as runas do plano básico multilíngue perguntando ao próprio
Rust — ao motor de expressões regulares para `\w`, e a `char::is_alphanumeric`
para a segmentação — e emite apenas as divergências:

| Tabela | Divergências | Arquivo gerado |
|---|---|---|
| `\w` da junção de hífens | 9 | `internal/adapter/pdftext/palavra_table.go` |
| Segmentação alfanumérica | 1.265 | `internal/adapter/searchidx/alfanumerico_table.go` |
| Diacríticos (INV-P07) | 2.205 | `internal/adapter/pdftext/diacriticos_table.go` |

### Como isto foi descoberto

**O corpus não pegou.** As 159 páginas passaram byte a byte com a
implementação errada. Foi o **teste de propriedade** com um milhão de cadeias
aleatórias que acusou **320.142 divergências** — 32% dos casos — e os exemplos
apontaram direto para as duas causas.

É a justificativa concreta para o teste de propriedade ser critério de aceite
da fase, e não opcional: nenhum corpus realista contém `¼-\n` nem `6̧-\n` em
volume suficiente para que a falha apareça.

---

## INV-P23 — O filtro do operador `&` só é compilado se houver acerto de frase

**Descrição.** A expressão regular do filtro é compilada **dentro do laço sobre
os resultados**, não antes dele. Uma expressão que contém `&` e cuja sintaxe é
inválida só chega a ser compilada se a busca de frase produzir **pelo menos um
acerto**. Sem acerto, o `.unwrap()` nunca roda e a importação segue normalmente.

**Origem.** `main.rs:386–398` — o `if key.contains('&')` está **dentro** do
`for (_score, doc_address) in top_docs`.

```rust
for (_score, doc_address) in top_docs {
    // ...
    if key.contains('&') {
        let exp = format!("(?imx){}", key.replace('&', r"\s*&\s*"));
        let re = regex::Regex::new(&exp).unwrap();   // ← só aqui
```

**Evidência empírica.** A captura do corpus da fase F7 registra o desfecho de
cada expressão por documento. A expressão `ACME & FILHOS (` — sintaxe inválida —
foi consultada contra os 26 documentos:

| Documento | Páginas de `ACME FILHOS` | Desfecho no legado |
|---|---|---|
| `08-inv-p01-e-comercial` | `[1, 2, 3]` | **pânico** |
| `23-f5-caracteres-de-marcacao` | `[1]` | **pânico** |
| `25-f5-mesma-linha-varios-desenhos` | `[1]` | **pânico** |
| os outros 23 documentos | `[]` | `ok`, zero recortes |

A correlação é exata: **pânico se e somente se houve acerto**.

**Caso positivo.** Expressão `acme & filhos (` contra um documento sem a frase
→ zero recortes, **sem erro**.

**Caso negativo.** A mesma expressão contra um documento **com** a frase →
`ErrExpressaoInvalida`.

**Como um porte ingênuo quebraria.** Compilar o filtro logo após tokenizar a
expressão — o que é a ordem natural em Go — transformaria 23 importações
bem-sucedidas em 23 falhas. O harness de paridade da fase F7 mede isso: a
sabotagem que antecipa a compilação produz **69 combinações divergentes de
1.378**.

**Verificação.** `TestFiltroDoOperadorSoRodaComAcerto`, e o campo `desfecho` de
`test/testdata/expected/*.busca.json`.
