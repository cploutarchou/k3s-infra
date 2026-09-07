---
description: Re-sync the skills/commands kit mirrors and reinstall globally
allowed-tools: Bash, Read
---

Refresh the portable skills + commands kit in this repo. From the repo
root run `./skills/sync.sh`, then `./skills/install.sh`. Report what was
synced and quote any DRIFT errors verbatim — drift in `.zcode/agents/`
or `.codex/agents/` variants must be fixed by hand, not overwritten.

If `./skills/sync.sh` does not exist, this repo does not vendor the
kit — say so and stop. The canonical kit lives in the k3s-infra repo.
