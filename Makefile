# BadBlock — rotinas da raiz. Cada app tem o próprio Makefile
# (make -C apps/<fonte>/collector, make -C apps/<fonte>/api).
#
#   make up        primeira vez: gera .env, cria as redes e sobe tudo
#   make test      testes unitários de todos os apps
#   make test-int  testes de integração de todos os apps (Docker)

APPS    := $(sort $(wildcard apps/*/collector apps/*/api apps/*/*/collector apps/*/*/api))
WEBSITES := $(sort $(wildcard websites/*))
COMPOSE := docker compose
TRAEFIK_NETWORK ?= $(shell grep -s '^TRAEFIK_NETWORK=' .env | cut -d= -f2 | grep . || echo network_public)

.DEFAULT_GOAL := help
.PHONY: help env network up down ps logs build migrate test test-int lint vet specs-check gitleaks clean-data

help: ## Lista os alvos
	@grep -E '^[a-z0-9-]+:.*## ' $(MAKEFILE_LIST) | awk 'BEGIN{FS=":.*## "}{printf "  %-12s %s\n", $$1, $$2}'

env: ## Gera o .env a partir do .env.example (não sobrescreve); PG_PASSWORD=x define a senha
	@if [ -f .env ]; then echo ".env já existe; nada feito"; exit 0; fi; \
	cp .env.example .env; \
	pw="$(PG_PASSWORD)"; [ -n "$$pw" ] || pw=$$(openssl rand -hex 24); \
	sed -i.bak "s/^POSTGRES_PASSWORD=$$/POSTGRES_PASSWORD=$$pw/" .env; \
	rm -f .env.bak; chmod 600 .env; echo ".env criado"

network: ## Cria as redes Docker externas (badblock e a do Traefik)
	@docker network inspect badblock >/dev/null 2>&1 || docker network create badblock
	@docker network inspect $(TRAEFIK_NETWORK) >/dev/null 2>&1 || docker network create $(TRAEFIK_NETWORK)

up: env network ## Sobe o stack inteiro (compila as imagens locais)
	$(COMPOSE) up -d --build
	@for s in cgibr:8101 afrinic:8102 apnic:8103 arin:8104 lacnic:8105 ripencc:8106 iana:8107 asnames:8108 roothints:8109 rootzone:8110 rootanchors:8111 anatel/pst:8112; do \
		f=$${s%%:*}; p=$$(grep -s "^API_$$(echo $$f | tr a-z/ A-Z_)_HOST_PORT=" .env | cut -d= -f2 | grep . || echo $${s#*:}); \
		printf '  api-%-11s http://127.0.0.1:%s/%s/\n' $$(echo $$f | tr / -) $$p $$f; done
	@for s in www:8201; do \
		f=$${s%%:*}; p=$$(grep -s "^WEBSITE_$$(echo $$f | tr a-z A-Z)_HOST_PORT=" .env | cut -d= -f2 | grep . || echo $${s#*:}); \
		printf '  website-%-7s http://127.0.0.1:%s/\n' $$f $$p; done

down: ## Para o stack (os volumes ficam)
	$(COMPOSE) down

ps: ## Estado dos containers
	$(COMPOSE) ps -a

logs: ## Logs de todos os serviços
	$(COMPOSE) logs -f --tail=100

build: ## Compila as imagens locais
	$(COMPOSE) build

migrate: ## Aplica as migrations pendentes
	$(MAKE) -C database/postgres migrate

test: ## Testes unitários de todos os apps e sites
	@for a in $(APPS) $(WEBSITES); do echo "== $$a"; $(MAKE) -C $$a test || exit 1; done

test-int: ## Testes de integração de todos os apps + migrations (Docker)
	@for a in $(APPS); do echo "== $$a"; $(MAKE) -C $$a test-int || exit 1; done
	$(MAKE) -C database/postgres test

lint: ## golangci-lint de todos os apps; build nos sites
	@for a in $(APPS) $(WEBSITES); do echo "== $$a"; $(MAKE) -C $$a lint || exit 1; done

vet: ## go vet de todos os apps
	@for a in $(APPS); do echo "== $$a"; $(MAKE) -C $$a vet || exit 1; done

specs-check: ## Confere links, estrutura e mapa das specs (specs/README.md)
	@sh specs/check.sh

gitleaks: ## Procura segredos no repositório (Docker)
	docker run --rm -v "$(CURDIR):/repo" zricethezav/gitleaks:v8.30.1 dir /repo --config /repo/.gitleaks.toml --redact

clean-data: ## APAGA os volumes locais (banco; os collectors recarregam tudo)
	$(COMPOSE) down -v
