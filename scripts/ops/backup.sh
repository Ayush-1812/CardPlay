#!/bin/sh
# Logical backup of the CardPlay database. See docs/10-operations.md.
#
#   BACKUP_DATABASE_URL=postgres://... BACKUP_DIR=/var/backups/cardplay scripts/ops/backup.sh
#
# Writes cardplay-<UTC timestamp>.dump (pg_dump custom format) and a .sha256
# beside it, verifies the archive is readable, and deletes archives older than
# BACKUP_KEEP_DAYS (default 14). The connection URL is never printed.
set -eu

: "${BACKUP_DATABASE_URL:?set BACKUP_DATABASE_URL (a role that can read every CardPlay table)}"
: "${BACKUP_DIR:?set BACKUP_DIR}"
keep_days="${BACKUP_KEEP_DAYS:-14}"

mkdir -p "$BACKUP_DIR"
umask 077
stamp=$(date -u +%Y%m%dT%H%M%SZ)
file="$BACKUP_DIR/cardplay-$stamp.dump"
tmp="$file.partial"

# Custom format is compressed, restorable table by table, and consistent: pg_dump
# reads one snapshot, so matches, commands and outbox rows always agree.
pg_dump --format=custom --compress=6 --no-owner --no-privileges --file="$tmp" --dbname="$BACKUP_DATABASE_URL"

# An archive that cannot be listed cannot be restored; never keep it.
if ! pg_restore --list "$tmp" >/dev/null; then
	rm -f "$tmp"
	echo "backup failed: archive is unreadable" >&2
	exit 1
fi
mv "$tmp" "$file"
(cd "$BACKUP_DIR" && sha256sum "$(basename "$file")" >"$(basename "$file").sha256")

find "$BACKUP_DIR" -name 'cardplay-*.dump' -mtime +"$keep_days" -exec rm -f {} \; -exec rm -f {}.sha256 \;

size=$(wc -c <"$file" | tr -d ' ')
echo "backup ok: $file ($size bytes)"
