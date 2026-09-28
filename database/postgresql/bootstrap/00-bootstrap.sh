#!/bin/sh
# Bootstrap do BadBlock: cria (ou atualiza) os roles, o banco e as extensões.
#
# A imagem oficial do postgres executa este script sozinha na primeira
# inicialização do volume (docker-entrypoint-initdb.d). Ele é idempotente, então
# também serve para rodar de novo contra um servidor já no ar, por exemplo para
# trocar as senhas depois de editar o .env:
#
#   make -C database/postgresql bootstrap
#
# As senhas chegam por variável de ambiente e nunca ficam gravadas em arquivo.
set -eu

: "${BADBLOCK_OWNER_PASSWORD:?defina BADBLOCK_OWNER_PASSWORD}"
: "${BADBLOCK_SYNC_PASSWORD:?defina BADBLOCK_SYNC_PASSWORD}"
: "${BADBLOCK_API_PASSWORD:?defina BADBLOCK_API_PASSWORD}"

BOOTSTRAP_SQL="${BOOTSTRAP_SQL:-/docker-entrypoint-initdb.d/bootstrap.psql}"

psql --no-psqlrc -v ON_ERROR_STOP=1 \
	--username "${POSTGRES_USER:-postgres}" \
	--dbname "${POSTGRES_DB:-postgres}" \
	-v database="${BADBLOCK_DB:-badblock}" \
	-v owner_password="$BADBLOCK_OWNER_PASSWORD" \
	-v sync_password="$BADBLOCK_SYNC_PASSWORD" \
	-v api_password="$BADBLOCK_API_PASSWORD" \
	-f "$BOOTSTRAP_SQL"
