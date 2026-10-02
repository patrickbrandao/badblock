# api-apnic — rotinas de build, teste e deploy.
#
#   make test        testes unitários
#   make test-int    testes de integração (Docker: PG18 via testcontainers)
#   make test-real   rotas medidas com o arquivo real inteiro (FILE=...)
#   make image       imagem Docker local
#   make push        publica a imagem no Docker Hub ($(IMAGE):$(VERSION) e :latest)
#   make up          sobe só este serviço (usa o .env da raiz)

APP        := api-apnic
SOURCE     := apnic
PORT       := $${API_APNIC_HOST_PORT:-8103}
SMOKE_ASN  ?= 4608
MODULE     := github.com/patrickbrandao/badblock/apps/apnic/api
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

.DEFAULT_GOAL := help
.PHONY: help build test test-int test-real lint fmt vet vendor image push up down logs smoke

help: ## Lista os alvos
	@grep -E '^[a-z0-9-]+:.*## ' $(MAKEFILE_LIST) | awk 'BEGIN{FS=":.*## "}{printf "  %-10s %s\n", $$1, $$2}'

build: ## Compila em bin/
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o bin/$(APP) ./cmd/$(APP)

test: ## Testes unitários
	go test -race -count=1 ./...

test-int: ## Testes de integração (precisa de Docker)
	go test -race -count=1 -tags integration ./...

test-real: ## Carrega o arquivo real com o collector e mede as rotas: make test-real FILE=/caminho/do/arquivo
	@test -n "$(FILE)" || (echo "use: make test-real FILE=/caminho/do/delegated-$(SOURCE)-extended-latest (baixe com curl)" && exit 1)
	APNIC_REAL_FILE=$(abspath $(FILE)) go test -count=1 -tags integration -run RealFile -v ./internal/httpapi/

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

up: ## Sobe só este serviço (usa o .env da raiz)
	$(COMPOSE) up -d --build

down: ## Para só este serviço
	$(COMPOSE) down

logs: ## Acompanha os logs
	$(COMPOSE) logs -f

smoke: ## Consulta a API local (porta do loopback; SMOKE_ASN=4608)
	curl -fsS http://127.0.0.1:$(PORT)/$(SOURCE)/status && echo && \
	curl -fsS http://127.0.0.1:$(PORT)/$(SOURCE)/meta && echo && \
	curl -fsS http://127.0.0.1:$(PORT)/$(SOURCE)/asn/$(SMOKE_ASN) && echo && \
	curl -fsS http://127.0.0.1:$(PORT)/$(SOURCE)/openapi.yaml >/dev/null
