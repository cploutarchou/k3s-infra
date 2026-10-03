#!/bin/sh
# Restore test, step 1 (rclone image): copy the newest complete backup
# (the newest <stamp>/ holding a manifest.txt) from $BACKUP_REMOTE/postgres
# into $WORK_DIR/backup. Fails when the newest one is older than
# MAX_AGE_HOURS, so a backup job that stopped running fails this test too.
set -u

WORK=${WORK_DIR:-/work}
REMOTE=${BACKUP_REMOTE:-}
MAX_AGE_HOURS=${MAX_AGE_HOURS:-30}

log() { printf '%s %s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "$*"; }

[ -n "$REMOTE" ] || { log "ERROR BACKUP_REMOTE is not set"; exit 2; }
if ! rclone lsf -R --files-only --include '/*/*/*/*/manifest.txt' "$REMOTE/postgres" >"$WORK/listing"; then
  log "ERROR cannot list $REMOTE/postgres"
  exit 1
fi
newest=$(sort "$WORK/listing" | tail -n 1)
[ -n "$newest" ] || { log "ERROR no complete backup under $REMOTE/postgres"; exit 1; }
dir=${newest%/manifest.txt}
stamp=${dir##*/}

# Age from the stamp (YYYYMMDDTHHMMSSZ).
ts=$(echo "$stamp" | sed -E 's/^(....)(..)(..)T(..)(..)(..)Z$/\1-\2-\3 \4:\5:\6/')
then_s=$(date -u -d "$ts" +%s 2>/dev/null) || { log "ERROR cannot read the date in '$stamp'"; exit 1; }
age_h=$((($(date -u +%s) - then_s) / 3600))
[ "$age_h" -le "$MAX_AGE_HOURS" ] || { log "ERROR newest backup $stamp is ${age_h}h old (limit ${MAX_AGE_HOURS}h)"; exit 1; }

mkdir -p "$WORK/backup"
rclone copy "$REMOTE/postgres/$dir" "$WORK/backup" || { log "ERROR cannot download $dir"; exit 1; }
echo "$dir" >"$WORK/backup/key"
log "fetched postgres/$dir (${age_h}h old)"
