# Ferramental de apoio à migração

Ferramentas de desenvolvimento. **Nenhuma faz parte do serviço em Go.**

## `capturar-corpus/` — captura do corpus dourado

Binário Rust que reproduz *verbatim* o pipeline do serviço legado e grava o
resultado como oráculo de paridade. Entregável 4 da fase F0.

```
cargo build --release
./target/release/capturar-corpus <dir-corpus> <dir-saida> [--chaves ARQ] [--texto MODO]
```

Para cada PDF do diretório de entrada, grava quatro arquivos:

| Arquivo | Conteúdo | Oráculo da fase |
|---|---|---|
| `<nome>.paginas-brutas.json` | texto por página **antes** da normalização | F5 |
| `<nome>.paginas.json` | texto por página **depois** da normalização | F6 |
| `<nome>.tokens.json` | termos do índice por página | F6 |
| `<nome>.recortes.json` | recortes na ordem exata de gravação, com desfecho | F7, F8 |

Opções:

- `--chaves ARQ` — JSON `[{"id_perfil":N,"expressao_nm":"..."}]`.
  **A ordem do arquivo é normativa:** precisa reproduzir
  `ORDER BY tp.id_perfil, tpv.expressao_nm`, porque governa a deduplicação
  (INV-P12). Padrão: `<dir-corpus>/chaves.json`.
- `--texto completo|sha256` — `completo` (padrão) grava o texto integral de
  cada recorte; `sha256` grava só o resumo, para corpus grande.

### Correspondência com o legado

| Módulo | Origem em `reference/main.rs` |
|---|---|
| `extracao.rs` | `criar_indice`, parte de extração — linhas 479–506 |
| `indice.rs` | `criar_indice`, parte de indexação — 508–535; `recortar` — 361–410 |
| `laco.rs` | laço de recorte de `upload_pdf` — 276–326 |

Comportamentos do legado **preservados de propósito**, incluindo os que são
defeitos: reconstrução do esquema em `recortar` (A09), limite de 1.000.000 de
acertos (A17), compilação da expressão regular dentro do laço (A08), e a flag
`(?imx)` cujo modo *extended* é a divergência INV-P01.

### ⚠ Alinhamento de versões

As dependências **não** estão fixadas contra o `Cargo.lock` do serviço em
produção, que ainda não foi disponibilizado (D-15). Até isso acontecer, a saída
**não tem valor normativo**: o analisador léxico, a tabela de diacríticos e a
extração do MuPDF podem divergir da versão real.

O procedimento de alinhamento está no cabeçalho do `Cargo.toml`.

## `gerar-corpus-sintetico/` — corpus de ponte

```
python3 gerar.py [dir-saida]      # padrão: test/testdata/corpus
```

Gera 23 documentos determinísticos, cada um nomeado pela invariante que
exercita, mais `chaves.json` e `MANIFESTO.json`.

Requer `reportlab` e a fonte DejaVuSans (cobre `ø đ ß æ ł ħ`, necessários para
INV-P07).

**Este corpus não substitui o real** (D-11). Ele cobre as invariantes que
conseguimos antecipar; não cobre a variedade de layout, qualidade de
digitalização e vocabulário que só documentos de produção têm.

## Fluxo completo

```bash
python3 tools/gerar-corpus-sintetico/gerar.py test/testdata/corpus
cargo build --release --manifest-path tools/capturar-corpus/Cargo.toml
./tools/capturar-corpus/target/release/capturar-corpus \
    test/testdata/corpus test/testdata/expected

# verificação de determinismo (critério de aceite da F0)
./tools/capturar-corpus/target/release/capturar-corpus \
    test/testdata/corpus /tmp/segunda-execucao
diff -r test/testdata/expected /tmp/segunda-execucao   # sem saída
```
