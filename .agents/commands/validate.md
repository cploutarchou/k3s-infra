---
description: Validate the repo — run ./scripts/validate.sh and report honestly
allowed-tools: Bash, Read
---

Validate this repository before committing. From the repo root:

1. If `./scripts/validate.sh` exists, run it and show the full output.
2. If it does not exist, run what is available: kubeconform or
   `kubectl kustomize` over the manifest directories, ansible-lint over
   ansible/, `go build ./...` in Go modules, or the repo's own CI
   checks if they are discoverable.

Report each check pass/fail with the exact output that justifies the
verdict. If a check could not run (tool missing, no cluster access),
say so explicitly and give the exact command the operator would run.
Never report a check as passed unless it actually ran.
