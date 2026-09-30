#!/bin/sh
# Restore a CardPlay backup into an EMPTY database. See docs/10-operations.md.
#
#   RESTORE_DATABASE_URL=postgres://.../cardplay_restore scripts/ops/restore.sh /var/backups/cardplay/cardplay-<stamp>.dump
#
# Refuses to touch a database that already has CardPlay tables, checks the
# archive checksum, restores in one transaction (all or nothing), and prints
# row counts for the core tables so the operator can compare with the source.
set -eu

file="${1:?usage: restore.sh <backup.dump>}"
: "${RESTORE_DATABASE_URL:?set RESTORE_DATABASE_URL to an empty target database}"

if [ -f "$file.sha256" ]; then
	(cd "$(dirname "$file")" && sha256sum --check --status "$(basename "$file").sha256") || {
		echo "restore refused: checksum mismatch for $file" >&2
		exit 1
	}
else
	echo "restore refused: $file.sha256 is missing" >&2
	exit 1
fi

existing=$(psql --dbname="$RESTORE_DATABASE_URL" --no-align --tuples-only --command \
	"SELECT count(*) FROM information_schema.tables WHERE table_schema='public'")
if [ "$existing" != "0" ]; then
	echo "restore refused: the target database is not empty ($existing tables)" >&2
	exit 1
fi

pg_restore --no-owner --no-privileges --exit-on-error --single-transaction \
	--dbname="$RESTORE_DATABASE_URL" "$file"

psql --dbname="$RESTORE_DATABASE_URL" --no-align --tuples-only --field-separator=' ' --command "
SELECT 'schema_migrations', count(*) FROM schema_migrations
UNION ALL SELECT 'users', count(*) FROM users
UNION ALL SELECT 'rooms', count(*) FROM rooms
UNION ALL SELECT 'matches', count(*) FROM matches
UNION ALL SELECT 'game_commands', count(*) FROM game_commands
UNION ALL SELECT 'room_chat', count(*) FROM room_chat"
echo "restore ok: run 'cardplay migrate' against the restored database, then check /readyz"
