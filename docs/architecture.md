# Architecture

## Topology

Three netcup root servers (4 EPYC cores, 8 GB RAM, 256 GB NVMe each) in
Nuremberg, all running Debian 13 and k3s in server mode with embedded etcd
(quorum 2 of 3).

| Node   | Public IP      | vLAN eth1 | Roles           |
| ------ | -------------- | --------- | --------------- |
| k3s-01 | 159.195.82.201 | 10.0.0.11 | server, etcd    |
| k3s-02 | 159.195.81.219 | 10.0.0.12 | server, etcd    |
| k3s-03 | 159.195.80.83  | 10.0.0.13 | server, etcd    |

## Networking

- All cluster traffic (API, etcd, flannel VXLAN, kubelet) rides the netcup
  cloud vLAN on `eth1` (10.0.0.0/24, MTU 1400): k3s runs with
  `--node-ip 10.0.0.x --flannel-iface eth1 --advertise-address 10.0.0.x`.
- kube-vip holds the control-plane VIP **10.0.0.10** (ARP, leader-elected)
  on the vLAN; the k3s config lists it in `tls-san`. Kubeconfigs for use
  inside the vLAN point at the VIP; from outside, at a node public IP
  (also in `tls-san`).
- Public interfaces expose only 22/80/443, enforced by nftables
  (`ansible/roles/nftables`). Everything from 10.0.0.0/24 on eth1 is
  trusted.
- Ingress: Traefik as a DaemonSet with hostPorts 80/443 on every node.
  DNS on Cloudflare (proxied); external-dns publishes ingress hostnames
  with the three node public IPs as default targets. TLS via cert-manager
  (Let's Encrypt, Cloudflare DNS-01 — works behind the proxy).

## GitOps

Flux reconciles `clusters/prod/` from git. Layering:

1. `flux-system` — Flux itself (created by `flux bootstrap`).
2. `infra-controllers` — kube-vip, Traefik, cert-manager, external-dns,
   CNPG operator.
3. `infra-configs` (dependsOn controllers) — ClusterIssuers, shared
   postgres Cluster.
4. `apps` (dependsOn configs) — one directory per workload.

Secrets are SOPS-encrypted with age; Flux decrypts with the `sops-age`
secret. A plain `kind: Secret` in git is a bug.

## Data

- Shared CNPG postgres cluster `databases/postgres`: 1 primary + 2
  replicas, required anti-affinity (one per node), local-path storage,
  WAL archiving + nightly base backups to R2. One database per app.
- Backups to Cloudflare R2 bucket `k3s-backups` (EU jurisdiction,
  endpoint `https://035087c37abeda0a744ab1c4c482d19f.eu.r2.cloudflarestorage.com`):
  - etcd snapshots every 6 h from each server (k3s `etcd-s3`).
  - CNPG WAL + base backups under `s3://k3s-backups/postgres`.

## Capacity rule

Total workload requests stay under ~2/3 of cluster capacity (~13 GB RAM,
~8 cores after system reservations) so two nodes can absorb the loss of
one. The MCP `ha_report` tool checks this.

## Debian 13 note

`/tmp` is tmpfs. The hardening role caps it at 1 GiB and k3s runs with
`TMPDIR=/var/lib/rancher/tmp` (NVMe) so image imports and snapshot staging
never land in RAM.
