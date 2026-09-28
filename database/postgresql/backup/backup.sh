#!/bin/sh
# Backup do banco badblock com pg_dump no formato custom (-Fc).
#
#   backup.sh daemon   espera o horário BACKUP_TIME (UTC) todo dia e faz o dump
#   backup.sh once     faz um dump agora e sai
#
# Retenção: BACKUP_KEEP_DAILY dumps em daily/ e, aos domingos, uma cópia em
# weekly/ com BACKUP_KEEP_WEEKLY cópias. A conexão vem das variáveis PG* padrão
# (PGHOST, PGUSER, PGPASSWORD, PGDATABASE).
set -eu

BACKUP_DIR="${BACKUP_DIR:-/backups}"
BACKUP_TIME="${BACKUP_TIME:-03:30}"
BACKUP_KEEP_DAILY="${BACKUP_KEEP_DAILY:-7}"
BACKUP_KEEP_WEEKLY="${BACKUP_KEEP_WEEKLY:-4}"

log() {
	echo "$(date -u +%Y-%m-%dT%H:%M:%SZ) backup: $*"
}

# Mantém só os $2 arquivos mais recentes do diretório $1.
prune() {
	# Os nomes são gerados por este script (badblock-AAAAMMDDTHHMMSSZ.dump).
	# shellcheck disable=SC2012
	ls -1 "$1"/badblock-*.dump 2>/dev/null | sort -r | tail -n +"$(($2 + 1))" |
		while read -r old; do
			rm -f "$old"
			log "removido $old"
		done
}

run_backup() {
	stamp=$(date -u +%Y%m%dT%H%M%SZ)
	mkdir -p "$BACKUP_DIR/daily" "$BACKUP_DIR/weekly"
	partial="$BACKUP_DIR/daily/.badblock-$stamp.partial"
	final="$BACKUP_DIR/daily/badblock-$stamp.dump"

	# O dump vai para um arquivo temporário e só ganha o nome final quando
	# termina: um dump interrompido nunca parece um backup válido.
	pg_dump --format=custom --compress=6 --file="$partial" "${PGDATABASE:-badblock}"
	mv "$partial" "$final"
	log "gravado $final ($(du -h "$final" | cut -f1))"

	if [ "$(date -u +%u)" = "7" ]; then
		cp "$final" "$BACKUP_DIR/weekly/"
		log "cópia semanal em $BACKUP_DIR/weekly/"
	fi

	prune "$BACKUP_DIR/daily" "$BACKUP_KEEP_DAILY"
	prune "$BACKUP_DIR/weekly" "$BACKUP_KEEP_WEEKLY"
}

# Segundos até o próximo BACKUP_TIME (UTC).
seconds_until_next() {
	now=$(date -u +%s)
	target=$(date -u -d "today $BACKUP_TIME" +%s)
	if [ "$target" -le "$now" ]; then
		target=$(date -u -d "tomorrow $BACKUP_TIME" +%s)
	fi
	echo $((target - now))
}

case "${1:-daemon}" in
once)
	run_backup
	;;
daemon)
	trap 'log "encerrando"; exit 0' TERM INT
	log "agendado para $BACKUP_TIME UTC (diários: $BACKUP_KEEP_DAILY, semanais: $BACKUP_KEEP_WEEKLY)"
	while :; do
		wait_s=$(seconds_until_next)
		# sleep em segundo plano para o trap responder na hora ao docker stop.
		sleep "$wait_s" &
		wait $!
		run_backup || log "ERRO: o dump falhou; nova tentativa no próximo horário"
	done
	;;
*)
	echo "uso: $0 [daemon|once]" >&2
	exit 2
	;;
esac
