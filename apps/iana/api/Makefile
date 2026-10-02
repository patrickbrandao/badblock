# api-iana — rotinas de build, teste e deploy.
#
#   make test        testes unitários
#   make test-int    testes de integração (Docker: PG18 via testcontainers)
#   make test-real   integração com os 10 arquivos reais da IANA, carregados pelo collector-iana
#   make image       imagem Docker local
#   make push        publica a imagem no Docker Hub ($(IMAGE):$(VERSION) e :latest)
#   make up          sobe só este serviço (usa o .env da raiz)

APP        := api-iana
MODULE     := github.com/patrickbrandao/badblock/apps/iana/api
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

# Os 10 arquivos, com o caminho relativo às bases (como no collector-iana).
IANA_BASE  ?= https://www.iana.org/assignments
RDAP_BASE  ?= https://data.iana.org/rdap
IANA_FILES := as-numbers/as-numbers-1.csv as-numbers/as-numbers-2.csv \
              ipv4-address-space/ipv4-address-space.csv \
              ipv6-unicast-address-assignments/ipv6-unicast-address-assignments.csv \
              iana-ipv4-special-registry/iana-ipv4-special-registry-1.csv \
              iana-ipv6-special-registry/iana-ipv6-special-registry-1.csv \
              iana-as-numbers-special-registry/special-purpose-as-numbers.csv
RDAP_FILES := asn.json ipv4.json ipv6.json

test-real: ## Store com o dataset real carregado pelo collector-iana (IANA_REAL_DIR=pasta reaproveita um download)
	@dir="$(IANA_REAL_DIR)"; \
	if [ -z "$$dir" ]; then \
		dir=$$(mktemp -d); trap 'rm -rf "$$dir"' EXIT; \
		for f in $(IANA_FILES); do curl -fsS -o "$$dir/$$(basename $$f)" "$(IANA_BASE)/$$f" || exit 1; done; \
		for f in $(RDAP_FILES); do curl -fsS -o "$$dir/$$f" "$(RDAP_BASE)/$$f" || exit 1; done; \
	fi; \
	IANA_REAL_DIR="$$dir" go test -count=1 -tags integration -run 'Real' -v ./internal/store

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

smoke: ## Consulta a API local (porta do loopback)
	curl -fsS http://127.0.0.1:$${API_IANA_HOST_PORT:-8107}/iana/status && echo && \
	curl -fsS http://127.0.0.1:$${API_IANA_HOST_PORT:-8107}/iana/asn/61613 && echo && \
	curl -fsS http://127.0.0.1:$${API_IANA_HOST_PORT:-8107}/iana/ip/10.0.0.1 && echo && \
	curl -fsS http://127.0.0.1:$${API_IANA_HOST_PORT:-8107}/iana/openapi.yaml >/dev/null

