#!/bin/sh
# Restore test, step 3 (rclone image, for its wget): push the heartbeat
# once the restore step has written its result. Steps run in order and
# a failed step stops the pod, so reaching here means the test passed.
set -u
WORK=${WORK_DIR:-/work}
log() { printf '%s %s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "$*"; }
[ -s "$WORK/result" ] || { log "ERROR no result from the restore step"; exit 1; }
log "restore test passed: $(cat "$WORK/result")"
if [ -n "${HEARTBEAT_URL:-}" ]; then
  wget -q -T 15 -O /dev/null "${HEARTBEAT_URL%%\?*}?status=up&msg=OK" || log "WARN heartbeat not delivered"
fi
exit 0
