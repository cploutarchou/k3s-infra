# polyedge (polyedge.cpdevlab.com)

Research, paper-trading and risk platform for Polymarket's BTC 15-minute
markets (github.com/cploutarchou/polyedge). Paper trading only: no live
venue exists in this build and `LIVE_ALLOWED` is false. Manifests:
`clusters/prod/apps/polyedge/`. Application docs: `docs/OPERATIONS.md` and
`docs/RUNBOOK.md` in the app repo.

| Piece | Where |
| --- | --- |
| `polyedged serve` (API, UI, WebSocket on :8080; metrics on :9090) | `deployment.yaml`, image `ghcr.io/cploutarchou/polyedge` |
| Migrations (`polyedged migrate`) | initContainer on the same pod, same image |
| Database | `polyedge` on the shared CNPG cluster, direct to `postgres-rw` (session advisory locks; no pooler), `pool_max_conns=8`, `sslmode=require` |
| Datasets | `polyedge-datasets` PVC (100Gi local-path) at `/var/lib/polyedge` |
| Secrets | `polyedge-secret.sops.yaml` (DATABASE_URL, MASTER_KEY, BOOTSTRAP_ADMIN_*), `polyedge-db-credentials.sops.yaml` (databases ns), `ghcr-pull-secret.sops.yaml` |
| Settings | `config.yaml` |
| DNS / TLS | external-dns from the Ingress (proxied), cert-manager DNS-01 |
| Edge | Traefik `cloudflare-only` middleware: only Cloudflare's ranges reach the app |
| Metrics | VictoriaMetrics job `polyedge` (pod port `metrics`); Grafana → k3s Applications, application = `polyedge` |

## Shape and why

- One replica, `Recreate`: `serve` is a single writer (engine, dataset
  recorder, backtest recovery at start). Never scale it. No PDB: on one
  replica it would block drains.
- Readiness and liveness probe `/health/live`, not `/health/ready`.
  `/health/ready` fails while the database is down (the UI and its
  emergency stop keep working without it) and while the engine waits on
  a full dataset queue (a restart would lose the queued inputs). Watch it
  from Uptime Kuma instead: HTTP monitor on
  `http://polyedge.polyedge.svc.cluster.local/health/ready` (Uptime Kuma
  runs in `monitoring`, which the NetworkPolicy admits). Add it by hand;
  its config is not in git.
- The datasets PVC pins the pod to one node (local-path). Losing that node
  stops polyedge until the node returns; positions and orders are in
  PostgreSQL and survive.
- NetworkPolicies: ingress only from `traefik` (UI/API) and `monitoring`
  (metrics, health); egress only to DNS, the CNPG pods on 5432 and TCP 443
  to public addresses (market-data feeds).

## Admin access

The first admin was bootstrapped on 2026-10-02 and the
`POLYEDGE_BOOTSTRAP_ADMIN_*` keys were then removed from the secret. Manage
users and passwords in the UI at `/settings/users` ("Reset password…",
ADMIN only); a password change ends that user's sessions.

The app creates a bootstrap admin only while the users table is empty, so
this matters only after the database is rebuilt. Then either:

- add both keys back (`sops clusters/prod/apps/polyedge/polyedge-secret.sops.yaml`;
  the password needs at least 12 characters), bump `polyedge/secret-revision`
  in `deployment.yaml`, log in, and remove the keys again with
  `sops unset ... '["stringData"]["POLYEDGE_BOOTSTRAP_ADMIN_EMAIL"]'` and
  `'["stringData"]["POLYEDGE_BOOTSTRAP_ADMIN_PASSWORD"]'` plus another bump; or
- create one from the CLI (operator only, a cluster write), password on stdin:
  `kubectl -n polyedge exec -i deploy/polyedge -- polyedged user create --email EMAIL --role ADMIN`

## Releasing a new version

The app repo's `Release image` workflow runs after CI passes on `master`
and prints `ghcr.io/cploutarchou/polyedge:sha-<commit>@sha256:<digest>` in
its job summary. Put that reference in both `image:` fields of
`deployment.yaml` (initContainer and container) in one PR. The pod is
recreated: the API is unavailable for the restart (seconds), working
simulated orders are cancelled with reason `process_restart`, and a new
dataset starts.

## Rotating secrets

- Database password: edit both `polyedge-db-credentials.sops.yaml` and the
  password inside `POLYEDGE_DATABASE_URL` in `polyedge-secret.sops.yaml`,
  then bump `polyedge/secret-revision`.
- Master key: follow the app's key-rotation procedure
  (`POLYEDGE_MASTER_KEY_PREVIOUS`); never just replace it, or vault records
  become unreadable.

## Emergency stop

Use the always-visible stop in the UI. Without the UI (operator only, a
cluster write):

```bash
kubectl -n polyedge exec deploy/polyedge -- polyedged estop on --reason "..."
```

## Datasets and archiving

About 2.1 GB a day (measured 2026-10-01), so 100Gi lasts roughly 45 days.
Archive bucket: R2 `polyedge-datasets` (default jurisdiction). The
archiver CronJob is not deployed yet: it needs an R2 API token scoped to
that bucket (Object Read & Write), created in the Cloudflare dashboard and
added as a SOPS secret. Until then watch the volume in Grafana
(`kubelet_volume_stats_used_bytes`, namespace `polyedge`).

## Cloudflare ranges

`ingress.yaml` lists Cloudflare's published ranges (retrieved 2026-10-01).
If Cloudflare adds a range, requests through it get 403 from Traefik.
Compare with `curl -s https://www.cloudflare.com/ips-v4` and `ips-v6` and
update the middleware.
