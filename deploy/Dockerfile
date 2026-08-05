# syntax=docker/dockerfile:1.7
#
# Imagem de distribuição do serviço de recorte.
#
# ------------------------------------------------------------------------
# POR QUE CGO_ENABLED=1 É OBRIGATÓRIO AQUI
# ------------------------------------------------------------------------
# A extração de texto do PDF usa MuPDF, uma biblioteca C. Não existe
# implementação em Go puro que reproduza a varredura blocos → linhas →
# caracteres com a fidelidade que a paridade exige (ver docs/INVARIANTES.md,
# INV-P09 e INV-P11).
#
# Consequências que este arquivo precisa acomodar:
#   - o binário não é estático por padrão, então a imagem final precisa da
#     libc e das bibliotecas compartilhadas que sobrarem da ligação;
#   - a compilação cruzada deixa de ser trivial: o estágio de construção roda
#     na mesma arquitetura da imagem final;
#   - o cache de artefatos C importa mais que o de módulos Go — MuPDF é grande.
#
# A fase F5 decide a forma final de provisionar o MuPDF (pacote da distribuição
# contra compilação a partir do fonte, fixada na versão de produção). Até lá
# este arquivo estabelece a estrutura e é intencionalmente conservador.
# ------------------------------------------------------------------------

# ============================ construção ============================
FROM golang:1.24-bookworm AS construcao

# Dependências de compilação do MuPDF. Fixadas por nome, não por versão: a
# fixação por versão entra em F5, junto com a decisão de origem do MuPDF.
RUN --mount=type=cache,target=/var/cache/apt,sharing=locked \
    --mount=type=cache,target=/var/lib/apt/lists,sharing=locked \
    apt-get update && apt-get install -y --no-install-recommends \
        build-essential \
        pkg-config \
        libmupdf-dev \
        libfreetype6-dev \
        libjbig2dec0-dev \
        libjpeg62-turbo-dev \
        libopenjp2-7-dev \
        libharfbuzz-dev \
        zlib1g-dev

WORKDIR /src

# Camada de dependências separada: muda menos que o código.
COPY go.mod go.sum* ./
RUN --mount=type=cache,target=/root/.cache/go-build \
    --mount=type=cache,target=/go/pkg/mod \
    go mod download

COPY . .

ARG VERSAO=dev
ARG REVISAO=desconhecida

# -trimpath remove caminhos absolutos do binário (reprodutibilidade).
# -s -w descartam tabela de símbolos e informação de depuração (tamanho).
RUN --mount=type=cache,target=/root/.cache/go-build \
    --mount=type=cache,target=/go/pkg/mod \
    CGO_ENABLED=1 go build \
        -trimpath \
        -ldflags "-s -w -X main.versao=${VERSAO} -X main.revisao=${REVISAO}" \
        -o /out/recorte-api \
        ./cmd/recorte-api

# Registra as bibliotecas compartilhadas de que o binário depende. Nesta fase a
# lista deve conter apenas libc e o carregador dinâmico, ambos presentes na
# imagem base — nada a copiar. A saída fica no log de construção como linha de
# base: quando a F5 introduzir a ligação com o MuPDF, a diferença nesta lista é
# exatamente o que a etapa de distribuição passará a precisar.
RUN ldd /out/recorte-api || true

# ============================ distribuição ============================
# distroless: sem shell, sem gerenciador de pacotes, sem utilitários. A imagem
# `base` traz a libc e o carregador dinâmico, necessários porque o binário é
# compilado com cgo.
#
# PENDÊNCIA DA FASE F5 — provisionamento do MuPDF
#
# Assim que a extração de texto entrar, o binário passará a depender das
# bibliotecas do MuPDF e de seus codecs. Duas rotas, a decidir em F5:
#   (a) ligação estática do MuPDF no binário — nada muda aqui;
#   (b) ligação dinâmica — este estágio precisa copiar as bibliotecas do
#       estágio de construção e definir LD_LIBRARY_PATH.
# A rota (a) é preferível justamente por manter esta camada trivial.
#
# Deliberadamente NÃO antecipamos a cópia de bibliotecas: seria maquinário
# especulativo, não exercitado por nenhum código desta fase, e portanto não
# verificável. A linha `ldd` acima é a medição que informará a decisão.
FROM gcr.io/distroless/base-debian12:nonroot AS distribuicao

COPY --from=construcao /out/recorte-api /recorte-api

# A imagem nonroot do distroless já roda como uid 65532. Declarado
# explicitamente para que a intenção não dependa da tag da imagem base.
USER nonroot:nonroot

EXPOSE 6001

# A sonda é o próprio binário: sem shell na imagem, não há curl nem wget.
#
# ATENÇÃO — estado esperado na fase F1: não há manipuladores registrados, então
# /ping responde 404 e o contêiner é marcado como não saudável. A fase F9
# registra a rota e a sonda passa a obter 200. Ver os critérios de aceite da F1.
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD ["/recorte-api", "-healthcheck"]

ENTRYPOINT ["/recorte-api"]
