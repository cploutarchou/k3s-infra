# mcp — k3s-infra MCP server

Go MCP server exposing the cluster to AI agents over streamable HTTP at
`/mcp`. Auth: the API key travels as `X-API-Key: <key>` or
`Authorization: Bearer <key>`. The handshake subset (`initialize`,
`notifications/initialized`, `tools/list`, `ping`) is deliberately
unauthenticated so connector clients (claude.ai) can probe the URL before
credentials are configured — it exposes only the server name and tool
schemas. Every `tools/call` requires the key; unauthorized calls get 401
with a `WWW-Authenticate` header.

## Tools

Read-only: `nodes`, `pods`, `events`, `logs`, `flux_status`, `cnpg_status`,
`ha_report` (node/etcd/pod/CNPG health + the 2/3 capacity rule).

Writes — deliberately narrow:

- `propose_change`: opens a GitHub PR against this repo (new branch, one
  file, PR). Flux applies it after merge. The server never applies
  manifests itself.
- `flux_reconcile`: sets `reconcile.fluxcd.io/requestedAt` on a
  Kustomization/HelmRelease. Only `patch` on those two Flux kinds is
  granted by RBAC.

## Configuration (env)

| Var               | Purpose                                    |
| ----------------- | ------------------------------------------ |
| `MCP_API_KEY`     | required; server refuses to start without  |
| `MCP_LISTEN_ADDR` | default `:8080`                            |
| `GITHUB_TOKEN`    | for `propose_change` (contents + PR scope) |
| `GITHUB_OWNER` / `GITHUB_REPO` | target repo (`cploutarchou/k3s-infra`) |

In-cluster it uses the pod ServiceAccount; locally it falls back to
`$KUBECONFIG` / `~/.kube/config`.

## Build & deploy

```sh
go build ./...                    # binary
docker build -t ghcr.io/cploutarchou/k3s-infra-mcp:0.1.0 .
```

Deployment goes through git: add `clusters/prod/apps/mcp/` (HelmRelease
referencing `mcp/chart`, SOPS secret `mcp-server` with `api-key` +
`github-token`), register it in `clusters/prod/apps/kustomization.yaml`,
and set `image.digest` in the values — never `:latest`.
