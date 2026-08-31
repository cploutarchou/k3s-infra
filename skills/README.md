# Portable skills

Reusable working patterns extracted from this project, written to be
dropped into any repository — they contain no cluster-specific facts.

## Installing into another project

Copy the folder(s) you want into the target repo's `.claude/skills/`:

```sh
cp -r skills/gitops-workflow /path/to/other-repo/.claude/skills/
```

Claude Code picks them up automatically; each skill triggers on the
situations named in its `description`, or explicitly via
`/<skill-name>`. To share across all your projects, copy into
`~/.claude/skills/` instead.

`incident-triage/` additionally contains `agent.md` — a subagent
definition. Install that one as `.claude/agents/incident-triage.md`
(agents and skills live in different directories).

## Where they are active

- **This repo (local and cloud)**: copies are committed under
  `.claude/skills/` and `.claude/agents/`, so every Claude Code session
  on this repo — including claude.ai/code cloud sessions, which clone
  the repo — loads them automatically. `skills/` remains the canonical,
  copy-out source; edit there and re-sync the `.claude/` copies.
- **Other repos (local or cloud)**: copy the folders into that repo's
  `.claude/skills/` (and the agent into `.claude/agents/`) and commit —
  cloud sessions only see what is committed.
- **claude.ai chat**: upload the ready-made zips from `dist/` via
  Settings → Capabilities → Skills (paid plans). Chat supports skills
  only — custom agents exist in Claude Code, not claude.ai chat; the
  incident-triage *skill* upload gives chat the same procedure without
  the agent's tool sandbox.

## Contents

| Folder | What it enforces |
| --- | --- |
| `gitops-workflow/` | manifest → validate → PR loop; digest pins, requests/limits, probes, SOPS-only secrets |
| `verify-before-claiming/` | verify versions against live sources, render before commit, never report an unrun check as passed |
| `mcp-server-build/` | remote MCP servers that claude.ai can actually connect to |
| `ansible-day0-hardening/` | day-0 server hardening: key-only SSH, default-drop firewall, serial + fail-fast |
| `incident-triage/` | read-only investigation, diagnosis before any fix |
