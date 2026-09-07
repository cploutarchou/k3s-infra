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

## Always-on tunnel (systemd --user)

`kube-tunnel install` renders `scripts/kube-tunnel.service` into
`~/.config/systemd/user/`, enables it and starts it. systemd restarts the
SSH session 5 s after any drop and the script walks k3s-01 → 02 → 03 on
each attempt, so the tunnel survives reboots, laptop sleep and a node
going away.

```sh
kube-tunnel install                       # once
sudo loginctl enable-linger "$USER"       # keep it up while logged out (optional)
systemctl --user status kube-tunnel       # health
journalctl --user -u kube-tunnel -f       # which hop is in use, reconnects
systemctl --user restart kube-tunnel      # force a fresh hop
systemctl --user disable --now kube-tunnel  # back to manual start/stop
```

`kube-tunnel status` still works (the unit uses the same control socket).
Do not use `kube-tunnel stop` while the unit is enabled: systemd will
simply reopen the tunnel. Use `systemctl --user stop kube-tunnel` instead.

## Second workstation

Everything the tunnel needs is in the repo except two credentials, which
must be copied over a secure channel (scp between your own machines, or a
password manager), never by chat or email:

| Item | Source | Target |
| ---- | ------ | ------ |
| SSH private key | `~/.ssh/k3s-infra` (mode 0600) | same path |
| kubeconfig (client cert + key) | `~/.kube/k3s-infra.yaml` (mode 0600) | same path |

Then on the new machine:

```sh
git clone git@github.com:cploutarchou/k3s-infra.git ~/workspace/k3s-infra
sudo apt-get install -y kubectl            # pkgs.k8s.io repo; see Setup above
cat >> ~/.ssh/config <<'EOF'

# k3s-infra (netcup) - read-only inspection only; changes go through ansible/
Host k3s-01
  HostName 159.195.82.201
Host k3s-02
  HostName 159.195.81.219
Host k3s-03
  HostName 159.195.80.83
Host k3s-0?
  User root
  IdentityFile ~/.ssh/k3s-infra
  IdentitiesOnly yes
EOF
cat >> ~/.bashrc <<'EOF'
export KUBECONFIG=$HOME/.kube/k3s-infra.yaml
alias k=kubectl
alias kube-tunnel="$HOME/workspace/k3s-infra/scripts/kube-tunnel.sh"
EOF
exec bash
ssh k3s-01 hostname                        # key works
kube-tunnel install                        # or: kube-tunnel start
kubectl get nodes -o wide
```

Prefer a **separate key per workstation** so one can be revoked without
touching the others: generate `ssh-keygen -t ed25519 -f ~/.ssh/k3s-infra
-C k3s-infra@<hostname>` on the new machine and, from a machine that
already has access, append its `.pub` to `/root/.ssh/authorized_keys` on
all three nodes. Ansible does not manage root's authorized_keys, so this is
a manual operator step on each node (SSH is key-only; there is no other
way in).

The kubeconfig carries a client certificate, not a token, so the same file
works from any machine. To revoke a workstation, delete its SSH public key
from the nodes; without the tunnel the kubeconfig is useless.

## Testing changes locally (no cluster writes)

```sh
./scripts/validate.sh                         # kubeconform + ansible-lint + SOPS check
kustomize build clusters/prod/apps/<name>     # render an overlay
kubectl diff -k clusters/prod/apps/<name>     # server-side dry-run against live state
```

`kubectl diff` sends a dry-run to the API and persists nothing; it is the
only cluster-touching command in the loop. Real changes still go through
git and Flux (see CLAUDE.md hard rule 1).
