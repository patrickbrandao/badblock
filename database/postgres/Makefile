# PostgreSQL do BadBlock — rotinas de schema.
#
#   make migrate               aplica as migrations pendentes de todas as pastas
#   make rollback APP=cgibr    desfaz a última migration de uma pasta
#   make new APP=cgibr NAME=x  cria uma migration vazia na pasta do app
#   make test                  sobe, desce e sobe tudo de novo num PG descartável
#   make psql                  psql no banco badblock

ROOT    := ../..
COMPOSE := docker compose --env-file $(ROOT)/.env

.DEFAULT_GOAL := help
.PHONY: help migrate rollback status new psql test

help: ## Lista os alvos
	@grep -E '^[a-z0-9-]+:.*## ' $(MAKEFILE_LIST) | awk 'BEGIN{FS=":.*## "}{printf "  %-10s %s\n", $$1, $$2}'

migrate: ## Aplica as migrations pendentes (central primeiro, depois cada app)
	$(COMPOSE) run --rm migrate up

rollback: ## Desfaz a última migration da pasta APP
	@test -n "$(APP)" || (echo "use: make rollback APP=cgibr" && exit 1)
	$(COMPOSE) run --rm -e MIGRATE_ONLY=$(APP) migrate rollback

status: ## Estado das migrations de cada pasta
	$(COMPOSE) run --rm migrate status

new: ## Cria uma migration: make new APP=cgibr NAME=descricao
	@test -n "$(APP)" -a -n "$(NAME)" || (echo "use: make new APP=cgibr NAME=descricao" && exit 1)
	@mkdir -p $(APP)
	@f="$(APP)/$$(date -u +%Y%m%d%H%M%S)_$(NAME).sql"; \
	printf -- '-- migrate:up\n\n\n-- migrate:down\n\n' > "$$f"; echo "criado $$f"

psql: ## psql no banco badblock (usuário postgres, dentro do container)
	docker exec -it badblock-postgres psql -U postgres -d badblock

test: ## up → rollback de tudo → up num PG18 descartável
	@./test-migrations.sh
