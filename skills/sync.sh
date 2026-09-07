#!/usr/bin/env bash
# Re-sync the canonical skills in skills/ into the in-repo mirrors the
# different harnesses read, rebuild the dist/ zips, and fail on drift it
# cannot fix itself. Run after editing anything under skills/.
#
#   .claude/skills/<name>/SKILL.md      Claude Code (committed; cloud sessions clone them)
#   .agents/skills/<name>/SKILL.md      ZCode (project-level and via plugin) + Codex
#   .claude/agents/incident-triage.md   verbatim copy of skills/incident-triage/agent.md
#   skills/dist/<name>.zip              claude.ai chat upload bundles (SKILL.md only)
#
# .zcode/agents/incident-triage.md and .codex/agents/incident-triage.toml
# carry harness-specific frontmatter on top of agent.md; they are never
# overwritten here — only their description is checked against the
# canonical one so the variants cannot silently diverge.
#
# After syncing, run ./skills/install.sh to refresh the user-global copies
# (~/.agents/skills, ~/.claude, ~/.codex).
set -euo pipefail

SRC="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$(cd "$SRC/.." && pwd)"
fail=0

for dir in "$SRC"/*/; do
  name="$(basename "$dir")"
  if [ "$name" = "dist" ] || [ ! -f "$dir/SKILL.md" ]; then
    continue
  fi

  mkdir -p "$ROOT/.claude/skills/$name" "$ROOT/.agents/skills/$name"
  cp "$dir/SKILL.md" "$ROOT/.claude/skills/$name/SKILL.md"
  cp "$dir/SKILL.md" "$ROOT/.agents/skills/$name/SKILL.md"

  if command -v zip >/dev/null 2>&1; then
    rm -f "$SRC/dist/$name.zip"
    (cd "$SRC" && zip -qr "dist/$name.zip" "$name/SKILL.md")
  else
    echo "warn: zip not found, dist/$name.zip not rebuilt" >&2
  fi
done

if [ -f "$SRC/incident-triage/agent.md" ]; then
  mkdir -p "$ROOT/.claude/agents"
  cp "$SRC/incident-triage/agent.md" "$ROOT/.claude/agents/incident-triage.md"
fi

# Portable slash commands: canonical commands/ -> workspace mirrors for
# ZCode (.agents/commands, also the plugin root) and Claude Code.
for cmd in "$ROOT"/commands/*.md; do
  if [ -f "$cmd" ]; then
    mkdir -p "$ROOT/.claude/commands" "$ROOT/.agents/commands"
    cp "$cmd" "$ROOT/.claude/commands/"
    cp "$cmd" "$ROOT/.agents/commands/"
  fi
done

desc="$(sed -n 's/^description: //p' "$SRC/incident-triage/agent.md" | head -n1)"
for variant in "$ROOT/.zcode/agents/incident-triage.md" \
               "$ROOT/.codex/agents/incident-triage.toml"; do
  if [ -f "$variant" ] && ! grep -qF -- "$desc" "$variant"; then
    echo "DRIFT: description in $variant differs from skills/incident-triage/agent.md — update it by hand (it has harness-specific frontmatter)" >&2
    fail=1
  fi
done

if [ "$fail" -ne 0 ]; then
  exit 1
fi
echo "Mirrors in sync: .claude/skills, .agents/skills, .claude/agents, .claude/commands, .agents/commands, skills/dist."
