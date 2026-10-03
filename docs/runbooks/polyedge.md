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

## MCP connector (API tokens)

The app serves the Model Context Protocol at `https://polyedge.cpdevlab.com/mcp`
(app repo `docs/API.md` "MCP endpoint", `docs/OPERATIONS.md` §8) so an AI
agent can read the platform and run the operator actions the console
offers, as one user, under that user's role and audit trail. Nothing on
the cluster side: same host, same ingress, same Cloudflare allow-list.

1. In the console (`/settings/users`, ADMIN) create a dedicated user for
   the agent with the role it should have (`RESEARCHER` reads and runs
   backtests, `TRADER` also changes strategies and starts or stops the
   paper account, `RISK_MANAGER` also changes limits and resets breakers).
   Never `ADMIN`: the app refuses tokens for admins.
2. Issue a token under "API tokens" on the same page (shown once). If the
   console is unavailable, from the CLI (operator only, a cluster write):
   `kubectl -n polyedge exec deploy/polyedge -- polyedged token create --email EMAIL --name NAME`
3. Add a custom connector in the agent's client with the URL above and the
   token as its API key. The handshake works before the key is set; tool
   calls do not.
4. Rotate by issuing a new token and revoking the old one in the console;
   revocation is immediate. Disabling the user or changing its role revokes
   its tokens.

The agent can never release the emergency stop, activate live trading,
manage users, credentials or tokens, or promote an experiment, whatever
the role.

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
- R2 token (dataset archiver): create the new token, edit
  `polyedge-r2-credentials.sops.yaml` with `sops`, and revoke the old token
  after the next run's summary shows `errors=0`. Each run starts a new
  pod, so nothing needs a restart.

## Emergency stop

Use the always-visible stop in the UI. Without the UI (operator only, a
cluster write):

```bash
kubectl -n polyedge exec deploy/polyedge -- polyedged estop on --reason "..."
```

## Datasets and archiving

About 1.6-2.1 GB a day. local-path does not enforce the 100Gi request: the
datasets share k3s-03's root filesystem with the CNPG replica postgres-2,
and at about 2 GB a day it reaches the kubelet's image-GC threshold (85%
used) around mid-December 2026 and hard eviction (5% free) around the turn
of the year. Watch k3s-03's root filesystem in Grafana
(`node_filesystem_avail_bytes`, node-exporter); the `kubelet_volume_stats_*`
series report the whole node filesystem for local-path volumes, not the
datasets.

The `polyedge-dataset-archive` CronJob (`dataset-archive.yaml`, logic in
`dataset-archive.sh`) copies datasets to R2 bucket `polyedge-datasets`
(default jurisdiction, location hint ENAM) at minute 17 of every hour:

- every closed segment, at the first run after it closes (the app rotates
  segments hourly), so the archive trails recording by up to about 2 h;
- a dataset's `manifest.json` once the dataset is finished (its manifest
  unchanged for 3 h; a new dataset starts with every pod start) and every
  segment of it is archived. An archived `manifest.json` marks a complete
  dataset;
- until then, a copy of the dataset's current manifest as
  `manifest.partial.json`, so an unfinished dataset can still be restored
  and checked (see Restore below).

Before uploading a segment it checks the SHA-256 against the manifest and
refuses on a mismatch; each upload is a single-part PUT with Content-MD5,
verified by R2 and again by rclone. A segment that was never closed (a
crash) is archived as found and logged `unverified`. It never deletes
anything, locally or in the bucket: **pruning local copies is not set up
yet**, so k3s-03's disk keeps filling (see above). A run that finds no
datasets at all fails as an error (empty or unmounted volume). The datasets
volume, like every data volume here, carries
`kustomize.toolkit.fluxcd.io/prune: disabled`, so removing it from Git
never deletes the data.

Credentials: `polyedge-r2-credentials.sops.yaml`, an R2 token scoped to the
bucket (Object Read & Write) under rclone's key names. Create it from the
repository root with the helper, which reads each value silently; never
paste a value anywhere:

```sh
./scripts/sops-new-secret.sh clusters/prod/apps/polyedge/polyedge-r2-credentials.sops.yaml \
  polyedge polyedge-r2-credentials \
  RCLONE_CONFIG_R2_ACCESS_KEY_ID RCLONE_CONFIG_R2_SECRET_ACCESS_KEY 'HEARTBEAT_URL?'
```

Other key names (such as the postgres backup secret's `ACCESS_KEY_ID`)
leave rclone anonymous, and every run fails at `cannot list`. The helper
refuses an existing file: rotate the token, or add `HEARTBEAT_URL` later,
with `sops clusters/prod/apps/polyedge/polyedge-r2-credentials.sops.yaml`.
`kustomization.yaml` lists the file, so it has to be committed together
with the CronJob: without it the whole `apps` Kustomization stops building
(`./scripts/validate.sh` fails first).

- **Did it run?** `kubectl -n polyedge get jobs -l app.kubernetes.io/name=polyedge-dataset-archive`,
  then `kubectl -n polyedge logs job/<name>`: the last line is
  `summary datasets=… uploaded=… partials=… errors=…`. A failed Job is
  kept for up to 7 days, but only the last three failed Jobs are kept, so
  after a few hours of failures the first failure's logs are gone.
- **Alerting:** set `HEARTBEAT_URL` in the secret to an Uptime Kuma push
  monitor (`http://uptime-kuma.monitoring.svc/api/push/<token>`, heartbeat
  interval 2 h). Only a clean run pushes, so a failing or missing archiver
  alerts. Without it, nothing alerts.
- **`ERROR … differs from the manifest`:** the local segment no longer
  matches the hash it was closed with (disk corruption). It is not
  uploaded; any earlier archived copy is the good one. Investigate the
  node's disk before anything else.
- **`ERROR … archived copy is N bytes … left as is`:** an unverified
  segment differs from its archived copy and nothing says which is right.
  Compare both by hand; delete the wrong object from the bucket only after
  that.
- **Restore:** copy `<dataset-id>/` from the bucket into
  `/var/lib/polyedge/datasets/` (or into `$POLYEDGE_DATA_DIR/datasets/` on
  a workstation) and run `polyedged dataset verify <dataset-id>`, then
  backtest as usual. If the bucket holds `manifest.partial.json` but no
  `manifest.json` (the dataset was still recording, or finished less than
  about 4 h before), rename it to `manifest.json` first. Every closed
  segment it lists is still checked against its SHA-256 (`checksums
  verified` in the output); the newest segment or two were not archived
  yet. If both files are there, use `manifest.json`. Verify reads the
  segments that are present and skips missing ones silently, so compare
  `segments` in its output with the segments the manifest lists.

## Cloudflare ranges

`ingress.yaml` lists Cloudflare's published ranges (retrieved 2026-10-01).
If Cloudflare adds a range, requests through it get 403 from Traefik.
Compare with `curl -s https://www.cloudflare.com/ips-v4` and `ips-v6` and
update the middleware.
