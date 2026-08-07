# Relatório de paridade — evidência para a decisão de corte

**Fase F12** · portão do projeto · gerado em **2026-08-07**

> ### O ferramental descrito aqui NÃO está mais na árvore de trabalho
>
> Depois desta fase, o repositório foi enxugado para conter apenas o que o
> serviço precisa para rodar. Saíram: a referência em Rust, o corpus dourado e
> seus oráculos, `test/parity`, `test/e2e`, `test/carga`, `tools/comparador`,
> `tools/sombra` e todo o ferramental de captura.
>
> **Nada disso foi perdido** — está no histórico do git, no commit `2febb7a`:
>
> ```sh
> git show 2febb7a --stat            # o que existia
> git checkout 2febb7a -- test tools reference   # traz tudo de volta
> ```
>
> Os comandos citados neste relatório (`make parity`, `make load-test`,
> `make chaos-test`) só funcionam depois disso. Os **números** permanecem
> válidos: eles descrevem uma medição que aconteceu, sobre um código que
> continua no repositório.
>
> A recomendação abaixo **não muda** com a remoção: ela depende de D-11, D-15 e
> D-14, e nenhuma delas foi respondida.

---

## Recomendação

> ## NÃO CORTAR AINDA.
>
> A verificação **determinística** passou, e passou depois de encontrar e
> corrigir **dois defeitos graves** que seis fases de teste não pegaram. Isso é
> a evidência a favor.
>
> A verificação **empírica** — corpus real e execução em sombra — **não foi
> feita**, porque os insumos nunca chegaram. Sem ela, o que se sabe é que o
> serviço reproduz o legado sobre **28 documentos sintéticos que nós mesmos
> escrevemos**. Isso não é base para cortar.

O que falta é nomeado na §7, com responsável e critério de aceite. Nada disso é
trabalho de engenharia: é insumo.

---

## 1. O que foi verificado, e com que resultado

| Frente | Situação | Resultado |
|---|---|---|
| Comparador determinístico, 5 camadas | **executado** | zero divergência |
| Teste de propriedade do laço, 10.000 casos | **executado** | zero divergência |
| Teste de propriedade da normalização, 1.000.000 casos | **executado** | zero divergência |
| Teste de propriedade do filtro `&` | **executado** | zero divergência |
| Caos — SIGKILL por estágio | **executado** | estado equivalente ao legado |
| Caos — queda do banco durante o processamento | **executado** | processo sobrevive |
| Carga — dobro do pico *assumido* | **executado** | zero divergência de correção |
| Memória por importação | **medida** | ver §5 — e a régua de `OPERACAO.md` **não** se confirmou |
| **Execução em sombra** | **NÃO EXECUTADA** | ferramental pronto, insumo ausente — **D-14** |
| **Corpus real** | **NÃO OBTIDO** | **D-11**, bloqueante desde a fase F0 |
| Latência p99 comparada ao legado | **NÃO MEDIDA** | exige o legado em produção lado a lado |

---

## 2. Os dois defeitos que a fase encontrou

Ambos passaram por **F5, F6, F7, F8, F9, F10 e F11** com a suíte inteira verde.
Isso é o achado mais importante do relatório — mais até que os defeitos em si.

### 2.1 A normalização não era aplicada em produção

**O que era.** A raiz de composição injetava `pdftext.NovoExtrator()`, que
devolve texto **bruto**. O serviço indexava sem junção de hífens e sem remoção
de diacríticos. `pdftext.Normalizar` — 1.000.000 de casos de teste de
propriedade na fase F6 — era **código morto no caminho de produção**.

**Efeito observável**, medido sobre o corpus dourado:

| Invariante | Efeito |
|---|---|
| INV-P02 | palavra partida por hífen não era rejuntada; a frase que a atravessa não encontrava |
| INV-P07 | os acentos permaneciam, e expressão sem acento — a forma cadastrada — não casava |
| INV-P19 | **invertida**: expressão ACENTUADA, inerte no legado, passava a casar; a sem acento parava de casar |

Cinco dos 28 documentos do corpus divergiam. Em produção, o efeito seria
**recortes errados para todos os clientes**, em ambas as direções.

**Correção.** `pdftext.ExtratorNormalizado`, o decorador que a própria fase F5
havia previsto por escrito e que nunca foi construído. A raiz de composição
passa a injetá-lo por meio de `app.extratorDeProducao`.

**Guarda contra regressão.** `TestExtratorDeProducaoNormaliza` — verificado por
sabotagem: revertendo a raiz para o extrator cru, o teste falha.

### 2.2 O termo longo não deixava buraco na numeração de posições

**O que era.** No Tantivy, a posição é atribuída pelo **tokenizador**; o
descarte de termos longos é um **filtro** posterior que **não renumera**. Um
termo removido deixa um **buraco**. O porte numerava as posições pelo índice na
lista já filtrada.

**Efeito observável.** Em `alfa <termo de 40 bytes> beta`, o legado coloca
`alfa` em 0 e `beta` em 2, e a frase `"alfa beta"` **não casa**. O porte
colocava `beta` em 1 e a frase **casava** — encontrando ocorrências que o
serviço atual nunca encontrou.

Não é hipotético: diários contêm rotineiramente códigos de autenticação,
tabelas coladas e lixo de reconhecimento óptico nessa faixa de comprimento.

**Correção.** `searchidx.TokenizarComPosicao`, e a busca de frase passou a
exigir a **distância** que a expressão pede — o que também cobre o termo longo
no meio da própria expressão.

**Guarda contra regressão.** `TestPropriedadeDoLaco` — verificado por
sabotagem: voltando a numerar pela lista filtrada, 19 de 9.357 casos divergem.

### 2.3 Por que nenhuma fase anterior pegou

Cada camada se alimentava do **oráculo** da anterior, não da saída da anterior
em Go:

```
F5  extração  →  compara com paginas-brutas.json
F6  normaliza o ORÁCULO bruto  →  compara com paginas.json
F7  busca sobre o ORÁCULO normalizado  →  compara com busca.json
```

O isolamento é deliberado e correto: ele localiza a causa. Mas ele deixa a
**costura** entre camadas sem cobertura, e foi exatamente ali que os dois
defeitos moraram. O oráculo `recortes.json` — que mede o pipeline inteiro —
existia desde a **fase F0** e **nenhum teste o consumia**.

**A lição operacional:** um conjunto de testes de unidade todos verdes não diz
nada sobre a montagem. A fase F12 existe por isso, e se justificou.

---

## 3. Comparador determinístico

`tools/comparador`, alvo `make parity`. Relatório em texto e em JSON
(`build/paridade.json`), com divergências agrupadas por camada e classe e o
menor caso reprodutor de cada classe — reduzido automaticamente por linha e
depois por runa, com os pontos de código nomeados.

```
camada              unidade     iguais  divergentes       taxa
1-extracao          páginas        159            0    0.0000%
2-normalizacao      páginas        159            0    0.0000%
3-termos            páginas        159            0    0.0000%
4-recortes         recortes         38            0    0.0000%
5-estados        documentos         28            0    0.0000%
```

**Portão:** zero divergência nas camadas 4 e 5. As camadas 1 a 3 são
diagnósticas — uma diferença que não chega a mudar recorte não altera o banco.

**Códigos de saída:** `0` aprovado, `1` reprovado, `2` não consegui medir. A
distinção entre 1 e 2 impede que um corpus que deixou de ser gerado passe por
"sem divergência".

### O comparador foi verificado por sabotagem

Apontando-o para o extrator cru, ele reproduz as cinco divergências, classifica
(`diacritico`, `ordem`, `quantidade`, `desfecho`) e reduz ao caso mínimo:

```
[3] 4-recortes · diacritico
    documento: 12-inv-p03-termos-longos, página 1
    linha 8:
      esperado: "cacacacacacacacacacacacacacacacacacacaca"
      obtido:   "çãçãçãçãçãçãçãçãçãçãçãçãçãçãçãçãçãçãçãçã"
      primeira diferença na runa 0: U+0063 "c" vs U+00E7 "ç"
```

---

## 4. Teste de propriedade do laço

`TestPropriedadeDoLaco` — 10.000 documentos sintéticos, semente fixa,
comparados contra `oraculo-laco`, que é o porte verbatim do laço do legado em
Rust.

O gerador combina as oito construções pedidas: acentuação, hífen no fim da
linha, expressões com `&`, termos longos, quebra de linha no meio de frase,
caracteres não decomponíveis, páginas vazias e sequências repetidas — e as
combina de verdade, várias por caso.

```
laço: 9.357 casos comparados, 9.357 idênticos;
      643 pânicos do legado (D-06) e 0 ignorados
```

Os 643 pânicos são casos em que o legado **morre** por `.unwrap()` numa
expressão regular inválida do filtro `&`. São descartados da comparação de
recortes de propósito — o legado não emite recorte algum ali — e já têm
cobertura própria em `TestPropriedadeFiltroOperador`. **A presença deles é a
evidência de que o gerador está produzindo os casos difíceis**, e não texto
inócuo.

---

## 5. Carga, caos e memória

### 5.1 Correção sob carga

`make load-test`. 64 importações simultâneas do maior documento do corpus, com
detector de corrida: **zero divergência** entre as execuções.

> **A régua está errada, e é preciso dizê-lo.** "O dobro do pico histórico"
> pressupõe conhecer o pico histórico. Ele é a decisão aberta **D-04**, nunca
> respondida. O teste usa 500 documentos/hora como **suposição nomeada**
> (`PicoHistoricoPorHora`), sobrescritível por variável de ambiente. O que o
> resultado sustenta é "não há corrupção sob concorrência alta", não "aguenta o
> dobro do pico".

### 5.2 Caos

`make chaos-test`. Em cada estágio — 1, 2, 3 — o processo recebe `SIGKILL` e o
banco é conferido:

| Cenário | Resultado |
|---|---|
| SIGKILL durante a indexação (status 2) | presa em 2, par recorte/texto íntegro |
| SIGKILL durante o recorte (status 3) | presa em 3, par recorte/texto íntegro |
| SIGKILL logo após a seleção | presa em 1 ou 2 |
| SIGKILL durante a drenagem | presa em estado não terminal |
| Queda do banco durante o processamento | **processo sobrevive** e volta a servir |

O critério é o do legado: a importação fica **no último status gravado**. O que
seria reprovado — e não aconteceu — é aparecer em **5** (concluída sem ter
concluído) ou em **−1** (que diria ao operador que o serviço processou e
falhou, quando ele morreu).

O par `tb_recorte`/`tb_recorte_texto` foi conferido em todos os cenários: a
atomicidade do achado A03 se sustenta sob morte súbita.

**Um desvio do enunciado, declarado.** A fase pede verificar que a queda do
banco leva a importação a **−1**. O teste **não afirma isso**, porque o legado
descarta o erro das gravações de status com `let _ =` (ESPECIFICACAO §3.6): o
−1 vem da falha na **leitura das chaves**, não das gravações. Derrubar as
conexões atinge as duas, e qual falha primeiro depende do instante. Afirmar
"termina em −1" seria afirmar mais do que o teste observa.

### 5.3 Memória — a régua de `OPERACAO.md` não se confirmou

```
documento 18-volume-120-paginas.pdf
  tamanho do PDF         0.1 MiB
  pico vivo              1.3 MiB  (14.3× o PDF)
  alocação total         1.3 MiB  (14.3× o PDF)
```

`docs/OPERACAO.md` §5 usa a regra de bolso de **8× o tamanho do PDF**. O medido
foi **14,3×** — quase o dobro.

**O que isso significa, e o que não significa.** O documento tem ~100 KiB, e
boa parte do que uma importação aloca é custo **fixo** — tabelas do tokenizador,
mapas do índice — que não escala com o PDF. Sobre 100 KiB esse custo domina e
infla o fator; sobre 30 MiB ele desapareceria. Portanto **o fator de 14,3× não
se transfere para um diário real**, e nem por isso a régua de 8× está validada:
ela continua **sem medição em documento de porte real**.

**Encaminhamento.** Manter os 8× em `OPERACAO.md` como **provisório e
declarado**, e remedir assim que **D-11** entregar documentos reais. Um teto de
concorrência derivado da régua errada é como o processo morre por falta de
memória levando junto todas as importações em andamento.

---

## 6. Execução em sombra — o que existe e o que falta

### 6.1 O que foi construído e testado

| Peça | Estado |
|---|---|
| Comparador de dois bancos, por importação | `tools/sombra`, com testes |
| Classificação por severidade | recorte e estado **barram**; tempo **não barra** |
| Nomeação do recorte faltante e do sobrando | testada |
| Distinção `total_recortes` nulo × zero (INV-P20) | testada |
| Tolerância de tempo documentada | 5 min, justificada |
| **Verificação de isolamento** | `VerificarIsolamento`, testada nos dois sentidos |

A verificação de isolamento **tenta escrever** nas três tabelas e exige recusa —
uma restrição que ninguém verifica é uma intenção. O teste cobre os dois lados:
credencial de leitura aprova, credencial de escrita **reprova nomeando as
tabelas**.

### 6.2 O que falta, e por quê

**A duplicação de tráfego não existe.** Ela é infraestrutura — proxy espelho ou
reprodução de um registro de requisições — e depende de a operação permiti-la:
**D-14**, aberta desde a fase F0. Sem ela não há sombra.

**O banco espelho precisa ser uma cópia**, não um banco vazio: o pareamento é
por `id_importacao`. Parear por `(hash, caderno, data)` esbarraria nas
duplicatas que o legado sempre permitiu.

### 6.3 Procedimento, quando D-14 for respondida

1. Congelar a implementação Go. **Nenhum ajuste depois disso sem reiniciar a
   janela** — é restrição da própria fase.
2. Restaurar uma cópia do banco de produção como espelho.
3. Conceder à instância em sombra credencial **somente leitura** no banco real
   e escrita no espelho.
4. Rodar `VerificarIsolamento` contra a credencial de produção **antes de ligar
   a duplicação**. Reprovou, para tudo.
5. Ligar a duplicação de tráfego.
6. Rodar o comparador diariamente; alarme imediato para qualquer divergência de
   severidade `recorte`.
7. Manter por **duas semanas OU 5.000 documentos, o que vier depois**,
   incluindo **um fechamento de mês** — quando o volume e o formato dos diários
   mudam.
8. Só então reavaliar o corte.

---

## 7. O que bloqueia o corte

| # | O que falta | Decisão | Responsável | Critério de aceite |
|---|---|---|---|---|
| 1 | **Corpus de PDFs reais** e dump de perfis | **D-11** | operação | comparador com zero divergência sobre documentos reais |
| 2 | **`Cargo.lock` de produção** | **D-15** | quem mantém o Rust | oráculos recompilados nas versões reais, corpus recapturado, zero divergência |
| 3 | **Espelhamento de tráfego** | **D-14** | infraestrutura | janela completa com zero divergência de recorte |
| 4 | Maior PDF e maior nº de recortes | **D-04** | operação | régua de memória remedida; teto derivado dela |
| 5 | Esquema real das sete tabelas | **D-13** | operação | migração de linha de base deixa de ser inferida |
| 6 | `COLLATE` do banco | **D-02** | operação | ordem do `ORDER BY` confirmada (governa INV-P12) |

### Por que D-15 é mais grave do que parece

Todo o corpus dourado foi capturado com **tantivy 0.22**, **mupdf 0.4** e
**diacritics 0.2** resolvidos neste ambiente — **não** com as versões de
produção. Se a produção usar outra versão do analisador léxico, do extrator ou
da tabela de diacríticos, **o oráculo está medindo outra coisa**, e as três
camadas de texto passam a valer zero.

Isso não é hipótese remota: o limite de 40 bytes de INV-P03 foi **medido** em
0.22.1 e é um detalhe interno do Tantivy, sujeito a mudar entre versões.

**Enquanto D-15 não for respondida, o resultado deste relatório é condicional.**

---

## 8. Verificação — comandos e saída real

```
$ make parity
ok  github.com/gluizcortez/projetorust/test/parity   133.9s
VEREDITO: APROVADO — zero divergência de recortes.
1-extracao 159/159 · 2-normalizacao 159/159 · 3-termos 159/159
4-recortes 38/38   · 5-estados 28/28

$ go test ./test/parity/... -run TestPropriedade -count=1
laço: 9.357 casos comparados, 9.357 idênticos; 643 pânicos do legado (D-06)
ok  github.com/gluizcortez/projetorust/test/parity

$ make load-test
carga: 64 importações em 473ms (135.3/s), 7 recorte(s) cada, zero divergência
ok  github.com/gluizcortez/projetorust/test/carga

$ make chaos-test
--- PASS: TestCaosSIGKILLPorEstagio (9.09s)
--- PASS: TestCaosBancoIndisponivelDuranteOProcessamento (2.59s)
--- PASS: TestCaosSIGKILLDuranteADrenagem
ok  github.com/gluizcortez/projetorust/test/e2e

$ make lint
0 issues.

$ golangci-lint run --build-tags=carga,integration ./...
0 issues.
```

---

## 9. Divergências aceitas

> **Atualizado por uma auditoria posterior à F12**, feita com o Rust restaurado
> do histórico e lido função por função. Ela encontrou **duas divergências que
> nenhuma fase havia registrado**, ambas fora do contrato HTTP e do laço de
> recorte, e nenhuma delas alterando o caminho feliz.

### As duas divergências encontradas na auditoria

| # | Divergência | Alcance | Decisão |
|---|---|---|---|
| **D-22** | O legado NÃO abre transação em `salvar_recorte` (`// TODO` no código). Falha no meio de uma chave deixa lá os recortes já gravados; o porte reverte a chave inteira. | Só o caminho de falha, só a chave que falha | Aberta. Opção B — transação por PAR — reproduz o legado sem reintroduzir a linha órfã de A03. |
| **D-23** | Registros vão para **stderr em JSON**; o legado usava **stdout em texto**. | Coleta de log. `docker logs` e `journald` capturam os dois; `> app.log` fica vazio | Aberta. Uma linha para reverter, se preciso. |

### As três correções deliberadas, inalteradas

| # | Diferença | Justificativa |
|---|---|---|
| A02 | comparação da chave de API em tempo constante | corrige vazamento por tempo; resposta idêntica |
| A03 | par recorte/texto atômico | corrige linha órfã; o legado tem `TODO` explícito |
| A05 | corpo malformado responde 422 em vez de entrar em pânico | o legado fecha a conexão sem resposta |

### As três mudanças de configuração, posteriores à F12

Feitas a pedido, para destravar a execução local. Todas reversíveis por variável
de ambiente.

| Variável | Legado | Porte | Reverter |
|---|---|---|---|
| `SERVIDOR_IP` | `192.168.42.1` | `0.0.0.0` (superconjunto) | `SERVIDOR_IP=192.168.42.1` |
| `API_KEY` | constante no código | padrão com o MESMO valor | `API_KEY=…` |
| `DATABASE_URL` | obrigatória, pânico sem ela | padrão local | `DATABASE_URL=…` |

**Nenhuma divergência de RECORTE conhecida e não corrigida.**

As três diferenças **deliberadas** em relação ao legado — todas de
robustez, nenhuma de conteúdo de banco — permanecem como estavam, documentadas
desde as fases em que entraram:

| # | Diferença | Justificativa |
|---|---|---|
| A02 | comparação da chave de API em tempo constante | corrige vazamento por tempo; resposta idêntica |
| A03 | par recorte/texto atômico | corrige linha órfã; o legado tem `TODO` explícito |
| A05 | corpo malformado responde 422 em vez de entrar em pânico | o legado fecha a conexão sem resposta |

E o campo onde **falta** assinatura: nenhuma divergência de sombra foi
classificada como aceitável, porque **não houve sombra**. O espaço abaixo existe
para quando houver.

| Divergência | Classe | Justificativa | Responsável pelo produto | Data |
|---|---|---|---|---|
| *(nenhuma até aqui)* | | | | |

---

## 10. Conclusão

O que a fase F12 provou:

- o serviço reproduz o legado **exatamente** sobre o corpus dourado, nas cinco
  camadas;
- reproduz sobre **10.000 documentos sintéticos** gerados para as construções
  difíceis;
- **não se corrompe** sob concorrência alta;
- deixa o banco **como o legado deixaria** quando o processo morre;
- e **corrigiu dois defeitos** que alterariam recortes de todos os clientes.

O que a fase F12 **não** provou, e não tinha como:

- que reproduz o legado sobre **documentos reais** — não há nenhum;
- que reproduz o legado **da versão que roda em produção** — não se sabe qual é;
- que reproduz o legado sobre **tráfego real** — não houve sombra.

**A recomendação é NÃO CORTAR**, e ela não depende de mais engenharia. Depende
de **D-11**, **D-15** e **D-14** — três pedidos abertos desde a fase F0, todos
de operação e de quem mantém o serviço em Rust.

Respondidas as três, o ferramental para fechar o portão **já está pronto e
testado**: é rodar.
