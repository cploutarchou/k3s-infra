#!/usr/bin/env bash
# Open an SSH tunnel from 127.0.0.1:16443 to the k3s API VIP (10.0.0.10:6443)
# so a local kubectl (KUBECONFIG=~/.kube/k3s-infra.yaml) can reach the cluster.
# The API port is not exposed publicly; only 22/80/443 are.
#
#   kube-tunnel.sh start   [node]   # try k3s-01, k3s-02, k3s-03 in order
#   kube-tunnel.sh stop
#   kube-tunnel.sh status
set -euo pipefail

LOCAL_PORT="${KUBE_TUNNEL_LOCAL_PORT:-16443}"
API_VIP="${KUBE_TUNNEL_API_VIP:-10.0.0.10}"
SSH_KEY="${KUBE_TUNNEL_SSH_KEY:-$HOME/.ssh/k3s-infra}"
SOCK="${XDG_RUNTIME_DIR:-/tmp}/k3s-infra-kube-tunnel.sock"
NODES=(159.195.82.201 159.195.81.219 159.195.80.83)  # k3s-01, k3s-02, k3s-03

ssh_opts=(-i "$SSH_KEY" -o BatchMode=yes -o ConnectTimeout=8
          -o ExitOnForwardFailure=yes -o ServerAliveInterval=30
          -o ServerAliveCountMax=3 -S "$SOCK")

is_up() { ssh -S "$SOCK" -O check "root@${NODES[0]}" >/dev/null 2>&1; }

case "${1:-status}" in
  start)
    if is_up; then echo "tunnel already up on 127.0.0.1:${LOCAL_PORT}"; exit 0; fi
    rm -f "$SOCK"
    if [[ -n "${2:-}" ]]; then NODES=("$2"); fi
    for n in "${NODES[@]}"; do
      if ssh "${ssh_opts[@]}" -M -fN -L "127.0.0.1:${LOCAL_PORT}:${API_VIP}:6443" "root@${n}"; then
        echo "tunnel up: 127.0.0.1:${LOCAL_PORT} -> ${API_VIP}:6443 via ${n}"
        echo "export KUBECONFIG=\$HOME/.kube/k3s-infra.yaml"
        exit 0
      fi
      echo "hop ${n} failed, trying next" >&2
    done
    echo "could not open tunnel via any node" >&2; exit 1 ;;
  stop)
    if is_up; then ssh -S "$SOCK" -O exit "root@${NODES[0]}" 2>/dev/null; echo "tunnel stopped"
    else rm -f "$SOCK"; echo "no tunnel running"; fi ;;
  status)
    if is_up; then echo "tunnel up on 127.0.0.1:${LOCAL_PORT}"; else echo "tunnel down"; exit 1; fi ;;
  *) sed -n 2,9p "$0"; exit 2 ;;
esac
