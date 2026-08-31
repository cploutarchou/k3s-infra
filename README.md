# k3s-infra

Source of truth for a 3-node HA k3s cluster (embedded etcd) on netcup root
servers, reconciled by Flux from `clusters/prod/`. See `CLAUDE.md` for the
operating contract and `docs/` for architecture and runbooks.

| Node   | Public IP      | vLAN (eth1) |
| ------ | -------------- | ----------- |
| k3s-01 | 159.195.82.201 | 10.0.0.11   |
| k3s-02 | 159.195.81.219 | 10.0.0.12   |
| k3s-03 | 159.195.80.83  | 10.0.0.13   |

- Day-0 / node lifecycle: `ansible/` (hardening, vLAN, nftables, k3s HA install)
- Cluster state: `clusters/prod/` (Flux, kube-vip, Traefik, cert-manager,
  external-dns, CNPG)
- MCP server: `mcp/` (read-only cluster tools; writes go through GitHub PRs)
- Validation: `./scripts/validate.sh`
- Docs: `docs/architecture.md`, `docs/runbooks/`

Nothing is applied by hand: changes merge to `master`, Flux reconciles.
