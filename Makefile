SHELL := /bin/bash
.DEFAULT_GOAL := ci

MODULO      := github.com/gluizcortez/projetorust
BINARIO     := recorte-api
VERSAO      ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
REVISAO     ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo desconhecida)
GO_MINIMO   := 1.24.0
IMAGEM      ?= recorte-api
TAG         ?= $(VERSAO)

# CGO é obrigatório: a extração de texto depende de MuPDF, biblioteca C
# (fase F5). Fixado aqui para que o binário local e o do contêiner tenham o
# mesmo modo de ligação — divergir nisso esconde falhas até o empacotamento.
export CGO_ENABLED := 1

LDFLAGS := -s -w -X main.versao=$(VERSAO) -X main.revisao=$(REVISAO)

.PHONY: ci guarda-toolchain lint test pg-subir pg-descer test-integration oraculos corpus parity build docker generate tidy tidy-check cobertura limpar ajuda

## ci: verificação completa — é o que a integração contínua executa
ci: guarda-toolchain tidy-check lint test build

## lint: análise estática
lint:
	golangci-lint run ./...

## test: testes unitários, com detector de corrida e ordem embaralhada
test:
	go test ./... -race -shuffle=on -coverprofile=coverage.out -covermode=atomic

## pg-subir: sobe um PostgreSQL local para os testes de integração
##   Não usa testcontainers: este ambiente não tem daemon Docker. Ver o
##   cabeçalho de internal/adapter/postgres/integracao_test.go.
PGBIN   := /usr/lib/postgresql/16/bin
PGDATA  := /tmp/pgdata-recorte
PGPORT  := 55432
PGUSER  := pgtest
export TEST_DATABASE_URL ?= postgres://postgres@localhost:$(PGPORT)/postgres?sslmode=disable

pg-subir:
	@if $(PGBIN)/pg_isready -h localhost -p $(PGPORT) >/dev/null 2>&1; then \
		echo "PostgreSQL já responde na porta $(PGPORT)"; exit 0; \
	fi; \
	id -u $(PGUSER) >/dev/null 2>&1 || useradd -m $(PGUSER); \
	rm -rf $(PGDATA); mkdir -p $(PGDATA); chown $(PGUSER) $(PGDATA); \
	su $(PGUSER) -c "$(PGBIN)/initdb -D $(PGDATA) -U postgres --auth=trust --encoding=UTF8 --locale=C" >/dev/null; \
	su $(PGUSER) -c "$(PGBIN)/pg_ctl -D $(PGDATA) -l /tmp/pg-recorte.log -o '-p $(PGPORT)' start"; \
	$(PGBIN)/pg_isready -h localhost -p $(PGPORT)

## pg-descer: encerra o PostgreSQL local
pg-descer:
	@su $(PGUSER) -c "$(PGBIN)/pg_ctl -D $(PGDATA) stop" 2>/dev/null || true

## test-integration: testes que exigem PostgreSQL real
##   Usa TEST_DATABASE_URL. Sem a variável, os testes são pulados com
##   mensagem explicativa em vez de falharem.
test-integration: pg-subir
	go test ./... -race -tags=integration -run 'Integration|Integracao' -v

## oraculos: compila os binários em Rust que os testes de propriedade consultam
##   São OPCIONAIS: sem eles os testes de propriedade são pulados com mensagem
##   explicativa, e os testes contra o corpus dourado seguem rodando.
oraculos:
	cargo build --release --manifest-path tools/capturar-corpus/Cargo.toml \
		--bin capturar-corpus --bin oraculo-normalizacao --bin oraculo-extended

## corpus: regenera o corpus sintético e recaptura o comportamento do legado
corpus:
	python3 tools/gerar-corpus-sintetico/gerar.py test/testdata/corpus
	cargo run --release --manifest-path tools/capturar-corpus/Cargo.toml \
		--bin capturar-corpus -- test/testdata/corpus test/testdata/expected

## parity: comparação contra o corpus dourado capturado do legado
parity:
	@if [ ! -d test/testdata/expected ]; then \
		echo "corpus ausente. Gere com:  make corpus"; \
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

## guarda-toolchain: impede que 'go get' eleve silenciosamente a versão exigida
##   Um salto na diretriz `go` troca o toolchain em tempo de compilação, o que
##   já quebrou a fase F2: a toolchain baixada automaticamente neste ambiente
##   vem sem `covdata` e a cobertura para de funcionar. A diretriz é decisão de
##   projeto, não efeito colateral de atualizar dependência.
guarda-toolchain:
	@declarada=$$(awk '/^go /{print $$2}' go.mod); \
	if [ "$$declarada" != "$(GO_MINIMO)" ]; then \
		echo "go.mod declara 'go $$declarada', esperado '$(GO_MINIMO)'."; \
		echo "Se a elevação for intencional, atualize GO_MINIMO no Makefile,"; \
		echo "deploy/Dockerfile e .github/workflows/ci.yml na mesma mudança."; \
		exit 1; \
	fi
	@if grep -q '^toolchain ' go.mod; then \
		echo "go.mod ganhou uma linha 'toolchain': remova-a para manter a compilação hermética."; \
		exit 1; \
	fi
	@echo "diretriz go: $(GO_MINIMO), sem linha toolchain"

## tidy: normaliza go.mod e go.sum
tidy:
	go mod tidy

## tidy-check: falha se 'go mod tidy' alteraria algo (usado na integração contínua)
##   Compara antes e depois da própria execução, e não contra o git: o alvo
##   detecta esquecimento de tidy, não trabalho em andamento não commitado.
tidy-check:
	@cp go.mod /tmp/go.mod.antes
	@cp go.sum /tmp/go.sum.antes 2>/dev/null || : > /tmp/go.sum.antes
	@go mod tidy
	@if ! diff -q /tmp/go.mod.antes go.mod >/dev/null || \
	    ! diff -q /tmp/go.sum.antes go.sum >/dev/null; then \
		echo "go.mod/go.sum estavam desatualizados: 'go mod tidy' os alterou. Commite as mudanças."; \
		diff -u /tmp/go.mod.antes go.mod || true; \
		exit 1; \
	fi
	@echo "go.mod e go.sum normalizados" 

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
