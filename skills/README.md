# Portable skills & commands

Reusable working patterns extracted from this project, written to be
dropped into any repository — they contain no cluster-specific facts.
`skills/` holds the skills and the agent; `commands/` (repo root) holds
the reusable slash commands (`/health`, `/triage`, `/validate`,
`/sync-skills`) that ZCode and Claude Code both load from their
`commands` directories.

## Editing and re-distributing

`skills/` is the canonical source; everything else is a generated copy.
After editing anything here:

```sh
./skills/sync.sh      # refresh in-repo mirrors + dist/ zips, check drift
./skills/install.sh   # refresh the user-global copies on this machine
```

Both scripts also cover `commands/*.md`: `sync.sh` mirrors them to
`.claude/commands/` and `.agents/commands/` (ZCode workspace + plugin
root), `install.sh` copies them to `~/.agents/commands/` (ZCode,
user-global) and `~/.claude/commands/` (Claude Code).

`sync.sh` never touches `.zcode/agents/` or `.codex/agents/` — those
carry harness-specific frontmatter on top of `agent.md` and are updated
by hand (the script fails if their description drifts from the
canonical one).

## Installing into another project

Copy the folder(s) you want into the target repo:

```sh
# Claude Code reads .claude/skills/, ZCode (and Codex) read .agents/skills/
cp -r skills/gitops-workflow /path/to/other-repo/.claude/skills/
cp -r skills/gitops-workflow /path/to/other-repo/.agents/skills/
```

Each skill triggers on the situations named in its `description`, or
explicitly via `/<skill-name>`.

`incident-triage/` additionally contains `agent.md` — a subagent
definition. Install that one as `.claude/agents/incident-triage.md`
(Claude Code) and/or `.zcode/agents/incident-triage.md` (ZCode); agents
and skills live in different directories.

## Where they are active

- **This repo (local and cloud)**: copies are committed under
  `.claude/skills/` and `.agents/skills/`, so every session on this
  repo — including claude.ai/code cloud sessions and ZCode — loads them
  automatically. `skills/` remains the canonical, copy-out source;
  re-sync with `./skills/sync.sh` after edits.
- **All repos for one user (this machine)**: run `./skills/install.sh` —
  copies everything into `~/.agents/skills/` (ZCode), `~/.claude/skills/`
  + `~/.claude/agents/` (Claude Code), and `~/.codex/agents/` (Codex
  agent), so every local session on any repo loads them. ZCode loads
  global skills from `~/.agents/skills/`; it has no user-global agent
  directory, so the triage agent reaches ZCode via a repo's plugin or
  workspace `.zcode/agents/` copy.
- **Other repos (local or cloud)**: copy the folders into that repo's
  `.claude/skills/` and `.agents/skills/` and commit — cloud sessions
  only see what is committed.
- **claude.ai chat**: upload the ready-made zips from `dist/` via
  Settings → Capabilities → Skills (paid plans). Chat supports skills
  only — custom agents exist in Claude Code/ZCode, not claude.ai chat;
  the incident-triage *skill* upload gives chat the same procedure
  without the agent's tool sandbox.

## Distributing to coworkers

- **Simplest**: point them at this repo — `git clone … && ./skills/install.sh`
  covers all their local ZCode/Claude Code/Codex sessions; the `dist/`
  zips cover their claude.ai chat.
- **Per-project**: commit the copies into each shared repo's
  `.claude/skills/` and `.agents/skills/` (as done here) — then every
  collaborator, cloud session, and ZCode instance on that repo gets
  them with zero setup.
- **Org-wide claude.ai**: on Team/Enterprise plans, workspace admins can
  manage skills centrally from the admin settings — check with your
  admin, as availability depends on plan and rollout; individual zip
  upload always works as the fallback.

## Contents

| Folder | What it enforces |
| --- | --- |
| `gitops-workflow/` | manifest → validate → PR loop; digest pins, requests/limits, probes, SOPS-only secrets |
| `verify-before-claiming/` | verify versions against live sources, render before commit, never report an unrun check as passed |
| `mcp-server-build/` | remote MCP servers that claude.ai can actually connect to |
| `ansible-day0-hardening/` | day-0 server hardening: key-only SSH, default-drop firewall, serial + fail-fast |
| `incident-triage/` | read-only investigation, diagnosis before any fix |

## Commands (`commands/` at the repo root)

| Command | What it does |
| --- | --- |
| `health` | read-only cluster health snapshot: nodes, non-running pods, etcd, Flux, CNPG |
| `triage` | runs the incident-triage procedure (auto-mounts the skill, delegates to the agent when present) |
| `validate` | runs the repo's `./scripts/validate.sh` (or whatever linters exist) and reports honestly |
| `sync-skills` | re-runs `./skills/sync.sh` + `./skills/install.sh` and surfaces drift errors |
