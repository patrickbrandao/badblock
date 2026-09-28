#!/bin/sh
# Restaura um dump do badblock.
#
#   restore.sh <arquivo.dump> [banco-destino]
#
# O banco-destino (padrão: badblock) precisa existir. Os objetos são recriados
# com --clean --if-exists, então o destino fica igual ao momento do dump. A
# conexão vem das variáveis PG* padrão; use o superusuário ou o badblock_owner.
set -eu

dump="${1:?informe o arquivo .dump}"
target="${2:-badblock}"

[ -f "$dump" ] || { echo "arquivo não encontrado: $dump" >&2; exit 1; }

echo "restaurando $dump em $target"
# Extensões exigem superusuário e já são criadas pelo bootstrap: ficam fora.
list=$(mktemp)
pg_restore --list "$dump" | grep -vE ' EXTENSION - | COMMENT - EXTENSION ' >"$list"
pg_restore --clean --if-exists --no-owner --role=badblock_owner \
	--exit-on-error --use-list="$list" --dbname="$target" "$dump"
rm -f "$list"
echo "restauração concluída"
