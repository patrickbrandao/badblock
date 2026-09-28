# BadBlock — rotinas da raiz. Cada app tem o próprio Makefile (make -C apps/<app>).
#
#   make up        primeira vez: gera .env, cria redes e certificados, sobe tudo
#   make test      testes unitários de todos os apps
#   make e2e       teste ponta a ponta via Traefik (stack no ar)

APPS    := apps/registry-sync apps/registry-api
COMPOSE := docker compose

.DEFAULT_GOAL := help
.PHONY: help env network certs up down ps logs build sync-once migrate test test-int lint vet e2e e2e-ci \
        gitleaks backup verify-backup deploy clean-data

help: ## Lista os alvos
	@grep -E '^[a-z0-9-]+:.*## ' $(MAKEFILE_LIST) | awk 'BEGIN{FS=":.*## "}{printf "  %-14s %s\n", $$1, $$2}'

env: ## Gera o .env a partir do .env.example com senhas aleatórias (não sobrescreve)
	@if [ -f .env ]; then echo ".env já existe; nada feito"; exit 0; fi; \
	cp .env.example .env; \
	for v in POSTGRES_PASSWORD BADBLOCK_OWNER_PASSWORD BADBLOCK_SYNC_PASSWORD BADBLOCK_API_PASSWORD VALKEY_PASSWORD; do \
		pw=$$(openssl rand -hex 24); sed -i.bak "s/^$$v=$$/$$v=$$pw/" .env; \
	done; rm -f .env.bak; chmod 600 .env; echo ".env criado com senhas aleatórias"

network: ## Cria as redes Docker externas (badblock e traefik)
	@docker network inspect badblock >/dev/null 2>&1 || docker network create badblock
	@docker network inspect traefik >/dev/null 2>&1 || docker network create traefik

certs: ## Gera o certificado local de *.badblock.localhost (se faltar)
	@[ -f infra/traefik/certs/badblock.localhost.pem ] || $(MAKE) -C infra/traefik certs

up: env network certs ## Sobe o stack inteiro (compila as imagens locais)
	$(COMPOSE) up -d --build
	@echo "API: https://api.badblock.localhost  |  Traefik: https://traefik.badblock.localhost"

down: ## Para o stack (os volumes ficam)
	$(COMPOSE) down

ps: ## Estado dos containers
	$(COMPOSE) ps

logs: ## Logs de todos os serviços
	$(COMPOSE) logs -f --tail=100

build: ## Compila as imagens locais
	$(COMPOSE) build

sync-once: ## Roda um ciclo completo do registry-sync agora
	docker exec badblock-registry-sync /registry-sync --once

migrate: ## Aplica as migrations pendentes
	$(MAKE) -C database/postgresql migrate

test: ## Testes unitários de todos os apps
	@for a in $(APPS); do echo "== $$a"; $(MAKE) -C $$a test || exit 1; done

test-int: ## Testes de integração de todos os apps (Docker)
	@for a in $(APPS); do echo "== $$a"; $(MAKE) -C $$a test-int || exit 1; done

lint: ## golangci-lint de todos os apps
	@for a in $(APPS); do echo "== $$a"; $(MAKE) -C $$a lint || exit 1; done

vet: ## go vet de todos os apps
	@for a in $(APPS); do echo "== $$a"; $(MAKE) -C $$a vet || exit 1; done

e2e: ## Teste ponta a ponta contra o stack no ar
	tests/e2e/e2e.sh

e2e-ci: env network certs ## Stack limpo com as fixtures + e2e (APAGA o banco local)
	$(COMPOSE) down -v
	$(COMPOSE) -f docker-compose.yml -f tests/e2e/docker-compose.e2e.yml up -d --build
	tests/e2e/e2e.sh

gitleaks: ## Procura segredos no repositório (Docker)
	docker run --rm -v "$(CURDIR):/repo" zricethezav/gitleaks:v8.30.1 dir /repo --config /repo/.gitleaks.toml --redact

backup: ## Backup do banco agora
	$(MAKE) -C database/postgresql backup

verify-backup: ## Restaura o último backup num banco descartável e confere
	$(MAKE) -C database/postgresql verify

deploy: ## Envia o compose ao servidor e atualiza o stack (deploy.env)
	scripts/deploy.sh stack

clean-data: ## APAGA os volumes locais (banco, backups, cache, arquivos do sync)
	$(COMPOSE) down -v
