# mcp — k3s-infra MCP server

Go MCP server exposing the cluster to AI agents over streamable HTTP at
`/mcp`. Operator procedures (rotation, releases, emergency stop,
verification) live in `docs/runbooks/mcp-server.md`.

## Auth

One shared API key, sent as `X-API-Key: <key>` (what the claude.ai
connector sends) or `Authorization: Bearer <key>` (SDK / API clients).
Rules, enforced by `internal/auth` and pinned by its tests:

- `tools/call` — and every method outside the handshake — needs the key.
  Missing credential: `401` with `WWW-Authenticate: Bearer realm="mcp"`.
- A credential that is presented must be valid on **every** request,
  handshake included. Wrong, empty (`Bearer `) or malformed (`Basic …`)
  credentials get `401` with `error="invalid_token"`; they are never
  admitted as an anonymous probe.
- The handshake subset (`initialize`, `notifications/initialized`,
  `tools/list`, `ping`) and the GET/DELETE transport verbs are admitted
  **without** a credential while `MCP_PUBLIC_HANDSHAKE=true` (default),
  so a claude.ai connector can be added before its key is configured.
  That subset exposes the server name/version and the tool schemas, which
  are public in this repository anyway — never cluster data. Set
  `MCP_PUBLIC_HANDSHAKE=false` to require the key on every request.
- Batch (array) bodies, unparseable bodies and bodies over 1 MiB are never
  treated as a handshake.
- Comparison is constant-time over SHA-256 digests. The server refuses to
  start without a key. Rejections are logged with credential *presence*
  only (`credential=none|invalid`), never values.

## Tools

Read-only (`readOnlyHint: true`, non-destructive, idempotent): `nodes`,
`pods`, `events`, `logs` (256 KiB cap), `flux_status`, `cnpg_status`,
`ha_report` (node/etcd/pod/CNPG health + the 2/3 capacity rule).

Writes — deliberately narrow, annotated non-destructive:

- `propose_change`: opens a GitHub PR against this repo (new branch, one
  file, PR). Flux applies it after merge. The server never applies
  manifests itself. Needs `GITHUB_TOKEN`; without it the tool answers
  "GITHUB_TOKEN not configured on the server".
- `flux_reconcile`: sets `reconcile.fluxcd.io/requestedAt` on a
  Kustomization/HelmRelease. Only `patch` on those two Flux kinds is
  granted by RBAC.

## Configuration (env)

| Var                    | Purpose                                                      |
| ---------------------- | ------------------------------------------------------------ |
| `MCP_API_KEY`          | required; the server refuses to start without it             |
| `MCP_PUBLIC_HANDSHAKE` | default `true`; `false` = key required on every request      |
| `MCP_LISTEN_ADDR`      | default `:8080`                                              |
| `GITHUB_TOKEN`         | for `propose_change` (contents + pull-requests write scope)  |
| `GITHUB_OWNER` / `GITHUB_REPO` | target repo (`cploutarchou/k3s-infra`)               |

In-cluster it uses the pod ServiceAccount; locally it falls back to
`$KUBECONFIG` / `~/.kube/config`.

## Build, test, release

```sh
go test ./...                     # auth policy + tool annotation contract
go build ./...
docker build --provenance=false --sbom=false \
  -t ghcr.io/cploutarchou/k3s-infra-mcp:<version> .
```

Deployment is `clusters/prod/apps/mcp/` (HelmRelease on `mcp/chart`, SOPS
secret `mcp-server` with `api-key` and optionally `github-token`). The
chart takes the key only from that existing Secret — never as a value —
and the image is pinned by digest. The image is side-loaded onto the nodes
(no registry push yet); the runbook has the exact steps and how the pinned
digest is derived.
