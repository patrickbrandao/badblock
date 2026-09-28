# Traefik local (desenvolvimento).
#
#   make certs   gera o certificado de *.badblock.localhost em certs/
#   make up      sobe o Traefik (depois de make certs)

.DEFAULT_GOAL := help
CERTS := certs
NAMES := badblock.localhost *.badblock.localhost localhost 127.0.0.1 ::1

.PHONY: help certs up down logs

help: ## Lista os alvos
	@grep -E '^[a-z0-9-]+:.*## ' $(MAKEFILE_LIST) | awk 'BEGIN{FS=":.*## "}{printf "  %-8s %s\n", $$1, $$2}'

certs: ## Gera o certificado local (mkcert se instalado; senão, CA local com openssl)
	@mkdir -p $(CERTS)
	@if command -v mkcert >/dev/null 2>&1; then \
		mkcert -cert-file $(CERTS)/badblock.localhost.pem -key-file $(CERTS)/badblock.localhost-key.pem $(NAMES); \
		echo "Certificado emitido pela CA do mkcert (confiável se você rodou 'mkcert -install')."; \
	else \
		sh ./gen-local-ca.sh $(CERTS); \
	fi

up: ## Sobe o Traefik local
	docker compose --profile dev up -d

down: ## Para o Traefik local
	docker compose --profile dev down

logs: ## Acompanha os logs
	docker compose --profile dev logs -f
