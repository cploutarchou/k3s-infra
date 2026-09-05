# Local kubectl access

The API server (6443) is only reachable on the vLAN; public interfaces
expose 22/80/443. Local `kubectl` therefore goes through an SSH tunnel to
the kube-vip control-plane VIP (`10.0.0.10`), hopping through any node.

## Setup (once per workstation)

- `kubectl` from the pkgs.k8s.io apt repo (client may be at most one minor
  ahead of the server).
- `~/.kube/k3s-infra.yaml`: server `https://127.0.0.1:16443`,
  `tls-server-name: 10.0.0.10`. The VIP and all node IPs are in the API
  cert SANs, so the same file works whichever node holds the VIP.
- `~/.ssh/config` aliases `k3s-01..03` using `~/.ssh/k3s-infra`.
- `~/.bashrc`: `export KUBECONFIG=$HOME/.kube/k3s-infra.yaml`, kubectl and
  flux completion, `alias k=kubectl`, `alias kube-tunnel=scripts/kube-tunnel.sh`.

## Daily use

```sh
kube-tunnel start        # tries k3s-01, then 02, then 03
kubectl get nodes -o wide
flux get kustomizations
kube-tunnel status
kube-tunnel stop
```

`kube-tunnel start <ip>` forces a specific hop. The tunnel uses an SSH
control socket under `$XDG_RUNTIME_DIR`, so `stop` is clean and `start` is
idempotent.

## Testing changes locally (no cluster writes)

```sh
./scripts/validate.sh                         # kubeconform + ansible-lint + SOPS check
kustomize build clusters/prod/apps/<name>     # render an overlay
kubectl diff -k clusters/prod/apps/<name>     # server-side dry-run against live state
```

`kubectl diff` sends a dry-run to the API and persists nothing; it is the
only cluster-touching command in the loop. Real changes still go through
git and Flux (see CLAUDE.md hard rule 1).
