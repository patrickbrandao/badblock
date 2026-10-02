#!/bin/sh
# Aplica as migrations de todas as pastas de schema, em ordem: primeiro
# central/ (tabela jobs e função compartilhada), depois cada app em ordem
# alfabética. Cada pasta tem a própria tabela de controle do dbmate
# (<pasta>_schema_migrations), então os apps evoluem de forma independente.
#
# Roda dentro da imagem do dbmate (serviço migrate do docker-compose.yml) e
# conecta pela POSTGRES_URL, a mesma dos apps. Argumentos repassados ao dbmate;
# padrão "up". Para uma pasta só: MIGRATE_ONLY=cgibr.
set -eu

: "${POSTGRES_URL:?defina POSTGRES_URL}"

ROOT="${MIGRATIONS_ROOT:-/db}"
CMD="${*:-up}"

dirs="central"
for d in "$ROOT"/*/; do
	name=$(basename "$d")
	[ "$name" = central ] && continue
	[ -n "$(ls "$d"/*.sql 2>/dev/null)" ] || continue
	dirs="$dirs $name"
done

# Rollback desfaz na ordem inversa: os apps antes do central.
case "$CMD" in
rollback* | down*)
	rev=""
	for name in $dirs; do rev="$name $rev"; done
	dirs="$rev"
	;;
esac

for name in $dirs; do
	if [ -n "${MIGRATE_ONLY:-}" ] && [ "$name" != "$MIGRATE_ONLY" ]; then
		continue
	fi
	echo "== $name"
	# shellcheck disable=SC2086
	dbmate --env POSTGRES_URL --no-dump-schema --wait \
		--migrations-dir "$ROOT/$name" \
		--migrations-table "${name}_schema_migrations" \
		$CMD
done
