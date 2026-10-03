#!/bin/bash
# Restore test, step 2 (postgres:16.9-bookworm image): restore the fetched
# backup into a throwaway PostgreSQL 16 started inside this pod, on an
# emptyDir, reachable only over its Unix socket, then check the result.
# It never connects to the live database.
#
# Decrypts with the same command as scripts/k8s-restore.sh and restores
# with the same pg_restore options (--no-owner), plus --exit-on-error so
# that any error fails the test. Writes $WORK_DIR/result on success.
set -euo pipefail

WORK=${WORK_DIR:-/work}
B="$WORK/backup"
DB=${RESTORE_DB:-plunk}
MIN_TABLES=${MIN_TABLES:-19}

log() { printf '%s %s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "$*"; }
fail() { log "ERROR $*"; exit 1; }

[ -n "${BACKUP_PASSPHRASE:-}" ] || { log "ERROR BACKUP_PASSPHRASE is not set"; exit 2; }
[ -s "$B/postgres.dump.enc" ] && [ -s "$B/manifest.txt" ] || fail "no fetched backup in $B"
key=$(cat "$B/key")
want_sha=$(sed -n 's/^sha256=//p' "$B/manifest.txt")
want_tables=$(sed -n 's/^tables=//p' "$B/manifest.txt")
got_sha=$(sha256sum "$B/postgres.dump.enc" | cut -d' ' -f1)
[ "$got_sha" = "$want_sha" ] || fail "$key: SHA-256 $got_sha differs from the manifest's $want_sha"

export PGDATA="$WORK/pgdata" PGHOST="$WORK/run" PGUSER=plunk
mkdir -p "$PGHOST"
initdb -U plunk --auth=trust -E UTF8 >/dev/null
trap 'pg_ctl stop -m fast >/dev/null 2>&1 || true' EXIT
pg_ctl -w -l "$WORK/postgres.log" \
  -o "-c listen_addresses='' -k $PGHOST -c shared_buffers=32MB -c fsync=off" start >/dev/null
createdb "$DB"

# The documented restore decrypts to a file first; a pipe is the same
# bytes without the plaintext on disk.
openssl enc -d -aes-256-cbc -pbkdf2 -in "$B/postgres.dump.enc" -pass env:BACKUP_PASSPHRASE |
  pg_restore -d "$DB" --no-owner --exit-on-error || fail "$key: decrypt or pg_restore failed"

q() { psql -XAtq -d "$DB" -c "$1"; }
tables=$(q "SELECT count(*) FROM pg_tables WHERE schemaname NOT IN ('pg_catalog', 'information_schema')")
[ "$tables" = "$want_tables" ] || fail "$key: restored $tables tables, the manifest says $want_tables"
[ "$tables" -ge "$MIN_TABLES" ] || fail "$key: only $tables tables restored (expected at least $MIN_TABLES)"
migrations=$(q "SELECT count(*) FROM _prisma_migrations WHERE finished_at IS NOT NULL AND rolled_back_at IS NULL") ||
  fail "$key: cannot read _prisma_migrations"
[ "$migrations" -gt 0 ] || fail "$key: no applied migrations restored"
users=$(q 'SELECT count(*) FROM users') || fail "$key: cannot read users"
projects=$(q 'SELECT count(*) FROM projects') || fail "$key: cannot read projects"
[ "$users" -gt 0 ] && [ "$projects" -gt 0 ] || fail "$key: users=$users projects=$projects, expected at least one of each"
contacts=$(q 'SELECT count(*) FROM contacts')
emails=$(q 'SELECT count(*) FROM emails')

echo "ok key=$key tables=$tables migrations=$migrations users=$users projects=$projects contacts=$contacts emails=$emails" >"$WORK/result"
log "$(cat "$WORK/result")"
