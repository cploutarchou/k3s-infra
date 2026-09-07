# Using this repo from ZCode (Z.ai)

ZCode (zcode.z.ai) is Z.ai's coding harness for GLM models. This repo
ships the same operating knowledge Claude Code uses — skills, the
incident-triage subagent, the read-only cluster MCP server, and a guard
hook — packaged as a ZCode plugin so ZCode gets them too.

Observed behavior (2026-09-07, session with no k3s-infra plugin entry in
`~/.zcode/cli/plugins/installed_plugins.json`): ZCode loads skills
natively from the workspace `.agents/skills/` **and** user-globally from
`~/.agents/skills/`, and loads the subagent from the workspace
`.zcode/agents/`. The plugin remains the delivery mechanism for the
MCP wiring and the guard hook. ZCode also reads the workspace
`AGENTS.md` at session start.

| What                | Claude Code                 | ZCode                                             |
| ------------------- | --------------------------- | ------------------------------------------------- |
| Project rules       | `CLAUDE.md`                 | `AGENTS.md` (same content)                        |
| Skills (project)    | `.claude/skills/*/SKILL.md` | `.agents/skills/*/SKILL.md` (native)              |
| Skills (any repo)   | `~/.claude/skills/`         | `~/.agents/skills/` via `./skills/install.sh`     |
| Commands            | `.claude/commands/*.md` + `~/.claude/commands/` | `.agents/commands/*.md`, `~/.agents/commands/`, or plugin |
| Subagent            | `.claude/agents/*.md`       | `.zcode/agents/*.md` (workspace) or the plugin    |
| Cluster MCP server  | claude.ai connector         | `.zcode/mcp.json` via the plugin (`X-API-Key`)    |
| Write guard hook    | (none yet)                  | `.zcode/hooks/hooks.json` via the plugin          |
| Plugin manifest     | —                           | `.zcode-plugin/plugin.json` (repo root = plugin)  |

Slash commands (`/health`, `/triage`, `/validate`, `/sync-skills`) come
from the canonical `commands/` directory; `./skills/sync.sh` mirrors
them and `./skills/install.sh` installs them user-globally.

`.claude/skills` and `.agents/skills` must stay identical;
`./skills/sync.sh` keeps them (and the `dist/` zips) in sync and fails
on drift in the harness-specific agent variants. `./skills/install.sh`
refreshes the user-global copies (`~/.agents/skills`, `~/.claude`,
`~/.codex`) so the skills work in every repo on this machine.

## Install (once per machine)

0. Make the skills global for every ZCode session on this machine (any
   repo):

   ```sh
   ./skills/install.sh
   ```

   This also installs them for Claude Code and Codex. The triage agent
   has no ZCode-global location — in other repos it rides a plugin or a
   `.zcode/agents/` copy; in this repo it is already in place.

1. Get the MCP API key. It is the `api-key` field of the SOPS secret; the
   value is never written to disk in plain text and never pasted into chat:

   ```sh
   sops -d --extract '["stringData"]["api-key"]' \
     clusters/prod/apps/mcp/mcp-server-secret.sops.yaml | xclip -selection clipboard
   ```

   (Adjust the extract path if the secret uses `data` instead of
   `stringData`.)

2. In ZCode: **Settings → Plugins → Create → Add marketplace**, then
   **Choose directory** and pick this repo's root (the directory that
   contains `.zcode-plugin/plugin.json`). Install `k3s-infra` from the
   **Personal** segment.

3. When prompted for the plugin's user config, paste the key into
   **k3s-infra MCP API key**. It is stored by ZCode as a sensitive value
   and injected into the `X-API-Key` header at connect time.

## Verify

- **MCP**: Settings → MCP should list `k3s-infra` as connected with the
  tools `nodes`, `pods`, `events`, `logs`, `flux_status`, `cnpg_status`,
  `ha_report`, `flux_reconcile`, `propose_change`. Ask the agent to "run
  ha_report" — a 401 means the key was not applied.
- **Skills**: type `$` in the chat input; `gitops-workflow`,
  `verify-before-claiming`, `incident-triage`, `mcp-server-build`, and
  `ansible-day0-hardening` should be listed.
- **Commands**: type `/` in the chat input; the Commands group should
  list `health`, `triage`, `validate`, and `sync-skills`. A command
  missing from the menu but present on disk usually means a name
  collision with a built-in or a higher-precedence copy — see the
  zcode-guide `diagnosing-commands` skill.
- **Subagent**: ask "spawn the incident-triage agent to check why
  signwise is slow" — it should only read.
- **Guard hook**: ask the agent to run `kubectl get nodes` (allowed) and
  then `kubectl delete pod x -n default` (must be denied with an
  AGENTS.md rule 1 message before anything executes). The script can be
  exercised directly:

  ```sh
  echo '{"tool_name":"Bash","tool_input":{"command":"kubectl delete pod x"}}' \
    | .zcode/hooks/guard-cluster-writes.sh
  ```

## What the guard hook does and does not cover

Denies, in any position of a `;`, `&&`, `||` or pipe chain: `kubectl`
apply/create/delete/patch/edit/replace/scale/drain/cordon/uncordon/
rollout/label/annotate/taint/set; `helm` install/upgrade/uninstall/
rollback/delete; `flux` suspend/resume/delete/create/bootstrap/install/
uninstall; any `etcdctl`; any path under the k3s etcd data dir.

It does not inspect commands run over `ssh`, so AGENTS.md rule 2 (no
mutation on nodes) is still on the operator and the model. `flux
reconcile` is intentionally allowed.

## Notes

- Installing from GitHub instead of a local directory needs a
  marketplace catalog; this repo does not ship one yet.
- ZCode gives user-level config precedence over project config. If you
  already have an MCP server named `k3s-infra` in `~/.zcode/cli/config.json`,
  that one wins.
- The Codex mirror (`.codex/agents/incident-triage.toml`, `.agents/skills`)
  shares the skill directory with the ZCode plugin.
