#!/bin/sh
# Testa as migrations num PostgreSQL 18 descartável: up de todas as pastas,
# rollback de tudo (apps antes do central) e up de novo. Falha se qualquer
# migration:up ou migration:down quebrar.
set -eu

cd "$(dirname "$0")"
NAME="badblock-migrations-test-$$"
NET="$NAME"
PW=testpw

cleanup() {
	docker rm -f "$NAME" >/dev/null 2>&1 || true
	docker network rm "$NET" >/dev/null 2>&1 || true
}
trap cleanup EXIT INT TERM

docker network create "$NET" >/dev/null
docker run -d --name "$NAME" --network "$NET" \
	-e POSTGRES_PASSWORD=$PW -e POSTGRES_DB=badblock \
	postgres:18-trixie >/dev/null

echo "aguardando o Postgres..."
i=0
until docker exec "$NAME" pg_isready -h 127.0.0.1 -U postgres -d badblock >/dev/null 2>&1; do
	i=$((i + 1))
	[ $i -lt 60 ] || { docker logs "$NAME"; exit 1; }
	sleep 1
done

# dbmate <pasta ou ""> <comando>: roda o migrate.sh numa pasta ou em todas.
dbmate() {
	only=$1
	shift
	docker run --rm --network "$NET" -v "$PWD:/db:ro" \
		-e POSTGRES_URL="postgres://postgres:$PW@$NAME:5432/badblock?sslmode=disable" \
		-e MIGRATE_ONLY="$only" \
		--entrypoint /bin/sh ghcr.io/amacneil/dbmate:2.36 /db/migrate.sh "$@"
}

tables() {
	docker exec "$NAME" psql -U postgres -d badblock -Atc \
		"SELECT count(*) FROM pg_tables WHERE schemaname = 'public' AND tablename NOT LIKE '%schema_migrations'"
}

echo "== up"
dbmate "" up

echo "== rollback de tudo"
for d in $(ls -d */ | tr -d / | grep -v '^central$' | sort -r) central; do
	n=$(ls "$d"/*.sql 2>/dev/null | wc -l)
	while [ "$n" -gt 0 ]; do
		dbmate "$d" rollback
		n=$((n - 1))
	done
done
left=$(tables)
[ "$left" = 0 ] || { echo "sobraram $left tabelas depois do rollback"; exit 1; }

echo "== up de novo"
dbmate "" up
n=$(tables)
[ "$n" -gt 0 ] || { echo "nenhuma tabela depois do segundo up"; exit 1; }
echo "migrations OK ($n tabelas)"
