#!/usr/bin/env bash
# Install these skills + the triage agent for the current user so they are
# active in EVERY session on this machine, in ANY repo, across harnesses:
#
#   ZCode        ~/.agents/skills/            skills
#                ~/.agents/commands/          slash commands
#   Claude Code  ~/.claude/skills/            skills
#                ~/.claude/commands/          slash commands
#                ~/.claude/agents/            incident-triage agent (markdown)
#   Codex        ~/.codex/agents/             incident-triage agent (TOML)
#
# ZCode has no user-global agent directory: the incident-triage agent is
# available in ZCode via this repo's plugin (.zcode/agents/, see
# docs/runbooks/zcode.md) or a workspace .zcode/agents/ copy — not from here.
#
# Usage: ./skills/install.sh   (run from the repo root or skills/)
set -euo pipefail

SRC="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$(cd "$SRC/.." && pwd)"

ZCODE_SKILLS="$HOME/.agents/skills"
ZCODE_COMMANDS="$HOME/.agents/commands"
CLAUDE_SKILLS="$HOME/.claude/skills"
CLAUDE_COMMANDS="$HOME/.claude/commands"
CLAUDE_AGENTS="$HOME/.claude/agents"
CODEX_AGENTS="$HOME/.codex/agents"

mkdir -p "$ZCODE_SKILLS" "$ZCODE_COMMANDS" "$CLAUDE_SKILLS" "$CLAUDE_COMMANDS" "$CLAUDE_AGENTS" "$CODEX_AGENTS"

for dir in "$SRC"/*/; do
  name="$(basename "$dir")"
  if [ "$name" = "dist" ] || [ ! -f "$dir/SKILL.md" ]; then
    continue
  fi
  for dest in "$ZCODE_SKILLS" "$CLAUDE_SKILLS"; do
    mkdir -p "$dest/$name"
    cp "$dir/SKILL.md" "$dest/$name/SKILL.md"
  done
  echo "skill installed: $name -> ~/.agents/skills (ZCode), ~/.claude/skills (Claude Code)"
done

for cmd in "$ROOT"/commands/*.md; do
  if [ -f "$cmd" ]; then
    name="$(basename "$cmd")"
    cp "$cmd" "$ZCODE_COMMANDS/$name"
    cp "$cmd" "$CLAUDE_COMMANDS/$name"
    echo "command installed: /${name%.md} -> ~/.agents/commands (ZCode), ~/.claude/commands (Claude Code)"
  fi
done

cp "$SRC/incident-triage/agent.md" "$CLAUDE_AGENTS/incident-triage.md"
echo "agent installed: incident-triage -> ~/.claude/agents (Claude Code)"

if [ -f "$ROOT/.codex/agents/incident-triage.toml" ]; then
  cp "$ROOT/.codex/agents/incident-triage.toml" "$CODEX_AGENTS/incident-triage.toml"
  echo "agent installed: incident-triage -> ~/.codex/agents (Codex)"
fi

echo "Done. Every ZCode, Claude Code, and Codex session on this machine"
echo "loads these skills (and, where supported, the agent) in any repo."
