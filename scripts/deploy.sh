#!/bin/sh
# Deploy do BadBlock num servidor único, via SSH.
#
#   scripts/deploy.sh stack              envia os arquivos do compose e atualiza tudo
#   scripts/deploy.sh service <serviço>  atualiza só um serviço (pull + up)
#
# O servidor roda o stack pelo docker-compose.yml da raiz (include), com as
# imagens do Docker Hub. Os dados do servidor ficam em deploy.env na raiz do
# repositório (fora do git):
#
#   DEPLOY_HOST=api.exemplo.com.br
#   DEPLOY_PORT=22
#   DEPLOY_USER=root
#   DEPLOY_PATH=/srv/badblock
#
# O .env de produção mora só no servidor, em $DEPLOY_PATH/.env: este script
# nunca o envia nem o sobrescreve.
set -eu

ROOT=$(cd "$(dirname "$0")/.." && pwd)
[ -f "$ROOT/deploy.env" ] || { echo "crie $ROOT/deploy.env (veja o cabeçalho deste script)" >&2; exit 1; }
# shellcheck disable=SC1091
. "$ROOT/deploy.env"
: "${DEPLOY_HOST:?defina DEPLOY_HOST em deploy.env}"
: "${DEPLOY_PATH:?defina DEPLOY_PATH em deploy.env}"
DEPLOY_PORT="${DEPLOY_PORT:-22}"
DEPLOY_USER="${DEPLOY_USER:-root}"
TARGET="$DEPLOY_USER@$DEPLOY_HOST"
SSH="ssh -p $DEPLOY_PORT"

remote() {
	$SSH "$TARGET" "cd '$DEPLOY_PATH' && $*"
}

# Só o necessário para o compose rodar com imagens prontas: arquivos do
# compose, bootstrap, migrations, backup e a config do Valkey. Nada de código
# nem de .env.
send_files() {
	echo "enviando arquivos do compose para $TARGET:$DEPLOY_PATH"
	$SSH "$TARGET" "mkdir -p '$DEPLOY_PATH'"
	rsync -az --delete-after -e "$SSH" \
		--include='/docker-compose.yml' \
		--include='/.env.example' \
		--include='/database/' \
		--include='/database/postgresql/' \
		--include='/database/postgresql/docker-compose.yml' \
		--include='/database/postgresql/bootstrap/***' \
		--include='/database/postgresql/migrations/***' \
		--include='/database/postgresql/backup/***' \
		--include='/database/valkey/' \
		--include='/database/valkey/docker-compose.yml' \
		--include='/database/valkey/valkey.conf' \
		--include='/infra/' \
		--include='/infra/traefik/' \
		--include='/infra/traefik/docker-compose.yml' \
		--include='/apps/' \
		--include='/apps/*/' \
		--include='/apps/*/docker-compose.yml' \
		--exclude='*' \
		"$ROOT/" "$TARGET:$DEPLOY_PATH/"
}

check_env() {
	if ! remote "test -f .env"; then
		echo "o servidor ainda não tem $DEPLOY_PATH/.env: copie o .env.example, preencha as senhas," >&2
		echo "deixe COMPOSE_PROFILES vazio e defina API_FQDN e TRAEFIK_CERTRESOLVER=le" >&2
		exit 1
	fi
}

case "${1:-}" in
stack)
	send_files
	check_env
	remote "docker network inspect badblock >/dev/null 2>&1 || docker network create badblock"
	remote "docker compose pull && docker compose up -d --remove-orphans"
	remote "docker compose ps"
	;;
service)
	svc="${2:?informe o serviço (registry-sync, registry-api...)}"
	check_env
	remote "docker compose pull $svc && docker compose up -d $svc"
	remote "docker compose ps $svc"
	;;
*)
	echo "uso: $0 stack | service <serviço>" >&2
	exit 2
	;;
esac
