#!/bin/bash
# Dump mailnexus's PostgreSQL for the mailnexus-postgres-backup CronJob
# (postgres-backup.yaml). Runs in the postgres:16.9-bookworm image, which
# has pg_dump 16.9 and the openssl CLI (the alpine image has no openssl).
#
# The artifact is the one the mailnexus repo's scripts/k8s-backup.sh
# writes: pg_dump -Fc of database plunk, encrypted with
#   openssl enc -aes-256-cbc -pbkdf2 -salt -pass env:BACKUP_PASSPHRASE
# so scripts/k8s-restore.sh restores it unchanged. -iter 10000 and
# -md sha256 are openssl's defaults for -pbkdf2, written out so that a
# future change of defaults cannot silently change the format.
#
# Writes to $WORK_DIR: postgres.dump.enc, manifest.txt and stamp. Nothing
# unencrypted touches disk: the dump goes straight into openssl, and the
# check after it decrypts into a pipe.
set -euo pipefail

WORK=${WORK_DIR:-/work}
OUT="$WORK/postgres.dump.enc"
ENC=(-aes-256-cbc -pbkdf2 -iter 10000 -md sha256)

log() { printf '%s %s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "$*"; }
fail() {
  log "ERROR $*"
  rm -f "$OUT" "$WORK/manifest.txt" "$WORK/stamp"
  exit 1
}

[ -n "${BACKUP_PASSPHRASE:-}" ] || { log "ERROR BACKUP_PASSPHRASE is not set"; exit 2; }
[ -n "${PGPASSWORD:-}" ] || { log "ERROR PGPASSWORD is not set"; exit 2; }

STAMP=$(date -u +%Y%m%dT%H%M%SZ)
server=$(psql -XAtq -c 'SHOW server_version') || fail "cannot connect to $PGUSER@$PGHOST/$PGDATABASE"
live_tables=$(psql -XAtq -c "SELECT count(*) FROM pg_tables WHERE schemaname NOT IN ('pg_catalog', 'information_schema')") ||
  fail "cannot count tables"

log "dumping $PGDATABASE from $PGHOST (server $server, $live_tables tables)"
pg_dump --no-password --lock-wait-timeout=120s -Fc |
  openssl enc "${ENC[@]}" -salt -pass env:BACKUP_PASSPHRASE -out "$OUT" ||
  fail "pg_dump or openssl failed"
# pipefail is what catches a failed pg_dump: openssl still writes a valid
# header and padding for an empty input, so the file is never empty.
[ -s "$OUT" ] || fail "encrypted dump is empty"

# Round trip: decrypt in a pipe, read the whole archive back as SQL (every
# data block decompressed), and count the tables it would create.
dumped_tables=$(openssl enc -d "${ENC[@]}" -pass env:BACKUP_PASSPHRASE -in "$OUT" |
  pg_restore -f - | grep -c -E '^CREATE (UNLOGGED )?TABLE ') ||
  fail "the encrypted dump does not decrypt and read back"
[ "$dumped_tables" = "$live_tables" ] ||
  fail "the dump creates $dumped_tables tables, the database has $live_tables"

size=$(stat -c %s "$OUT")
sha256=$(sha256sum "$OUT" | cut -d' ' -f1)
md5=$(md5sum "$OUT" | cut -d' ' -f1)
{
  echo "created=$(date -u +%Y-%m-%dT%H:%M:%SZ)"
  echo "host=$(hostname)"
  echo "source=k3s CronJob mailnexus/mailnexus-postgres-backup"
  echo "database=$PGDATABASE"
  echo "server_version=$server"
  echo "pg_dump_version=$(pg_dump --version)"
  echo "format=pg_dump -Fc"
  echo "encrypted=yes"
  echo "cipher=openssl enc ${ENC[*]} -salt (salt length 8)"
  echo "tables=$dumped_tables"
  echo "file=postgres.dump.enc"
  echo "size=$size"
  echo "sha256=$sha256"
  echo "md5=$md5"
} >"$WORK/manifest.txt"
echo "$STAMP" >"$WORK/stamp"
log "ok stamp=$STAMP size=$size tables=$dumped_tables sha256=$sha256"
