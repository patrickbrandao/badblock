# registry-sync — rotinas de build, teste e deploy.
#
#   make test        testes unitários
#   make test-int    testes de integração (Docker: PG18 via testcontainers)
#   make image       imagem Docker local
#   make deploy      atualiza só este serviço no servidor (deploy.env na raiz)

APP        := registry-sync
MODULE     := github.com/patrickbrandao/badblock/apps/$(APP)
ROOT       := ../..
VERSION    ?= $(shell git describe --tags --match '$(APP)/v*' --abbrev=0 2>/dev/null | sed 's|$(APP)/v||' || echo dev)
COMMIT     ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
BUILD_DATE ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
IMAGE      ?= tmsoftbrasil/badblock-$(APP)
LDFLAGS    := -s -w -X $(MODULE)/internal/buildinfo.Version=$(VERSION) \
              -X $(MODULE)/internal/buildinfo.Commit=$(COMMIT) \
              -X $(MODULE)/internal/buildinfo.Date=$(BUILD_DATE)
GOLANGCI   ?= golangci/golangci-lint:v2.14.0
COMPOSE    := docker compose --env-file $(ROOT)/.env

.DEFAULT_GOAL := help
.PHONY: help build test test-int cover lint fmt vet image up down logs once fixtures release deploy

help: ## Lista os alvos
	@grep -E '^[a-z0-9-]+:.*## ' $(MAKEFILE_LIST) | awk 'BEGIN{FS=":.*## "}{printf "  %-10s %s\n", $$1, $$2}'

build: ## Compila em bin/
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o bin/$(APP) ./cmd/$(APP)

test: ## Testes unitários
	go test -race -count=1 ./...

test-int: ## Testes de integração (precisa de Docker)
	go test -race -count=1 -tags integration ./...

cover: ## Cobertura (unitários + integração)
	go test -count=1 -tags integration -coverprofile=coverage.out ./... && go tool cover -func=coverage.out | tail -1

lint: ## golangci-lint (local ou via Docker)
	@if command -v golangci-lint >/dev/null 2>&1; then golangci-lint run ./...; \
	else docker run --rm -v "$(CURDIR):/app" -w /app $(GOLANGCI) golangci-lint run ./...; fi

fmt: ## gofmt
	gofmt -w .

vet: ## go vet (inclui os testes de integração)
	go vet -tags integration ./...

image: ## Imagem Docker local ($(IMAGE):$(VERSION))
	docker build --build-arg VERSION=$(VERSION) --build-arg COMMIT=$(COMMIT) --build-arg BUILD_DATE=$(BUILD_DATE) \
		-t $(IMAGE):$(VERSION) -t $(IMAGE):latest .

up: ## Sobe só este serviço (usa o .env da raiz)
	$(COMPOSE) up -d --build

down: ## Para só este serviço
	$(COMPOSE) down

logs: ## Acompanha os logs
	$(COMPOSE) logs -f

once: ## Roda um ciclo completo agora, dentro do container
	docker exec badblock-$(APP) /$(APP) --once

fixtures: ## Regera testdata/sources a partir das fontes reais em SRC=<dir>
	@test -n "$(SRC)" || (echo "use: make fixtures SRC=/caminho/das/fontes" && exit 1)
	python3 testdata/make-fixtures.py "$(SRC)" testdata/sources

release: ## Cria e envia a tag $(APP)/vX.Y.Z (V=X.Y.Z); o GitHub Actions publica a imagem
	@test -n "$(V)" || (echo "use: make release V=1.2.0" && exit 1)
	git tag -a "$(APP)/v$(V)" -m "$(APP) v$(V)"
	git push origin "$(APP)/v$(V)"

deploy: ## Atualiza este serviço no servidor (deploy.env na raiz)
	@$(ROOT)/scripts/deploy.sh service $(APP)
