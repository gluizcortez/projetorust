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

.PHONY: ci guarda-toolchain lint test subir descer pg-subir pg-descer test-integration build docker generate tidy tidy-check cobertura limpar ajuda

## ci: verificação completa — é o que a integração contínua executa
ci: guarda-toolchain tidy-check lint test build

## lint: análise estática
lint:
	golangci-lint run ./...

## test: testes unitários, com detector de corrida e ordem embaralhada
test:
	go test ./... -race -shuffle=on -coverprofile=coverage.out -covermode=atomic

## subir: sobe o serviço e o banco com docker compose
##   Nada precisa ser definido antes: docker-compose.yml traz todos os valores,
##   inclusive a API KEY. O serviço fica em http://localhost:6001.
subir:
	docker compose up --build -d
	@echo "serviço em http://localhost:6001 — teste com:  curl localhost:6001/ping"

## descer: derruba o serviço e o banco, preservando os dados
descer:
	docker compose down

## pg-subir: sobe um PostgreSQL local para os testes de integração
##   Não usa testcontainers: este ambiente não tem daemon Docker. Ver o
##   cabeçalho de internal/adapter/postgres/integracao_test.go.
PGBIN   := /usr/lib/postgresql/16/bin
PGDATA  := /tmp/pgdata-recorte
# Diretório do soquete unix. O padrão compilado é /var/run/postgresql, que o
# usuário sem privilégio não consegue escrever neste ambiente — o servidor sobe,
# falha ao criar o arquivo de trava e morre.
PGSOCK  := /tmp/pgsock-recorte
PGPORT  := 55432
PGUSER  := pgtest
export TEST_DATABASE_URL ?= postgres://postgres@localhost:$(PGPORT)/postgres?sslmode=disable

pg-subir:
	@if $(PGBIN)/pg_isready -h localhost -p $(PGPORT) >/dev/null 2>&1; then \
		echo "PostgreSQL já responde na porta $(PGPORT)"; exit 0; \
	fi; \
	id -u $(PGUSER) >/dev/null 2>&1 || useradd -m $(PGUSER); \
	rm -rf $(PGDATA); mkdir -p $(PGDATA) $(PGSOCK); chown $(PGUSER) $(PGDATA) $(PGSOCK); \
	su $(PGUSER) -c "$(PGBIN)/initdb -D $(PGDATA) -U postgres --auth=trust --encoding=UTF8 --locale=C" >/dev/null; \
	su $(PGUSER) -c "$(PGBIN)/pg_ctl -D $(PGDATA) -l /tmp/pg-recorte.log -o '-p $(PGPORT) -k $(PGSOCK)' start"; \
	$(PGBIN)/pg_isready -h localhost -p $(PGPORT)

## pg-descer: encerra o PostgreSQL local
pg-descer:
	@su $(PGUSER) -c "$(PGBIN)/pg_ctl -D $(PGDATA) stop" 2>/dev/null || true

## test-integration: testes que exigem PostgreSQL real
##   Usa TEST_DATABASE_URL. Sem a variável, os testes são pulados com
##   mensagem explicativa em vez de falharem.
# -p 1 serializa os PACOTES. Sem isso, `internal/adapter/postgres` — que faz
# `DROP SCHEMA recorte CASCADE` a cada teste — roda em paralelo com
# `internal/app`, que consulta as mesmas tabelas no MESMO banco. A janela é
# curta e a falha, intermitente: o sintoma é um 500 onde o teste espera 404.
# Um banco por pacote seria a alternativa; serializar custa alguns segundos e
# não exige infraestrutura nova.
test-integration: pg-subir
	go test ./... -race -tags=integration -p 1 -run 'Integration|Integracao' -v

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
