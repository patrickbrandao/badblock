# collector-rdns — rotinas de build, teste e deploy.
#
#   make test        testes unitários (servidor DNS falso em 127.0.0.1)
#   make test-int    testes de integração (Docker: PG18 e Valkey via testcontainers)
#   make test-real   um prefixo de verdade contra resolvedores de verdade (DNS_SERVERS=... PREFIX=...)
#   make image       imagem Docker local
#   make push        publica a imagem no Docker Hub ($(IMAGE):$(VERSION) e :latest)
#   make up          sobe só este serviço (usa o .env da raiz)

APP        := collector-rdns
MODULE     := github.com/patrickbrandao/badblock/apps/rdns/collector
ROOT       := ../../..
VERSION    ?= $(shell git describe --tags --match '$(APP)/v*' --abbrev=0 2>/dev/null | sed 's|$(APP)/v||' | grep . || echo dev)
COMMIT     ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
BUILD_DATE ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
IMAGE      ?= tmsoftbrasil/badblock-$(APP)
PLATFORMS  ?= linux/amd64,linux/arm64
LDFLAGS    := -s -w -X $(MODULE)/internal/buildinfo.Version=$(VERSION) \
              -X $(MODULE)/internal/buildinfo.Commit=$(COMMIT) \
              -X $(MODULE)/internal/buildinfo.Date=$(BUILD_DATE)
GOLANGCI   ?= golangci/golangci-lint:v2.14.0
COMPOSE    := docker compose --env-file $(ROOT)/.env
BUILD_ARGS := --build-arg VERSION=$(VERSION) --build-arg COMMIT=$(COMMIT) --build-arg BUILD_DATE=$(BUILD_DATE)
# Projeto compose onde as réplicas estão no ar (once/force): badblock pelo
# compose da raiz; badblock-collector-rdns quando esta pasta sobe sozinha.
STACK      ?= badblock

.DEFAULT_GOAL := help
.PHONY: help build test test-int test-real lint fmt vet vendor image push up down logs once force

help: ## Lista os alvos
	@grep -E '^[a-z0-9-]+:.*## ' $(MAKEFILE_LIST) | awk 'BEGIN{FS=":.*## "}{printf "  %-10s %s\n", $$1, $$2}'

build: ## Compila em bin/
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o bin/$(APP) ./cmd/$(APP)

test: ## Testes unitários
	go test -race -count=1 ./...

test-int: ## Testes de integração (precisa de Docker)
	go test -race -count=1 -tags integration ./...

test-real: ## Varre um prefixo de verdade a 64 consultas/s: make test-real DNS_SERVERS=1.1.1.1 PREFIX=200.160.2.0/24
	@test -n "$(DNS_SERVERS)" -a -n "$(PREFIX)" || (echo "use: make test-real DNS_SERVERS=<resolvedor> PREFIX=<IPv4 de /22 a /24>" && exit 1)
	RDNS_REAL_DNS_SERVERS=$(DNS_SERVERS) RDNS_REAL_PREFIX=$(PREFIX) go test -count=1 -tags integration -run RealNet -v ./internal/scan/

lint: ## golangci-lint (local ou via Docker)
	@if command -v golangci-lint >/dev/null 2>&1; then golangci-lint run ./...; \
	else docker run --rm -v "$(CURDIR):/app" -w /app $(GOLANGCI) golangci-lint run ./...; fi

fmt: ## go fmt (fora do vendor/)
	go fmt ./...

vet: ## go vet (inclui os testes de integração)
	go vet -tags integration ./...

vendor: ## Refaz o vendor/ depois de mudar dependência (go mod tidy + go mod vendor)
	go mod tidy
	go mod vendor

image: ## Imagem Docker local ($(IMAGE):$(VERSION))
	docker build $(BUILD_ARGS) -t $(IMAGE):$(VERSION) -t $(IMAGE):latest .

push: ## Publica $(IMAGE):$(VERSION) e :latest, multi-arch (docker login antes)
	@test "$(VERSION)" != dev || (echo "sem tag $(APP)/vX.Y.Z; use VERSION=X.Y.Z" && exit 1)
	docker buildx build --platform $(PLATFORMS) $(BUILD_ARGS) -t $(IMAGE):$(VERSION) -t $(IMAGE):latest --push .

up: ## Sobe só este serviço (usa o .env da raiz; COLLECTOR_RDNS_REPLICAS=4 para quatro instâncias)
	$(COMPOSE) up -d --build

down: ## Para só este serviço
	$(COMPOSE) down

logs: ## Acompanha os logs
	$(COMPOSE) logs -f

once: ## Uma derivação agora, na 1ª réplica no ar (STACK=badblock-collector-rdns se esta pasta subiu sozinha)
	$(COMPOSE) -p $(STACK) exec collector-rdns /$(APP) --once

force: ## Deriva de novo mesmo sem versão nova do cgibr (1ª réplica)
	$(COMPOSE) -p $(STACK) exec collector-rdns /$(APP) --force
