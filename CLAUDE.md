# k3s-infra

This repo is the source of truth for a 3-node HA k3s cluster on netcup root
servers. The cluster is reconciled from `clusters/prod/` by Flux. If a change
is not in git, it does not exist.

## Cluster facts

| Node   | Role              | Public IP      | vLAN (eth1) | OS        |
| ------ | ----------------- | -------------- | ----------- | --------- |
| k3s-01 | server (etcd, cp) | 159.195.82.201 | 10.0.0.11   | Debian 13 |
| k3s-02 | server (etcd, cp) | 159.195.81.219 | 10.0.0.12   | Debian 13 |
| k3s-03 | server (etcd, cp) | 159.195.80.83  | 10.0.0.13   | Debian 13 |

- All three are k3s servers with embedded etcd. Quorum is 2 of 3. Losing two
  nodes at once makes the cluster read-only until quorum returns.
- Hardware per node: 4 dedicated AMD EPYC cores, 8 GB RAM, 256 GB NVMe.
  Location: Nuremberg. SSH: root with key `~/.ssh/k3s-infra`.
- Cluster traffic runs on the vLAN (`--node-ip` on 10.0.0.x,
  `--flannel-iface eth1`). Public interfaces expose only 22/80/443.
- Domain: `cpdevlab.com`, DNS on Cloudflare (proxied). TLS via cert-manager
  with Cloudflare DNS-01.
- Backups: Cloudflare R2 bucket `k3s-backups` — etcd snapshots every 6 h,
  CNPG WAL archiving + base backups.
- Capacity rule: total workload requests stay under ~2/3 of cluster capacity
  (~13 GB RAM) so two nodes can absorb the loss of one.
- Debian 13 mounts `/tmp` as tmpfs. k3s and containerd work dirs must not
  rely on `/tmp`.
- GitOps: Flux from `clusters/prod/`. Secrets: SOPS + age.

## Hard rules

1. **Never run a write command against the cluster.** No `kubectl apply`,
   `delete`, `patch`, `edit`, `scale`, `drain`, `cordon`, `rollout restart`,
   `label`, `annotate`. No `helm install/upgrade/uninstall`. To change the
   cluster, edit YAML under `clusters/` and let Flux reconcile.
2. **Never SSH into a node to change anything.** Read-only inspection over
   SSH is fine (`journalctl`, `systemctl status`, `cat`, `df`, `free`,
   `ip`, `nft list ruleset`). Anything that mutates a node goes into
   `ansible/` as a playbook, or is handed to the operator as a copy-paste
   command in a clearly marked block.
3. **Never touch etcd directly.** No `etcdctl`, no editing
   `/var/lib/rancher/k3s/server/db/`. Snapshot restore is a break-glass
   procedure for a human, documented in `docs/runbooks/etcd-restore.md`.
4. **Never commit secrets.** Secrets go through SOPS (age). A plain
   `kind: Secret` with a base64 value in git is a bug — flag it, don't
   write it. Never print decrypted secret values into logs or chat.
5. **Never delete a PVC, StatefulSet, or CNPG Cluster manifest** without
   stating explicitly what data would be lost and waiting for confirmation.
6. **Never weaken authentication, authorization, or the firewall** (RBAC,
   Traefik middlewares, nftables rules, SSH config) without calling it out
   as a security-relevant change and waiting for confirmation.
7. **Do not claim a check passed unless you actually ran it.** If a check
   could not run (read-only boundary, missing credentials), say so and give
   the exact command for the operator.

## How to change the cluster

1. Read the current state (`kubectl get`, read the manifests in git).
2. Write or edit manifests under `clusters/prod/`.
3. Run local validation: `./scripts/validate.sh` (kubeconform +
   ansible-lint + SOPS check).
4. Open a branch and a PR for anything non-trivial. Do not push directly to
   `main` except for the batched commits allowed by the commit policy on
   changes the operator asked for in-session.
5. Report what will change once Flux reconciles, including anything that
   will restart and any capacity impact.

Node-level changes (packages, sysctl, firewall, k3s version) follow the same
pattern through `ansible/`: write the playbook, show the diff/plan, run only
after the operator approves.

## Conventions

- One directory per application under `clusters/prod/apps/<name>/` with
  `kustomization.yaml`, `deployment.yaml`, `service.yaml`, `ingress.yaml`,
  and `<name>-secret.sops.yaml` when needed. Register new apps in the parent
  kustomization so Flux picks them up.
- Kustomize overlays, not templated copies.
- Resource requests and limits are required on every container.
- Every Deployment needs a readiness probe. Liveness probes only where a
  hang is a real failure mode.
- Image tags pinned by digest. Never `:latest`.
- Databases: one CNPG database (or schema) per app on the shared postgres
  cluster; no ad-hoc database containers.
- Ingress hostnames: `<app>.cpdevlab.com`. Internal-only services get no
  Ingress at all.
- Go for cluster tooling and the MCP server; keep `mcp/` self-contained
  with its own Dockerfile and Helm chart.

## Repository layout

```text
ansible/          # day-0 and node lifecycle: hardening, vLAN, k3s install
clusters/prod/
  flux-system/    # Flux bootstrap manifests
  infrastructure/ # kube-vip, traefik, cert-manager, external-dns, cnpg, ...
  apps/           # one directory per workload
mcp/              # Go MCP server: read-only cluster tools + PR-based writes
scripts/          # validate.sh, kubeconfig minting, health report
docs/             # architecture, decisions, runbooks/
.claude/          # settings, skills, hooks, subagents
```

## Commit policy

- Batch work: at most 1 commit per 30 minutes. Accumulate changes and commit
  as one logical unit. Never commit per-file or per-edit.
- Conventional commits (`feat|fix|docs|chore|refactor(scope): ...`), no AI
  attribution, no emoji.
- Author is the machine's git identity (Christos Ploutarchou). Never
  override it.
- Never commit secrets. Encrypted SOPS files only.

## Verification standard

After any change, verify and report honestly:

- Manifests: `./scripts/validate.sh` output.
- Cluster health: `kubectl get nodes -o wide`,
  `kubectl get pods -A --field-selector=status.phase!=Running`,
  `kubectl get --raw=/healthz/etcd`, `flux get kustomizations`.
- HA posture: etcd members healthy, CNPG 1 primary + 2 replicas spread
  across nodes, latest etcd snapshot and WAL archive timestamps in R2,
  capacity headroom under the 2/3 rule.

Final reports include: what changed, exact files, migrations or restarts
triggered, commands executed with results, remaining risks, and anything
requiring manual verification.