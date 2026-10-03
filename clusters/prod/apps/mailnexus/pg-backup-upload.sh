#!/bin/sh
# Upload the backup that pg-backup-dump.sh left in $WORK_DIR to R2 and
# check what R2 stored. Runs in the rclone image (busybox sh) as the
# second step of the mailnexus-postgres-backup CronJob, only after the
# dump step exited 0.
#
# Layout: $BACKUP_REMOTE/postgres/YYYY/MM/DD/<stamp>/{postgres.dump.enc,manifest.txt}
# The <stamp> directory is what scripts/k8s-restore.sh takes once copied
# down. The manifest goes last, so a directory with a manifest is
# complete. Keys are never reused, so a bucket lock that forbids
# overwrites does not get in the way.
#
# Each file is sent as a single-part PUT with Content-MD5 (upload cutoff
# 5 GiB), which R2 verifies before storing; then the stored object's size,
# MD5 (its ETag) and SHA-256 (read back in full) are compared with the
# local file. Exit 1 on any mismatch. The heartbeat (an Uptime Kuma push
# monitor, HEARTBEAT_URL) is sent only after a clean run.
set -u

WORK=${WORK_DIR:-/work}
REMOTE=${BACKUP_REMOTE:-}

log() { printf '%s %s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "$*"; }

[ -n "$REMOTE" ] || { log "ERROR BACKUP_REMOTE is not set (e.g. r2:mailnexus-backups)"; exit 2; }
for f in postgres.dump.enc manifest.txt stamp; do
  [ -s "$WORK/$f" ] || { log "ERROR $WORK/$f is missing or empty; the dump step did not finish"; exit 1; }
done
stamp=$(cat "$WORK/stamp")
case $stamp in
  [0-9][0-9][0-9][0-9][0-9][0-9][0-9][0-9]T[0-9][0-9][0-9][0-9][0-9][0-9]Z) ;;
  *) log "ERROR unexpected stamp '$stamp'"; exit 1 ;;
esac
y=$(echo "$stamp" | cut -c1-4) m=$(echo "$stamp" | cut -c5-6) d=$(echo "$stamp" | cut -c7-8)
dest="$REMOTE/postgres/$y/$m/$d/$stamp"
dump="$WORK/postgres.dump.enc"

size=$(stat -c %s "$dump")
md5=$(md5sum "$dump" | cut -d' ' -f1)
sha256=$(sha256sum "$dump" | cut -d' ' -f1)
grep -qx "sha256=$sha256" "$WORK/manifest.txt" || { log "ERROR $dump does not match its manifest"; exit 1; }

rclone copyto "$dump" "$dest/postgres.dump.enc" || { log "ERROR upload of postgres.dump.enc failed"; exit 1; }

rsize=$(rclone lsf --format s "$dest/postgres.dump.enc")
rmd5=$(rclone md5sum "$dest/postgres.dump.enc" | cut -d' ' -f1)
rsha256=$(rclone cat "$dest/postgres.dump.enc" | sha256sum | cut -d' ' -f1)
if [ "$rsize" != "$size" ] || [ "$rmd5" != "$md5" ] || [ "$rsha256" != "$sha256" ]; then
  log "ERROR stored object differs: size $rsize/$size md5 $rmd5/$md5 sha256 $rsha256/$sha256"
  exit 1
fi

rclone copyto "$WORK/manifest.txt" "$dest/manifest.txt" || { log "ERROR upload of manifest.txt failed"; exit 1; }

log "summary key=${dest#*:} size=$size md5=$md5 sha256=$sha256 verified=yes"
if [ -n "${HEARTBEAT_URL:-}" ]; then
  wget -q -T 15 -O /dev/null "${HEARTBEAT_URL%%\?*}?status=up&msg=OK" || log "WARN heartbeat not delivered"
fi
exit 0
