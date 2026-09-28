# PostgreSQL do BadBlock — migrations, bootstrap, backup e acesso.
#
# Os alvos usam o container badblock-postgres (compose da raiz ou desta pasta)
# e o .env da raiz.

ROOT    := ../..
COMPOSE := docker compose --env-file $(ROOT)/.env
DBMATE  := $(COMPOSE) run --rm migrate

.DEFAULT_GOAL := help
.PHONY: help up down migrate rollback status new bootstrap psql backup backups verify restore schema test

help: ## Lista os alvos
	@grep -E '^[a-z0-9-]+:.*## ' $(MAKEFILE_LIST) | awk 'BEGIN{FS=":.*## "}{printf "  %-10s %s\n", $$1, $$2}'

up: ## Sobe Postgres, migrate e backup (só esta pasta)
	$(COMPOSE) up -d

down: ## Para esta pasta (o volume de dados fica)
	$(COMPOSE) down

migrate: ## Aplica as migrations pendentes
	$(DBMATE) --wait up

rollback: ## Desfaz a última migration
	$(DBMATE) --wait rollback

status: ## Mostra as migrations aplicadas e pendentes
	$(DBMATE) --wait status

new: ## Cria uma migration: make new NAME=adiciona_tabela_x
	@test -n "$(NAME)" || (echo "use: make new NAME=descricao_curta" && exit 1)
	docker run --rm -u "$$(id -u):$$(id -g)" -v "$(CURDIR)/migrations:/db/migrations" \
		ghcr.io/amacneil/dbmate:2.36 --migrations-dir /db/migrations new $(NAME)

bootstrap: ## Reaplica roles, senhas e grants do banco (idempotente)
	docker exec badblock-postgres /docker-entrypoint-initdb.d/00-bootstrap.sh

psql: ## psql como superusuário no banco badblock
	docker exec -it badblock-postgres psql -U postgres -d badblock

backup: ## Faz um backup agora
	docker exec badblock-backup /bin/sh /backup/backup.sh once

backups: ## Lista os backups guardados
	docker exec badblock-backup sh -c 'ls -lh /backups/daily /backups/weekly'

verify: ## Restaura o backup mais recente num banco descartável e confere
	docker exec -e PGUSER=postgres -e PGPASSWORD="$$(grep -E '^POSTGRES_PASSWORD=' $(ROOT)/.env | cut -d= -f2-)" \
		badblock-backup /bin/sh /backup/verify.sh

restore: ## Restaura um dump: make restore FILE=/backups/daily/badblock-....dump
	@test -n "$(FILE)" || (echo "use: make restore FILE=/backups/daily/badblock-AAAAMMDDTHHMMSSZ.dump" && exit 1)
	docker exec -e PGUSER=postgres -e PGPASSWORD="$$(grep -E '^POSTGRES_PASSWORD=' $(ROOT)/.env | cut -d= -f2-)" \
		badblock-backup /bin/sh /backup/restore.sh $(FILE)

schema: ## Exporta o schema atual para schema.sql (referência, não é migration)
	docker exec badblock-postgres pg_dump -U postgres -d badblock --schema-only --no-owner \
		--schema=ingest --schema=registry --schema=api > schema.sql
	@echo "schema.sql atualizado"

test: ## Testa bootstrap e migrations (up, rollback de todas, up) num PG descartável
	./test-migrations.sh
