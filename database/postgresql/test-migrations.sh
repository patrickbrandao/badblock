#!/bin/sh
# Prova que o bootstrap e as migrations funcionam do zero e que cada migration
# tem um "down" que desfaz o que o "up" fez: sobe um PostgreSQL 18
# descartável, aplica tudo, desfaz tudo, confere que não sobrou nada e aplica
# de novo. Usado no CI e à mão (make -C database/postgresql test).
set -eu

DIR=$(cd "$(dirname "$0")" && pwd)
NAME="badblock-migration-test-$$"
PG_IMAGE="postgres:18-trixie"
DBMATE_IMAGE="ghcr.io/amacneil/dbmate:2.36"

cleanup() {
	docker rm -f "$NAME" >/dev/null 2>&1 || true
	docker network rm "$NAME" >/dev/null 2>&1 || true
}
trap cleanup EXIT

docker network create "$NAME" >/dev/null
docker run -d --name "$NAME" --network "$NAME" \
	-e POSTGRES_PASSWORD=pg \
	-e BADBLOCK_OWNER_PASSWORD=owner -e BADBLOCK_SYNC_PASSWORD=sync -e BADBLOCK_API_PASSWORD=api \
	-v "$DIR/bootstrap:/docker-entrypoint-initdb.d:ro" \
	"$PG_IMAGE" >/dev/null

i=0
until docker exec "$NAME" pg_isready -h 127.0.0.1 -U postgres -d badblock >/dev/null 2>&1; do
	i=$((i + 1))
	[ "$i" -gt 90 ] && { docker logs "$NAME"; echo "o Postgres não subiu" >&2; exit 1; }
	sleep 1
done

dbmate() {
	docker run --rm --network "$NAME" -v "$DIR/migrations:/db/migrations:ro" \
		-e DATABASE_URL="postgres://badblock_owner:owner@$NAME:5432/badblock?sslmode=disable" \
		-e DBMATE_MIGRATIONS_DIR=/db/migrations -e DBMATE_NO_DUMP_SCHEMA=true \
		"$DBMATE_IMAGE" --wait "$@"
}

psql_q() {
	docker exec "$NAME" psql -U postgres -d badblock -Atc "$1"
}

echo "== up"
dbmate up
views=$(psql_q "SELECT count(*) FROM information_schema.views WHERE table_schema = 'api'")
[ "$views" -gt 0 ] || { echo "nenhuma view no schema api" >&2; exit 1; }

echo "== rollback de todas as migrations"
# Os nomes das migrations são controlados (AAAAMMDDHHMMSS_nome.sql).
# shellcheck disable=SC2012
n=$(ls "$DIR"/migrations/*.sql | wc -l | tr -d " ")
i=0
while [ "$i" -lt "$n" ]; do
	dbmate rollback
	i=$((i + 1))
done
left=$(psql_q "SELECT count(*) FROM pg_namespace WHERE nspname IN ('ingest', 'registry', 'api')")
[ "$left" = "0" ] || { echo "sobraram $left schemas depois do rollback" >&2; exit 1; }

echo "== up de novo"
dbmate up
echo "== permissões"
psql_q "SET ROLE badblock_api; SELECT count(*) FROM api.prefix" >/dev/null
if psql_q "SET ROLE badblock_api; SELECT count(*) FROM registry.prefix" >/dev/null 2>&1; then
	echo "badblock_api não deveria ler registry.prefix" >&2
	exit 1
fi
psql_q "SET ROLE badblock_sync; INSERT INTO ingest.source_state (source_id) VALUES ('teste'); DELETE FROM ingest.source_state" >/dev/null

echo "OK: $n migrations sobem, descem e sobem de novo; permissões conferidas"
