#!/usr/bin/env bash
# PreToolUse guard for shell commands. Enforces hard rules 1 and 3 of
# AGENTS.md: no cluster write commands, no direct etcd access. The only
# sanctioned cluster write is asking Flux to reconcile. Reads the hook
# payload on stdin and answers with a deny decision when it matches.
set -u
payload="$(cat)"
if command -v jq >/dev/null 2>&1; then
  cmd="$(printf '%s' "$payload" | jq -r '.tool_input.command // .tool_input.cmd // ""' 2>/dev/null)"
else
  cmd="$(printf '%s' "$payload" | sed -n 's/.*"command"[[:space:]]*:[[:space:]]*"\(\([^"\\]\|\\.\)*\)".*/\1/p' | head -n1)"
fi
[ -n "$cmd" ] || exit 0

deny() {
  printf '{"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"deny","permissionDecisionReason":"%s"}}\n' "$1"
  exit 0
}

# Split on shell separators so chained commands are checked individually.
while IFS= read -r part || [ -n "$part" ]; do
  [ -n "$part" ] || continue
  if printf '%s' "$part" | grep -Eq '(^|[[:space:]])(sudo[[:space:]]+)?kubectl([[:space:]]+[^[:space:]]+)*[[:space:]]+(apply|create|delete|patch|edit|replace|scale|drain|cordon|uncordon|rollout|label|annotate|taint|set)([[:space:]]|$)'; then
    deny "Blocked by AGENTS.md rule 1: kubectl write commands are forbidden. Edit YAML under clusters/prod/ and let Flux reconcile."
  fi
  if printf '%s' "$part" | grep -Eq '(^|[[:space:]])(sudo[[:space:]]+)?helm([[:space:]]+[^[:space:]]+)*[[:space:]]+(install|upgrade|uninstall|rollback|delete)([[:space:]]|$)'; then
    deny "Blocked by AGENTS.md rule 1: helm install/upgrade/uninstall/rollback are forbidden. Change the HelmRelease manifest in git instead."
  fi
  if printf '%s' "$part" | grep -Eq '(^|[[:space:]])(sudo[[:space:]]+)?flux([[:space:]]+[^[:space:]]+)*[[:space:]]+(suspend|resume|delete|create|bootstrap|uninstall|install)([[:space:]]|$)'; then
    deny "Blocked by AGENTS.md rule 1: only 'flux reconcile' is a sanctioned cluster write. Everything else goes through git."
  fi
  if printf '%s' "$part" | grep -Eq '(^|[[:space:]/])(sudo[[:space:]]+)?etcdctl([[:space:]]|$)|/var/lib/rancher/k3s/server/db/'; then
    deny "Blocked by AGENTS.md rule 3: never touch etcd directly. Snapshot restore is a human break-glass procedure (docs/runbooks/etcd-restore.md)."
  fi
done < <(printf '%s' "$cmd" | sed -E 's/(&&|\|\||;|\|)/\n/g')
exit 0
