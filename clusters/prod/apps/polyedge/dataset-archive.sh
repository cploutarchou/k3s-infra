#!/bin/sh
# Archive polyedge's recorded datasets to R2. Run by the
# polyedge-dataset-archive CronJob (dataset-archive.yaml); POSIX sh for
# the busybox shell in the rclone image.
#
# A dataset is a directory of gzip JSONL segments plus manifest.json (app
# repo internal/event/eventlog). Only a dataset's newest segment can still
# be written; every earlier one is closed and immutable, with its SHA-256
# in the manifest. Each run:
#   - lists what the bucket already holds, once;
#   - uploads every closed segment the bucket does not hold at its local
#     size, after checking its SHA-256 against the manifest, as a
#     single-part PUT with Content-MD5 that R2 verifies before storing and
#     rclone checks again from the response;
#   - treats a dataset as finished once its manifest has not changed for
#     STALE_MINUTES (the recorder rewrites it on every flush), then
#     archives its last segment and, after every segment is archived, its
#     manifest: an archived manifest.json marks a complete dataset;
#   - until then, keeps a copy of the current manifest as
#     manifest.partial.json, so the archived segments of an unfinished
#     dataset can be restored and checked against their SHA-256s;
#   - never deletes anything, locally or in the bucket.
# Exits 1 when anything could not be archived. The heartbeat (an Uptime
# Kuma push monitor, HEARTBEAT_URL) is sent only after a clean run.
set -u

ROOT=${DATASETS_DIR:-/var/lib/polyedge/datasets}
REMOTE=${ARCHIVE_REMOTE:-}
STALE_MINUTES=${STALE_MINUTES:-180}

log() { printf '%s %s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "$*"; }

[ -n "$REMOTE" ] || { log "ERROR ARCHIVE_REMOTE is not set (e.g. r2:polyedge-datasets)"; exit 2; }
case $STALE_MINUTES in '' | *[!0-9]*) log "ERROR STALE_MINUTES must be a whole number of minutes"; exit 2 ;; esac
[ -d "$ROOT" ] && [ -r "$ROOT" ] || { log "ERROR datasets directory $ROOT is missing or unreadable"; exit 2; }

WORK=$(mktemp -d) || { log "ERROR cannot create a work directory"; exit 2; }
trap 'rm -rf "$WORK"' EXIT

# Every object the bucket holds, as "key;size".
if ! rclone lsf -R --files-only --format ps --separator ';' "$REMOTE" >"$WORK/remote"; then
  log "ERROR cannot list $REMOTE"
  exit 1
fi

remote_size() { awk -F';' -v k="$1" '$1 == k { print $2; exit }' "$WORK/remote"; }

datasets=0 finished_n=0 uploaded=0 bytes=0 present=0 unverified=0 partials=0 errors=0
now=$(date +%s)

for dir in "$ROOT"/*/; do
  [ -d "$dir" ] || continue
  id=$(basename "$dir")
  case $id in
    [A-Za-z0-9]*) ;;
    *) log "WARN $id: not a dataset directory name, skipped"; continue ;;
  esac
  case $id in *[!A-Za-z0-9._-]* | *..*) log "WARN $id: not a dataset directory name, skipped"; continue ;; esac
  man="$dir/manifest.json"
  if [ ! -f "$man" ]; then
    log "WARN $id: no manifest.json yet, skipped"
    continue
  fi
  datasets=$((datasets + 1))

  mtime=$(stat -c %Y "$man") || { log "ERROR $id: cannot stat manifest.json"; errors=$((errors + 1)); continue; }
  finished=0
  [ $(((now - mtime) / 60)) -ge "$STALE_MINUTES" ] && finished=1

  # Segments before the manifest: the recorder writes a segment's SHA-256
  # into the manifest before it creates the next segment, so every segment
  # but the newest in this listing has its hash in the manifest read after.
  find "$dir" -maxdepth 1 -type f -name 'seg-[0-9][0-9][0-9][0-9][0-9][0-9].jsonl.gz' | sort >"$WORK/segs"
  last=$(tail -n 1 "$WORK/segs")

  # Segment name -> SHA-256, from a copy of the manifest (the recorder
  # renames a new one over it on every flush), which the app writes with
  # json.MarshalIndent: one key per line, in field order.
  if ! cp "$man" "$WORK/man" ||
    ! awk -F'"' '/^ *"name": "/ { n = $4 } /^ *"sha256": "/ { print n, $4 }' "$WORK/man" >"$WORK/sha"; then
    log "ERROR $id: cannot read manifest.json"
    errors=$((errors + 1))
    continue
  fi

  complete=1 have=0
  while IFS= read -r f; do
    seg=$(basename "$f")
    key="$id/$seg"
    if [ "$f" = "$last" ] && [ "$finished" -eq 0 ]; then
      complete=0 # the segment the recorder is still writing
      continue
    fi
    size=$(stat -c %s "$f") || { log "ERROR $key: cannot stat"; errors=$((errors + 1)); complete=0; continue; }
    rsize=$(remote_size "$key")
    if [ "$rsize" = "$size" ]; then
      present=$((present + 1)) have=$((have + 1))
      continue
    fi
    want=$(awk -v s="$seg" '$1 == s { print $2; exit }' "$WORK/sha")
    if [ -n "$want" ]; then
      got=$(sha256sum "$f" </dev/null | cut -d' ' -f1)
      if [ "$got" != "$want" ]; then
        log "ERROR $key: local SHA-256 $got differs from the manifest's $want; not uploaded"
        errors=$((errors + 1)) complete=0
        continue
      fi
    elif [ -n "$rsize" ]; then
      # Without a manifest hash nothing says which copy is right.
      log "ERROR $key: archived copy is $rsize bytes, local is $size and has no manifest hash; left as is"
      errors=$((errors + 1)) complete=0
      continue
    else
      log "WARN $key: no SHA-256 in the manifest (not closed cleanly); archiving as found"
      unverified=$((unverified + 1))
    fi
    [ -n "$rsize" ] && log "WARN $key: replacing the archived $rsize-byte copy with the verified $size-byte segment"
    if rclone copyto "$f" "$REMOTE/$key" </dev/null; then
      uploaded=$((uploaded + 1)) bytes=$((bytes + size)) have=$((have + 1))
      log "archived $key ($size bytes)"
    else
      log "ERROR $key: upload failed"
      errors=$((errors + 1)) complete=0
    fi
  done <"$WORK/segs"

  if [ "$finished" -eq 1 ] && [ "$complete" -eq 1 ]; then
    finished_n=$((finished_n + 1))
    msize=$(stat -c %s "$man")
    [ "$(remote_size "$id/manifest.json")" = "$msize" ] && continue
    if rclone copyto "$man" "$REMOTE/$id/manifest.json" </dev/null; then
      log "archived $id/manifest.json: dataset complete"
    else
      log "ERROR $id/manifest.json: upload failed"
      errors=$((errors + 1))
    fi
    continue
  fi
  [ "$finished" -eq 1 ] && log "WARN $id: finished, but not every segment is archived; manifest held back"

  # Not complete yet: a fresh copy of the manifest as manifest.partial.json,
  # which lists the SHA-256 of every archived closed segment. The active
  # dataset's changes on every flush, so it is sent again each run (a few
  # KB); a stalled dataset's only when it changes.
  [ "$have" -gt 0 ] || continue
  [ -n "$(remote_size "$id/manifest.json")" ] && continue
  if ! cp "$man" "$WORK/partial"; then
    log "ERROR $id: cannot read manifest.json"
    errors=$((errors + 1))
    continue
  fi
  [ "$(remote_size "$id/manifest.partial.json")" = "$(stat -c %s "$WORK/partial")" ] && continue
  if rclone copyto "$WORK/partial" "$REMOTE/$id/manifest.partial.json" </dev/null; then
    partials=$((partials + 1))
    log "archived $id/manifest.partial.json"
  else
    log "ERROR $id/manifest.partial.json: upload failed"
    errors=$((errors + 1))
  fi
done

log "summary datasets=$datasets finished=$finished_n uploaded=$uploaded bytes=$bytes already_archived=$present unverified=$unverified partials=$partials errors=$errors"
[ "$errors" -eq 0 ] || exit 1

if [ -n "${HEARTBEAT_URL:-}" ]; then
  wget -q -T 15 -O /dev/null "${HEARTBEAT_URL%%\?*}?status=up&msg=OK" || log "WARN heartbeat not delivered"
fi
exit 0
