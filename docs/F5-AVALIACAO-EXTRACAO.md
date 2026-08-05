# F5 — Relatório de avaliação da extração de texto

> Artefato do **passo 1** do procedimento da fase F5, que exige a avaliação
> **antes** de qualquer implementação.
>
> Ferramenta: `tools/avaliar-extracao`. Reprodutível com
> `go run ./tools/avaliar-extracao`.
>
> Oráculo: `test/testdata/expected/*.paginas-brutas.json` — o texto **antes**
> da normalização, capturado na fase F0 pelo binário Rust que reusa o pipeline
> do legado. A normalização é a fase F6; usar o texto normalizado aqui tornaria
> uma falha de extração indistinguível de uma de normalização.

---

## 1. O que o legado faz

`reference/main.rs:489-505`:

```rust
for page in documento.pages()? {
    let text_page = page?.to_text_page(TextPageOptions::empty())?;
    let mut pagina = vec![];
    for block in text_page.blocks() {
        for line in block.lines() {
            let chars = format!("{}\n", line.chars()
                .map(|c| c.char().unwrap_or(' '))
                .collect::<String>().trim());
            pagina.push(chars);
        }
    }
    paginas.push(pagina.join(""));
}
```

Três propriedades definem o resultado:

1. a iteração é **blocos → linhas → caracteres**;
2. cada **linha** é aparada e recebe **exatamente um** `\n`;
3. as linhas são concatenadas **sem separador entre blocos** — a fronteira de
   bloco não aparece na saída.

A consequência de (3) é importante para a avaliação: **só as fronteiras de
LINHA são observáveis**. Onde o MuPDF decide começar um bloco novo é
irrelevante para a saída.

## 2. Ligação avaliada

`github.com/gen2brain/go-fitz` v1.28.2 — a ligação Go mais madura para MuPDF.

Ela **embarca** o MuPDF como biblioteca estática e o usa por cgo, o que dispensa
instalar `libmupdf-dev`. Com `CGO_ENABLED=0` ela recorre ao `purego` e passa a
exigir um `libmupdf.so` compartilhado em tempo de execução — ver §8, onde isso
está medido e registrado como armadilha.

A API expõe apenas saídas prontas, **sem acesso a `fz_stext_page`**:

```
func (f *Document) Text(pageNumber int) (string, error)
func (f *Document) HTML(pageNumber int, header bool) (string, error)
func (f *Document) SVG(pageNumber int) (string, error)
```

Isso confirma a previsão de INV-P11: nenhuma ligação de alto nível expõe a
granularidade de blocos e linhas diretamente. Duas estratégias foram medidas.

## 3. Estratégia "texto" — `doc.Text(n)` — **REPROVADA**

| Métrica | Resultado |
|---|---|
| Documentos idênticos byte a byte | **7 de 21** |
| Páginas idênticas byte a byte | **20 de 154** |
| Classes de divergência | 1 — "apenas espaçamento e quebras de linha" (134 páginas) |

Toda divergência é da mesma classe: o conteúdo não branco é idêntico, a
segmentação não é.

**Causa raiz.** `fz_print_stext_page_as_text` **junta as linhas de um bloco com
espaço** e só emite `\n` na fronteira de bloco. O legado emite `\n` por linha.

Exemplo de `03-inv-p02-hifen-ascii`, página 1, byte 72:

```
go-fitz : Fica·determinada·a·conti-◆nuacao·do·processo·administrativo·referente·ao·servidor·JOAO·SILVA.\n
legado  : Fica·determinada·a·conti-◆\nnuacao·do·processo·administrativo\nreferente·ao·servidor·JOAO·SILVA.\n
```

**Por que isso não é cosmético.** A quebra de linha é separador de termos
(INV-P05, INV-P09), e o `\n` é o gatilho da junção de hífens (INV-P02):

- Legado: `conti-\n` + `nuacao` → o padrão `(\w+)(-\n)` casa → termo
  `continuacao`.
- `Text()`: `conti-nuacao` — o MuPDF já aplicou a **própria** regra de
  de-hifenização, mantendo o hífen e sem `\n`. O padrão do legado **não casa**,
  e o hífen é separador → dois termos, `conti` e `nuacao`.

Ou seja: a expressão `CONTINUACAO DO PROCESSO` gera recorte no legado e **não
gera** com esta estratégia. Divergência de produto, não de formatação.

## 4. Estratégia "html" — `doc.HTML(n)` remontado — **APROVADA**

O escritor HTML do MuPDF emite **um `<p>` por linha do `fz_stext_page`**:

```html
<p style="top:83.2pt;left:56.0pt;line-height:11.0pt"><span …>Fica determinada a conti-</span></p>
<p style="top:99.2pt;left:56.0pt;line-height:11.0pt"><span …>nuacao do processo administrativo</span></p>
<p style="top:115.2pt;left:56.0pt;line-height:11.0pt"><span …>referente ao servidor JOAO SILVA.</span></p>
```

Isso é exatamente a granularidade que o laço do legado precisa. A remontagem é
literal: texto de cada `<p>` → aparar → acrescentar `\n` → concatenar.

| Métrica | Resultado |
|---|---|
| Documentos idênticos byte a byte | **26 de 26** |
| Páginas idênticas byte a byte | **159 de 159** |
| Divergências | **nenhuma** |

### 4.1 Casos adversariais acrescentados ao corpus

O corpus da F0 é sintético e gerado por reportlab, que desenha cada linha como
um objeto de texto separado — o que poderia fazer a estratégia funcionar por
acidente. Cinco documentos foram acrescentados **especificamente para atacá-la**,
e todos passaram:

| Documento | O que ataca |
|---|---|
| `23-f5-caracteres-de-marcacao` | `<`, `>`, `&`, aspas e entidades literais no texto — escape e desescape do HTML |
| `24-f5-linhas-coladas` | espaçamento de 2, 4 e 6 pt — força a heurística de agrupamento de linhas |
| `25-f5-mesma-linha-varios-desenhos` | trechos na mesma coordenada vertical, desenhados separadamente — devem virar UMA linha |
| `26-f5-ordem-de-desenho-invertida` | linhas desenhadas de baixo para cima — ordem do fluxo ≠ ordem visual |
| `27-f5-tamanhos-mistos` | fontes de tamanhos diferentes na mesma linha — o MuPDF quebra em `span`s distintos |

O caso `25` é o mais informativo: três `drawString` na mesma altura produzem um
único `<p>`, igual à única linha que o laço do Rust vê. Isso indica que a
correspondência `<p>` ⟺ linha do `stext` é **estrutural**, não coincidência do
gerador.

## 5. Recomendação

**Caminho A′ — `go-fitz` com remontagem a partir da saída HTML.**

O roadmap previa três caminhos. A medição encontrou um quarto, melhor que os
dois primeiros:

| Caminho | Situação |
|---|---|
| A — ligação existente com `Text()` | **reprovado**: 20 de 154 páginas |
| **A′ — ligação existente com `HTML()`** | **aprovado: 159 de 159 páginas** |
| B — *shim* em C sobre `fz_stext_page` via cgo | desnecessário |
| C — extrator Rust como serviço lateral | desnecessário |

O caminho A′ é preferível a B por três razões concretas:

1. **Empacotamento trivial.** O MuPDF embarcado é ligado estaticamente: nada
   de `libmupdf-dev` no estágio de construção, nada de bibliotecas
   compartilhadas no de distribuição. A pendência que a fase F1 registrou no
   `deploy/Dockerfile` **desaparece** — ver §8.
2. **Sem código C próprio.** Um *shim* sobre `fz_stext_page` seria mais código
   nosso, com gestão manual de ciclo de vida de recursos nativos, para chegar
   ao mesmo resultado já medido.
3. **Reversível.** A implementação fica atrás da porta `domain.ExtratorTexto`.
   Se o corpus real revelar divergência, trocar por B ou C é substituir um
   adaptador.

## 6. Riscos residuais — o que esta medição NÃO prova

Registrados para que a decisão seja informada, não otimista.

| Risco | Situação |
|---|---|
| **O corpus é sintético.** Diários reais têm digitalização, colunas irregulares, tabelas, texto rotacionado e camada de OCR. | **Não coberto.** É a pendência **D-11**, que segue bloqueante para os critérios de saída da F12. |
| **INV-P10** — caractere irrecuperável vira espaço. O escritor HTML pode representá-lo de outra forma. | **Não exercitado** pelo corpus: nenhum documento tem glifo não mapeável. Precisa de PDF real com fonte quebrada. |
| **Custo.** A saída HTML é bem maior que a de texto. | Medido na §7. |
| **Análise do HTML.** Extrair texto de HTML com expressão regular é frágil. | Mitigado: a implementação usa o analisador de `golang.org/x/net/html`, não expressão regular. A ferramenta de avaliação usa regex por ser descartável. |
| **Versão do MuPDF.** O formato de saída do escritor HTML pode mudar entre versões. | Mitigado pelo teste de paridade sobre o corpus, que roda na integração contínua. Fixar a versão de `go-fitz` é obrigatório. |

## 7. Custo medido

Documento de 120 páginas (`18-volume-120-paginas.pdf`), nesta máquina:

| Métrica | Valor |
|---|---|
| Tempo por documento | **17,6 ms** |
| Tempo por página | ~0,15 ms |
| Alocações | 708 KB, 1.802 alocações por documento |
| 200 extrações consecutivas | 3,17 s, heap **estável** (fator 0,73) |

O custo da saída HTML, maior que a de texto, é irrelevante nesta escala. A
extração não é o gargalo do pipeline.

## 8. Consequência para o empacotamento

O `go-fitz` **embarca o MuPDF como biblioteca estática**
(`libmupdf_linux_amd64.a` + `libmupdfthird_linux_amd64.a`, ~22 MB). Duas
consequências medidas:

1. **Nenhum pacote de MuPDF precisa ser instalado.** O `deploy/Dockerfile` e o
   fluxo de integração contínua perderam a instalação de `libmupdf-dev`,
   `libfreetype6-dev`, `libjbig2dec0-dev`, `libjpeg62-turbo-dev`,
   `libopenjp2-7-dev`, `libharfbuzz-dev` e `zlib1g-dev`.
2. **O binário depende só de libc:**

   ```
   $ ldd recorte-api
   linux-vdso.so.1
   libc.so.6 => /lib/x86_64-linux-gnu/libc.so.6
   /lib64/ld-linux-x86-64.so.2
   ```

   A rota (a) prevista na fase F1 se confirmou: nada a copiar para o estágio de
   distribuição. A pendência que a F1 deixou no `Dockerfile` está **resolvida**.

### Armadilha registrada: `CGO_ENABLED=0` compila e quebra em execução

Testado. Com cgo desligado, o `go-fitz` recorre ao `purego` e passa a exigir um
`libmupdf.so` **compartilhado** em tempo de execução:

```
$ CGO_ENABLED=0 go build ./cmd/recorte-api   # compila, binário estático
$ ./recorte-api
panic: cannot load library: libmupdf.so: cannot open shared object file
```

A falha **não aparece na compilação**. Alguém tentando produzir um binário
estático "melhor" obteria um que quebra na primeira extração. Registrado em
comentário no `Dockerfile`, no fluxo de integração contínua e no `Makefile`.
