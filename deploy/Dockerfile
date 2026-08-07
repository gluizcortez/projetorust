# syntax=docker/dockerfile:1.7
#
# Imagem de distribuição do serviço de recorte.
#
# ------------------------------------------------------------------------
# POR QUE CGO_ENABLED=1 É OBRIGATÓRIO AQUI  (resolvido na fase F5)
# ------------------------------------------------------------------------
# A extração de texto usa MuPDF, biblioteca C, via github.com/gen2brain/go-fitz.
# Não existe implementação em Go puro que reproduza a varredura
# blocos → linhas → caracteres com a fidelidade que a paridade exige
# (docs/INVARIANTES.md, INV-P09 e INV-P11).
#
# O go-fitz EMBARCA o MuPDF como biblioteca estática (libmupdf_linux_amd64.a e
# libmupdfthird_linux_amd64.a, ~22 MB). Duas consequências, ambas medidas na
# fase F5:
#
#   1. NÃO é preciso instalar libmupdf-dev nem os codecs. O estágio de
#      construção precisa apenas de um compilador C, que a imagem golang já
#      traz.
#   2. O binário resultante depende SOMENTE de libc em tempo de execução:
#
#          $ ldd recorte-api
#          linux-vdso.so.1
#          libc.so.6 => /lib/x86_64-linux-gnu/libc.so.6
#          /lib64/ld-linux-x86-64.so.2
#
#      Nada a copiar para o estágio de distribuição — a rota (a) prevista na
#      fase F1 se confirmou.
#
# ARMADILHA — NÃO troque para CGO_ENABLED=0.
# O binário COMPILA, e entra em pânico no arranque:
#
#     panic: cannot load library: libmupdf.so: cannot open shared object file
#
# Sem cgo, o go-fitz recorre ao purego e passa a exigir um libmupdf.so
# COMPARTILHADO em tempo de execução, que a imagem não tem. A falha não
# aparece na compilação, só na primeira extração.
# ------------------------------------------------------------------------

# ============================ construção ============================
FROM golang:1.24-bookworm AS construcao

# A imagem golang já traz gcc e libc-dev, que é tudo de que a ligação estática
# do MuPDF embarcado no go-fitz precisa. Nenhum pacote de MuPDF é instalado —
# ver o cabeçalho.

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
        ./src/main

# Confere que o binário não ganhou dependências compartilhadas além de libc e
# do carregador dinâmico — as duas que a imagem de distribuição fornece.
# Qualquer outra entrada aqui quebraria a imagem final em tempo de execução, e
# é melhor descobrir na construção.
RUN ldd /out/recorte-api && \
    ! ldd /out/recorte-api | grep -vE 'linux-vdso|libc\.so|ld-linux|statically linked' | grep -q '=>'


# ============================ distribuição ============================
# distroless: sem shell, sem gerenciador de pacotes, sem utilitários. A imagem
# `base` traz a libc e o carregador dinâmico, necessários porque o binário é
# compilado com cgo.
#
# A fase F5 confirmou a rota (a): o MuPDF é ligado estaticamente ao binário e
# não há biblioteca alguma a copiar para cá.
FROM gcr.io/distroless/base-debian12:nonroot AS distribuicao

COPY --from=construcao /out/recorte-api /recorte-api

# A imagem nonroot do distroless já roda como uid 65532. Declarado
# explicitamente para que a intenção não dependa da tag da imagem base.
USER nonroot:nonroot

EXPOSE 6001

# A sonda é o próprio binário: sem shell na imagem, não há curl nem wget.
#
# A rota /ping foi registrada na fase F9 e responde 200 sem autenticação e sem
# tocar no banco — como o legado (reference/main.rs:112-115), que responde
# `pong` mesmo com o PostgreSQL indisponível. A sonda portanto verifica que o
# PROCESSO está de pé, não que o serviço está funcional; um endpoint de saúde
# que consulte dependências é evolução da fase F11, atrás de HEALTH_ENDPOINTS.
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD ["/recorte-api", "-healthcheck"]

ENTRYPOINT ["/recorte-api"]
