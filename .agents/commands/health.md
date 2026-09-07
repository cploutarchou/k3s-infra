---
description: Read-only Kubernetes cluster health snapshot (nodes, pods, etcd, Flux, CNPG)
allowed-tools: Bash, Read
---

Take a read-only health snapshot of the Kubernetes cluster the current
kubectl context points at, and report it compactly. Never mutate
anything during this command — reads only.

Run and summarize:

- `kubectl get nodes -o wide`
- `kubectl get pods -A --field-selector=status.phase!=Running`
- `kubectl get --raw=/healthz/etcd`
- `flux get kustomizations` (if the flux CLI is installed)
- `kubectl get clusters.postgresql.cnpg.io -A` (if the CNPG CRDs exist)

If the k3s-infra MCP tools are available, prefer `ha_report` for the
cluster summary and use the raw commands only for what it omits.

Finish with: overall status (healthy / degraded / down), the specific
failures with evidence (the command and the relevant output line), and
any check you could not run — listed as not-run with the reason. Never
present an unrun check as passed.
