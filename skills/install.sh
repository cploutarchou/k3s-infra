#!/usr/bin/env bash
# Install these skills + the triage agent for the current user, so they are
# active in EVERY Claude Code session on this machine (any repo).
# Usage: ./skills/install.sh   (run from the repo root or skills/)
set -euo pipefail

SRC="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
DEST_SKILLS="$HOME/.claude/skills"
DEST_AGENTS="$HOME/.claude/agents"
mkdir -p "$DEST_SKILLS" "$DEST_AGENTS"

for dir in "$SRC"/*/; do
  name="$(basename "$dir")"
  [ "$name" = "dist" ] && continue
  [ -f "$dir/SKILL.md" ] || continue
  mkdir -p "$DEST_SKILLS/$name"
  cp "$dir/SKILL.md" "$DEST_SKILLS/$name/SKILL.md"
  echo "skill installed: $name"
done

if [ -f "$SRC/incident-triage/agent.md" ]; then
  cp "$SRC/incident-triage/agent.md" "$DEST_AGENTS/incident-triage.md"
  echo "agent installed: incident-triage"
fi

echo "Done. New Claude Code sessions on this machine pick these up in any repo."
