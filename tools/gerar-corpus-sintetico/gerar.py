#!/usr/bin/env python3
"""Gera o corpus sintético de paridade — fase F0.

O corpus REAL de diários oficiais anonimizados é uma dependência da operação
(ver docs/DECISOES-ABERTAS.md, D-11) e não pode ser fabricado. Este gerador
produz documentos que exercitam, um a um, os comportamentos catalogados em
docs/INVARIANTES.md — o suficiente para destravar as fases F5 a F8 enquanto o
corpus real não chega.

Cada documento é nomeado pela invariante que exercita, para que uma falha de
paridade aponte direto para a regra violada.

    python3 gerar.py [dir-saida]        # padrão: test/testdata/corpus

Requisitos: reportlab. Fonte: DejaVuSans (cobre ø, đ, ß, æ, ł, ħ).
"""

import json
import os
import sys
import zlib

from reportlab import rl_config

# Determinismo: suprime data de criação e identificador aleatório do documento.
# Precisa ser definido ANTES de importar/instanciar o canvas.
rl_config.invariant = 1

from reportlab.lib.pagesizes import A4  # noqa: E402
from reportlab.pdfbase import pdfmetrics  # noqa: E402
from reportlab.pdfbase.ttfonts import TTFont  # noqa: E402
from reportlab.pdfgen import canvas  # noqa: E402

FONTE_TTF = "/usr/share/fonts/truetype/dejavu/DejaVuSans.ttf"
FONTE = "DejaVu"
TAMANHO = 11
ENTRELINHA = 16
MARGEM_ESQ = 56
MARGEM_TOPO = 60
COLUNA_DIR = 310

LARGURA, ALTURA = A4


# --------------------------------------------------------------------------
# Especificação declarativa do corpus
#
# Cada documento é uma lista de páginas. Uma página é uma lista de linhas
# (coluna única) ou um dicionário {"esq": [...], "dir": [...]} (duas colunas).
# --------------------------------------------------------------------------

CABECALHO = "DIARIO OFICIAL DO MUNICIPIO - EDICAO SINTETICA"

DOCUMENTOS = {
    # ---- baseline -------------------------------------------------------
    "01-simples": {
        "descricao": "Documento de referência, texto corrido, sem armadilhas.",
        "paginas": [
            [
                CABECALHO,
                "",
                "PORTARIA No 1.024, DE 15 DE MARCO DE 2024",
                "",
                "O SECRETARIO MUNICIPAL DE ADMINISTRACAO, no uso de suas",
                "atribuicoes legais, resolve nomear os servidores abaixo.",
                "",
                "JOAO SILVA - matricula 44521",
                "MARIA SOUZA - matricula 44522",
            ],
            [
                CABECALHO,
                "",
                "EXTRATO DE CONTRATO",
                "",
                "Contratada: ALFA CONSTRUCOES LTDA",
                "Objeto: reforma predial da unidade central.",
            ],
        ],
    },
    # ---- INV-P09: segmentação de blocos e linhas -------------------------
    "02-inv-p09-duas-colunas": {
        "descricao": "Duas colunas — exercita a segmentação em blocos (INV-P09).",
        "paginas": [
            {
                "esq": [
                    "COLUNA ESQUERDA",
                    "PORTARIA No 2.001",
                    "Nomeia JOAO SILVA para o",
                    "cargo de assessor tecnico.",
                ],
                "dir": [
                    "COLUNA DIREITA",
                    "PORTARIA No 2.002",
                    "Exonera MARIA SOUZA do",
                    "cargo de coordenadora.",
                ],
            }
        ],
    },
    # ---- INV-P02: junção de hífens --------------------------------------
    "03-inv-p02-hifen-ascii": {
        "descricao": "Hífen no fim da linha após caractere ASCII (INV-P02).",
        "paginas": [
            [
                CABECALHO,
                "",
                "Fica determinada a conti-",
                "nuacao do processo administrativo",
                "referente ao servidor JOAO SILVA.",
            ]
        ],
    },
    "04-inv-p02-hifen-acentuado": {
        "descricao": (
            "Hífen no fim da linha após caractere ACENTUADO. Um porte com \\w "
            "ASCII NÃO junta estas palavras (INV-P02)."
        ),
        "paginas": [
            [
                CABECALHO,
                "",
                "Determina a instituiçã-",
                "o do comite permanente.",
                "",
                "Autoriza a açã-",
                "o judicial cabivel.",
                "",
                "Refere-se a corrupçã-",
                "o passiva no ambito municipal.",
            ]
        ],
    },
    "05-inv-p02-hifen-negativo": {
        "descricao": "Hífens que NÃO devem provocar junção (INV-P02, caso negativo).",
        "paginas": [
            [
                CABECALHO,
                "",
                "Linha terminada em hifen isolado",
                "-",
                "continuacao apos hifen isolado",
                "",
                "Linha terminada em espaco e hifen -",
                "outra continuacao",
            ]
        ],
    },
    # ---- INV-P07: diacríticos -------------------------------------------
    "06-inv-p07-acentos-portugues": {
        "descricao": "Acentuação do português, coberta pela decomposição canônica (INV-P07).",
        "paginas": [
            [
                CABECALHO,
                "",
                "JOSÉ DA CONCEIÇÃO",
                "MARIA DOS ANJOS AÇÃO",
                "ANTÔNIO CÂNDIDO MÜLLER",
                "JOÃO NÚÑEZ",
            ]
        ],
    },
    "07-inv-p07-nao-decomponiveis": {
        "descricao": (
            "Caracteres que a decomposição canônica NÃO resolve: ø đ ß æ ł ħ "
            "(INV-P07). É aqui que uma implementação só com NFD diverge."
        ),
        "paginas": [
            [
                CABECALHO,
                "",
                "SØREN KIERKEGAARD - consultor externo",
                "ĐORĐE PETROVIĆ - fornecedor",
                "STRAßE HANDELS GMBH - contratada",
                "ÆTHER SYSTEMS LTDA - contratada",
                "ŁUKASZ NOWAK - perito",
                "ĦAMRUN SERVICES - contratada",
            ]
        ],
    },
    # ---- INV-P01: operador & --------------------------------------------
    "08-inv-p01-e-comercial": {
        "descricao": (
            "Três grafias do operador & na mesma página. Um porte sem emulação "
            "do modo extended perde as grafias sem espaço (INV-P01)."
        ),
        "paginas": [
            [
                CABECALHO,
                "",
                "Contratada: ACME & FILHOS LTDA",
            ],
            [
                CABECALHO,
                "",
                "Contratada: ACME&FILHOS LTDA",
            ],
            [
                CABECALHO,
                "",
                "Contratada: ACME   &   FILHOS LTDA",
            ],
            [
                CABECALHO,
                "",
                "Contratada: ACME FILHOS LTDA (sem o operador - nao deve casar)",
            ],
        ],
    },
    # ---- INV-P05: frase contra subcadeia ---------------------------------
    "09-inv-p05-frase-separadores": {
        "descricao": (
            "Variações de separador entre os termos da frase. Todas devem casar "
            "(INV-P05, casos positivos)."
        ),
        "paginas": [
            [CABECALHO, "", "Nomeia JOAO SILVA para o cargo."],
            [CABECALHO, "", "Nomeia JOAO   SILVA para o cargo."],
            [CABECALHO, "", "Nomeia JOAO,  SILVA para o cargo."],
            [CABECALHO, "", "Nomeia JOAO - SILVA para o cargo."],
        ],
    },
    "10-inv-p05-frase-quebra-de-linha": {
        "descricao": "Frase atravessando quebra de linha — deve casar (INV-P05).",
        "paginas": [
            [
                CABECALHO,
                "",
                "Fica nomeado o servidor JOAO",
                "SILVA para o cargo em comissao.",
            ]
        ],
    },
    "11-inv-p05-negativos": {
        "descricao": (
            "Casos que NÃO podem casar com a frase 'JOAO SILVA' (INV-P05, "
            "casos negativos). Um porte com strings.Contains casa o primeiro."
        ),
        "paginas": [
            [CABECALHO, "", "Registro de JOAOSILVA sem separador."],
            [CABECALHO, "", "Registro de SILVA JOAO em ordem invertida."],
            [CABECALHO, "", "Registro de JOAO DA SILVA com termo no meio."],
            [CABECALHO, "", "Registro de JOAO SILVEIRA com sufixo distinto."],
        ],
    },
    # ---- INV-P03 / INV-P04: analisador léxico ----------------------------
    "12-inv-p03-termos-longos": {
        "descricao": (
            "Termos de 30, 39, 40, 45 e 80 caracteres sem separador. Exercita o "
            "descarte por comprimento do analisador (INV-P03, INV-P04)."
        ),
        "paginas": [
            [
                CABECALHO,
                "",
                "Codigo de 30: " + "A" * 30,
                "Codigo de 39: " + "B" * 39,
                "Codigo de 40: " + "C" * 40,
                "Codigo de 45: " + "D" * 45,
                "Codigo de 80: " + "E" * 80,
                "",
                "Acentuado de 20 pares (40 runas, 80 bytes):",
                "çã" * 20,
                "Acentuado de 19 pares (38 runas, 76 bytes):",
                "çã" * 19,
            ]
        ],
    },
    "12b-inv-p04-bytes-contra-runas": {
        "descricao": (
            "DISCRIMINADOR de INV-P04. Sequências de 'α' (2 bytes por runa) que "
            "SOBREVIVEM à remoção de diacríticos. Se o limite for medido em "
            "bytes, só 'α'x19 (38 bytes) permanece no índice; se for em runas, "
            "'α'x19, x20 e x39 permanecem e só x40 é descartado. Nenhum outro "
            "documento do corpus separa as duas hipóteses, porque a remoção de "
            "diacríticos deixa o resto do texto em ASCII."
        ),
        "paginas": [
            [
                CABECALHO,
                "",
                "grego 19 runas / 38 bytes: " + "α" * 19,
                "grego 20 runas / 40 bytes: " + "α" * 20,
                "grego 39 runas / 78 bytes: " + "α" * 39,
                "grego 40 runas / 80 bytes: " + "α" * 40,
            ]
        ],
    },
    # ---- INV-P10: caractere irrecuperável --------------------------------
    "13-inv-p10-espacos-especiais": {
        "descricao": (
            "Espaço inquebrável (U+00A0), espaço ideográfico (U+3000) e espaço "
            "de largura zero (U+200B). O aparo do Rust remove os dois primeiros "
            "e não o terceiro (ESPECIFICACAO §4.2, INV-P10)."
        ),
        "paginas": [
            [
                CABECALHO,
                "",
                " JOAO SILVA com NBSP nas bordas ",
                "JOAO SILVA com NBSP no meio",
                "JOAO​SILVA com largura zero no meio",
            ]
        ],
    },
    # ---- INV-P12: deduplicação por perfil --------------------------------
    "14-inv-p12-duas-expressoes-mesma-pagina": {
        "descricao": (
            "ALFA e BETA na MESMA página. Perfil 7 tem as duas expressões e deve "
            "gerar UM recorte, atribuído a ALFA. Perfil 8 tem apenas BETA e "
            "gera o seu próprio recorte (INV-P12)."
        ),
        "paginas": [
            [CABECALHO, "", "Pagina sem ocorrencias relevantes."],
            [CABECALHO, "", "Pagina sem ocorrencias relevantes."],
            [
                CABECALHO,
                "",
                "EXTRATO DE CONTRATOS",
                "",
                "Contratada: ALFA CONSTRUCOES LTDA",
                "Contratada: BETA CONSTRUCOES LTDA",
            ],
        ],
    },
    "15-inv-p12-expressoes-paginas-distintas": {
        "descricao": "ALFA e BETA em páginas distintas — dois recortes no perfil 7 (INV-P12).",
        "paginas": [
            [CABECALHO, "", "Contratada: ALFA CONSTRUCOES LTDA"],
            [CABECALHO, "", "Contratada: BETA CONSTRUCOES LTDA"],
        ],
    },
    "16-ocorrencias-repetidas-na-pagina": {
        "descricao": (
            "A mesma expressão três vezes na mesma página deve gerar UM recorte "
            "(um documento por página no índice)."
        ),
        "paginas": [
            [
                CABECALHO,
                "",
                "JOAO SILVA consta na linha um.",
                "JOAO SILVA consta na linha dois.",
                "JOAO SILVA consta na linha tres.",
            ]
        ],
    },
    # ---- INV-P19: expressão acentuada nunca casa -------------------------
    "17-inv-p19-expressao-acentuada": {
        "descricao": (
            "O índice tem os acentos removidos, a expressão cadastrada NÃO. "
            "A expressão 'CONCEIÇÃO' jamais casa; 'CONCEICAO' casa (INV-P19)."
        ),
        "paginas": [
            [
                CABECALHO,
                "",
                "MARIA DA CONCEIÇÃO SOUZA - matricula 90001",
            ]
        ],
    },
    # ---- volume ----------------------------------------------------------
    "18-volume-120-paginas": {
        "descricao": "Documento longo — desempenho, memória e numeração de páginas.",
        "paginas": [
            [
                CABECALHO,
                "",
                f"PAGINA {n:03d} DE 120",
                "",
                "Texto de preenchimento do diario oficial sintetico.",
                (
                    "Contratada: ALFA CONSTRUCOES LTDA"
                    if n % 17 == 0
                    else "Sem ocorrencia relevante nesta pagina."
                ),
            ]
            for n in range(1, 121)
        ],
    },
    "19-pagina-vazia": {
        "descricao": "Documento com página sem texto algum — não pode gerar erro.",
        "paginas": [
            [CABECALHO, "", "Pagina com conteudo."],
            [],
            [CABECALHO, "", "Contratada: ALFA CONSTRUCOES LTDA"],
        ],
    },
}

# Documentos inválidos, gerados sem reportlab.
INVALIDOS = {
    "20-corrompido": "truncado",
    "21-vazio": "vazio",
    "22-nao-e-pdf": "texto",
}


# --------------------------------------------------------------------------
# Chaves de pesquisa
#
# Reproduz a saída de `obter_chaves_pesquisa` (reference/main.rs:544-571).
# A ORDEM é normativa: `ORDER BY tp.id_perfil, tpv.expressao_nm`.
#
# ATENÇÃO: a ordenação aqui usa pontos de código Unicode. O banco real usa seu
# COLLATE, que pode ordenar diferente para expressões que divergem por acento
# ou caixa. Ver docs/DECISOES-ABERTAS.md, D-02.
# --------------------------------------------------------------------------

CHAVES = [
    # perfil 7 — duas expressões, exercita INV-P12
    (7, "ALFA CONSTRUCOES"),
    (7, "BETA CONSTRUCOES"),
    # perfil 8 — mesma expressão do perfil 7, exercita o isolamento entre perfis
    (8, "BETA CONSTRUCOES"),
    # perfil 10 — operador &, INV-P01
    (10, "ACME & FILHOS"),
    # perfil 11 — frase de dois termos, INV-P05
    (11, "JOAO SILVA"),
    # perfil 12 — nomes não decomponíveis, INV-P07
    (12, "SOREN KIERKEGAARD"),
    (12, "STRASSE HANDELS"),
    # perfil 13 — expressão acentuada contra índice sem acento, INV-P19
    (13, "CONCEICAO SOUZA"),
    (13, "CONCEIÇÃO SOUZA"),
    # perfil 14 — expressão degenerada, INV-P06
    (14, "---"),
    # perfil 15 — junção de hífens, INV-P02
    (15, "ACAO JUDICIAL"),
    (15, "CONTINUACAO DO PROCESSO"),
    (15, "INSTITUICAO DO COMITE"),
    # perfil 16 — termos longos, INV-P03
    (16, "C" * 40),
    (16, "B" * 39),
]


def chaves_ordenadas():
    """Ordena por (id_perfil, expressao_nm) — a mesma cláusula do SQL."""
    return [
        {"id_perfil": p, "expressao_nm": e}
        for p, e in sorted(CHAVES, key=lambda kv: (kv[0], kv[1]))
    ]


# --------------------------------------------------------------------------
# Renderização
# --------------------------------------------------------------------------


def registrar_fonte():
    if not os.path.exists(FONTE_TTF):
        sys.exit(
            f"fonte não encontrada: {FONTE_TTF}\n"
            "instale fonts-dejavu-core ou ajuste FONTE_TTF"
        )
    pdfmetrics.registerFont(TTFont(FONTE, FONTE_TTF))


def desenhar_coluna(c, linhas, x, y):
    for linha in linhas:
        if linha:
            c.drawString(x, y, linha)
        y -= ENTRELINHA
    return y


def gerar_pdf(caminho, paginas):
    c = canvas.Canvas(caminho, pagesize=A4, invariant=1)
    c.setCreator("gerar-corpus-sintetico")
    c.setProducer("gerar-corpus-sintetico")
    c.setAuthor("corpus sintetico de paridade")
    c.setTitle(os.path.basename(caminho))

    for pagina in paginas:
        c.setFont(FONTE, TAMANHO)
        if isinstance(pagina, dict):
            desenhar_coluna(c, pagina.get("esq", []), MARGEM_ESQ, ALTURA - MARGEM_TOPO)
            desenhar_coluna(c, pagina.get("dir", []), COLUNA_DIR, ALTURA - MARGEM_TOPO)
        else:
            desenhar_coluna(c, pagina, MARGEM_ESQ, ALTURA - MARGEM_TOPO)
        c.showPage()

    c.save()


def gerar_invalidos(dir_saida):
    """Documentos que exercitam os caminhos de erro."""
    # Truncado: cabeçalho de PDF válido, corpo cortado no meio.
    valido = os.path.join(dir_saida, "01-simples.pdf")
    with open(valido, "rb") as f:
        bruto = f.read()
    with open(os.path.join(dir_saida, "20-corrompido.pdf"), "wb") as f:
        f.write(bruto[: len(bruto) // 3])

    # Vazio: zero bytes.
    open(os.path.join(dir_saida, "21-vazio.pdf"), "wb").close()

    # Não é PDF: texto puro com extensão .pdf.
    with open(os.path.join(dir_saida, "22-nao-e-pdf.pdf"), "wb") as f:
        f.write(b"Este arquivo nao e um PDF. Nao possui a assinatura %PDF-.\n")


def main():
    dir_saida = sys.argv[1] if len(sys.argv) > 1 else "test/testdata/corpus"
    os.makedirs(dir_saida, exist_ok=True)

    registrar_fonte()

    manifesto = []
    for nome, spec in DOCUMENTOS.items():
        caminho = os.path.join(dir_saida, f"{nome}.pdf")
        gerar_pdf(caminho, spec["paginas"])
        with open(caminho, "rb") as f:
            bruto = f.read()
        manifesto.append(
            {
                "documento": nome,
                "descricao": spec["descricao"],
                "total_paginas": len(spec["paginas"]),
                "bytes": len(bruto),
                "crc32": format(zlib.crc32(bruto) & 0xFFFFFFFF, "08x"),
            }
        )
        print(f"  {nome}.pdf  ({len(spec['paginas'])} pág., {len(bruto)} bytes)")

    gerar_invalidos(dir_saida)
    for nome, tipo in INVALIDOS.items():
        caminho = os.path.join(dir_saida, f"{nome}.pdf")
        with open(caminho, "rb") as f:
            bruto = f.read()
        manifesto.append(
            {
                "documento": nome,
                "descricao": f"Documento inválido ({tipo}) — exercita o caminho de erro.",
                "total_paginas": 0,
                "bytes": len(bruto),
                "crc32": format(zlib.crc32(bruto) & 0xFFFFFFFF, "08x"),
            }
        )
        print(f"  {nome}.pdf  (inválido: {tipo}, {len(bruto)} bytes)")

    with open(os.path.join(dir_saida, "chaves.json"), "w", encoding="utf-8") as f:
        json.dump(chaves_ordenadas(), f, ensure_ascii=False, indent=2)
        f.write("\n")

    with open(os.path.join(dir_saida, "MANIFESTO.json"), "w", encoding="utf-8") as f:
        json.dump(manifesto, f, ensure_ascii=False, indent=2)
        f.write("\n")

    print(f"\n{len(manifesto)} documento(s) e {len(CHAVES)} chave(s) em {dir_saida}")


if __name__ == "__main__":
    main()
