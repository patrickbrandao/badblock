#!/bin/sh
# Prova que o último backup restaura: cria um banco descartável, restaura o dump
# mais recente nele, compara a contagem das tabelas centrais com o banco em
# produção e apaga o banco descartável.
#
# Precisa de um role com CREATEDB (o superusuário postgres), via variáveis PG*.
set -eu

BACKUP_DIR="${BACKUP_DIR:-/backups}"
SOURCE_DB="${PGDATABASE:-badblock}"
CHECK_DB="badblock_restore_check"

# Os nomes são gerados pelo backup.sh (badblock-AAAAMMDDTHHMMSSZ.dump).
# shellcheck disable=SC2012
latest=$(ls -1 "$BACKUP_DIR"/daily/badblock-*.dump 2>/dev/null | sort -r | head -n 1 || true)
[ -n "$latest" ] || { echo "nenhum backup em $BACKUP_DIR/daily" >&2; exit 1; }

counts() {
	psql --no-psqlrc -At -d "$1" -c "
		SELECT (SELECT count(*) FROM registry.asn)    || ' ' ||
		       (SELECT count(*) FROM registry.prefix) || ' ' ||
		       (SELECT count(*) FROM registry.holder) || ' ' ||
		       (SELECT count(*) FROM registry.change_log) || ' ' ||
		       (SELECT coalesce(max(id), 0) FROM registry.dataset)"
}

cleanup() {
	dropdb --if-exists "$CHECK_DB" >/dev/null 2>&1 || true
}
trap cleanup EXIT

echo "verificando $latest"
cleanup
createdb --owner=badblock_owner "$CHECK_DB"
# Extensões exigem superusuário e já são criadas pelo bootstrap: ficam fora.
list=$(mktemp)
pg_restore --list "$latest" | grep -vE ' EXTENSION - | COMMENT - EXTENSION ' >"$list"
pg_restore --no-owner --role=badblock_owner --exit-on-error --use-list="$list" --dbname="$CHECK_DB" "$latest"
rm -f "$list"

restored=$(counts "$CHECK_DB")
echo "restaurado (asn prefix holder change_log dataset): $restored"

# O banco vivo pode ter avançado depois do dump; a verificação exige só que o
# restaurado tenha dados e uma versão de dataset válida.
# Separação proposital: as cinco contagens viram $1..$5.
# shellcheck disable=SC2086
set -- $restored
if [ "$1" -gt 0 ] && [ "$2" -gt 0 ] && [ "$5" -gt 0 ]; then
	echo "OK: backup restaurável (vivo: $(counts "$SOURCE_DB"))"
else
	echo "FALHA: dump restaurado sem dados do central" >&2
	exit 1
fi
