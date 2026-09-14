# MCP server (mcp.cpdevlab.com)

The k3s-infra MCP server (`mcp/`) gives AI agents read-only cluster tools
plus a PR-based write path. Manifests: `clusters/prod/apps/mcp/`.

| Piece | Where |
| --- | --- |
| Source, tests, Dockerfile | `mcp/` (Go, `mark3labs/mcp-go`, streamable HTTP, stateless) |
| Chart | `mcp/chart`, released by the HelmRelease in `clusters/prod/apps/mcp/helmrelease.yaml` |
| API key | `api-key` in `mcp-server-secret.sops.yaml` → env `MCP_API_KEY` |
| GitHub token (optional) | `github-token` in the same secret → env `GITHUB_TOKEN`; absent today, so `propose_change` is inert |
| Endpoint | `https://mcp.cpdevlab.com/mcp` (Cloudflare → Traefik → Service), `/healthz` unauthenticated |
| Replicas | 2, required anti-affinity across nodes, drift detection on |
| Clients | claude.ai custom connector (`X-API-Key`), ZCode plugin (`docs/runbooks/zcode.md`), Claude Code / curl |

## Auth model (what a client must expect)

| Request | No credential | Wrong / empty credential | Correct key |
| --- | --- | --- | --- |
| `initialize`, `notifications/initialized`, `tools/list`, `ping` | 200 (handshake is public\*) | 401 `invalid_token` | 200 |
| `tools/call` and every other method | 401 `Bearer realm="mcp"` | 401 `invalid_token` | 200 |
| GET (notification stream), DELETE | passes through\* | 401 `invalid_token` | passes |
| Batch / unparseable / >1 MiB body | 401 | 401 | passes to the transport |

\* only while `auth.publicHandshake: true` in the HelmRelease values. With
`false` every row's first column becomes 401. The public handshake exists
because the claude.ai connector probes the URL before its key is
configured; it reveals only the server name and the tool schemas.

The key travels as `X-API-Key: <key>` or `Authorization: Bearer <key>`.
The 2026-08-31 Traefik access-log capture showed the claude.ai connector
sends `X-Api-Key` on every request and never `Authorization`.

## Verify (after any deploy, from the workstation)

Only the handshake may answer 200 without a key. Everything below is
read-only against the live endpoint; `$KEY` comes from the secret (see
Rotation) and must never be pasted into chat or logs.

```bash
U=https://mcp.cpdevlab.com/mcp
H='Content-Type: application/json'
CALL='{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"nodes","arguments":{}}}'
INIT='{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-03-26","capabilities":{},"clientInfo":{"name":"curl","version":"0"}}}'
# expect 401
curl -sS -o /dev/null -w '%{http_code}\n' -H "$H" -X POST --data "$CALL" "$U"
curl -sS -o /dev/null -w '%{http_code}\n' -H "$H" -H 'Authorization: Bearer wrong' -X POST --data "$INIT" "$U"
curl -sS -o /dev/null -w '%{http_code}\n' -H "$H" -H 'Authorization: Bearer ' -X POST --data "$CALL" "$U"
# expect 200 + the three nodes
KEY=$(sops -d --extract '["stringData"]["api-key"]' clusters/prod/apps/mcp/mcp-server-secret.sops.yaml)
curl -sS -H "$H" -H "X-API-Key: $KEY" -X POST --data "$CALL" "$U" | sed 's/^data: //' | jq -r '.result.content[0].text' | jq -r 'map(.name)'
unset KEY
```

Then, from a Claude session with the connector attached, ask it to run
`nodes`. A tool error mentioning 401 means the connector is not sending
the current key: update the connector's `X-API-Key` value in claude.ai
(Settings → Connectors → k3s-infra). Server-side, a connector without a
key shows up in the pod logs as:

```bash
kubectl -n mcp logs deploy/mcp-server | grep 'auth: rejected'
# auth: rejected POST /mcp credential=none rpc="tools/call" client=<ip> ua="..."
```

## Rotate the API key

Rotation is three git changes and one client update. The old key stays
valid until the new pods start, so update clients right after the merge.

```bash
# 1. New 256-bit key into the SOPS secret; the value never hits the terminal.
F=clusters/prod/apps/mcp/mcp-server-secret.sops.yaml
T=$(mktemp) && printf '"%s"' "$(openssl rand -hex 32)" > "$T"
sops set --value-file "$F" '["stringData"]["api-key"]' "$T" && shred -u "$T"
# 2. Force a rollout — env from secretKeyRef is read only at container start.
#    In clusters/prod/apps/mcp/helmrelease.yaml bump
#    values.podAnnotations."k3s-infra.cpdevlab.com/secret-rotated-at" to today.
# 3. PR → merge. Flux applies the Secret and rolls the Deployment.
# 4. Update every client (never paste the key into chat):
sops -d --extract '["stringData"]["api-key"]' "$F" | xclip -selection clipboard
```

Clients to update: the claude.ai connector header, the ZCode plugin user
config (`docs/runbooks/zcode.md`), any `claude mcp add --header
"X-API-Key: …"` entry, and shell profiles that export the key for curl.

## Release a new image

The image is side-loaded into containerd on all three nodes (no registry
push yet — `docs/decisions.md`). The HelmRelease pins the manifest digest
containerd assigns on import, which for a `docker save` archive is the
digest in the archive's `index.json`.

```bash
cd mcp
V=0.3.0   # also bump: const version in main.go, Chart.yaml version/appVersion, values.yaml tag
docker build --provenance=false --sbom=false -t ghcr.io/cploutarchou/k3s-infra-mcp:$V .
docker save ghcr.io/cploutarchou/k3s-infra-mcp:$V -o /var/tmp/k3s-infra-mcp-$V.tar
tar -xOf /var/tmp/k3s-infra-mcp-$V.tar index.json | jq -r '.manifests[0].digest'   # → helmrelease.yaml image.digest
cd ../ansible
ansible-playbook playbooks/30-sideload-image.yml \
  -e sideload_tar=/var/tmp/k3s-infra-mcp-$V.tar \
  -e sideload_image=ghcr.io/cploutarchou/k3s-infra-mcp:$V
```

The playbook prints the digest each node registered; it must equal the
pinned one before the HelmRelease change is merged, otherwise the pods
sit in `ErrImagePull` (`imagePullPolicy: IfNotPresent`, no pull secret).

## Emergency stop and restart

Drift detection is on: a bare `kubectl scale` is undone within the 30 min
reconcile interval. Use git, or suspend first.

- **Through git (preferred):** set `replicaCount: 0` in the HelmRelease,
  merge, optionally `flux reconcile hr mcp-server -n mcp` to hurry. Revert
  to restart.
- **Immediate:** `flux suspend hr mcp-server -n mcp` then
  `kubectl -n mcp scale deploy mcp-server --replicas=0`. Restart with
  `flux resume hr mcp-server -n mcp`; drift detection restores 2 replicas.

## Enable `propose_change`

The live secret has only `api-key`, so the tool is inert. To enable it,
create a fine-grained GitHub PAT scoped to `cploutarchou/k3s-infra` with
*Contents: read and write* and *Pull requests: read and write* only, add
it as `github-token` next to `api-key` (same `sops set` pattern), bump the
rotation stamp, and merge. Rotate it the same way; revoke it on GitHub
first if it is ever suspected leaked.
