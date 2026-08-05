SHELL := /bin/bash
.DEFAULT_GOAL := ci

MODULO      := github.com/gluizcortez/projetorust
BINARIO     := recorte-api
VERSAO      ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
REVISAO     ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo desconhecida)
IMAGEM      ?= recorte-api
TAG         ?= $(VERSAO)

# CGO é obrigatório: a extração de texto depende de MuPDF, biblioteca C
# (fase F5). Fixado aqui para que o binário local e o do contêiner tenham o
# mesmo modo de ligação — divergir nisso esconde falhas até o empacotamento.
export CGO_ENABLED := 1

LDFLAGS := -s -w -X main.versao=$(VERSAO) -X main.revisao=$(REVISAO)

.PHONY: ci lint test test-integration parity build docker generate tidy cobertura limpar ajuda

## ci: verificação completa — é o que a integração contínua executa
ci: tidy lint test build

## lint: análise estática
lint:
	golangci-lint run ./...

## test: testes unitários, com detector de corrida e ordem embaralhada
test:
	go test ./... -race -shuffle=on -coverprofile=coverage.out -covermode=atomic

## test-integration: testes que exigem PostgreSQL real
test-integration:
	go test ./... -race -tags=integration -run 'Integration|Integracao' -v

## parity: comparação contra o corpus dourado capturado do legado
parity:
	@if [ ! -d test/testdata/expected ]; then \
		echo "corpus ausente. Gere com:"; \
		echo "  python3 tools/gerar-corpus-sintetico/gerar.py test/testdata/corpus"; \
		echo "  cargo run --release --manifest-path tools/capturar-corpus/Cargo.toml -- \\"; \
		echo "      test/testdata/corpus test/testdata/expected"; \
		exit 1; \
	fi
	go test ./test/parity/... -v

## build: compila o binário em bin/
build:
	@mkdir -p bin
	go build -trimpath -ldflags "$(LDFLAGS)" -o bin/$(BINARIO) ./cmd/$(BINARIO)
	@echo "bin/$(BINARIO) $(VERSAO) ($(REVISAO))"

## docker: constrói a imagem de distribuição
docker:
	docker build \
		-f deploy/Dockerfile \
		--build-arg VERSAO=$(VERSAO) \
		--build-arg REVISAO=$(REVISAO) \
		-t $(IMAGEM):$(TAG) .
	@echo "tamanho: $$(docker image inspect $(IMAGEM):$(TAG) --format '{{.Size}}' | numfmt --to=iec)"

## generate: regenera código gerado (tabela de diacríticos em F6, sqlc em F4)
generate:
	go generate ./...

## tidy: normaliza go.mod e go.sum, e falha se houver divergência
tidy:
	go mod tidy
	@arquivos=$$(ls go.mod go.sum 2>/dev/null); \
	git diff --exit-code $$arquivos || { \
		echo "go.mod ou go.sum mudaram: rode 'make tidy' e commite"; exit 1; }

## cobertura: relatório de cobertura por pacote
cobertura: test
	go tool cover -func=coverage.out | tail -30

## limpar: remove artefatos de compilação
limpar:
	rm -rf bin coverage.out
	go clean -cache -testcache

## ajuda: lista os alvos
ajuda:
	@grep -E '^## ' $(MAKEFILE_LIST) | sed 's/^## /  /' | column -t -s ':'
