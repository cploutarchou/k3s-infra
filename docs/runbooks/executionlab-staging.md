# ExecutionLab staging

Staging deployment of `cploutarchou/dydx-trading-bot` (frontend → backend →
bot) in the `executionlab-staging` namespace.

| | |
| --- | --- |
| App | https://staging.executionlab.io |
| API | https://api.staging.executionlab.io |
| Namespace | `executionlab-staging` |
| Database | `executionlab_staging_platform` on the shared CNPG cluster |
| Manifests | `clusters/prod/apps/executionlab-staging/` |
| Grafana | "k3s Applications", application = `executionlab-staging` |

## The two hosts are not a choice

`frontend/src/api/origin.ts` allowlists API base URLs. On
`staging.executionlab.io` the only accepted base is
`https://api.staging.executionlab.io`; the production base is explicitly
rejected for the staging environment and anything else returns null. Serving
the API on a path of the frontend host, or on any other hostname, breaks the
app. The same logic means the stock frontend image needs no staging rebuild.

`api.staging.executionlab.io` is **DNS-only (grey cloud)**. Cloudflare
Universal SSL covers `executionlab.io` and `*.executionlab.io` — one label
only — so a proxied two-label host fails the TLS handshake at the edge.
Traefik serves the Let's Encrypt certificate directly instead. Re-enabling the
orange cloud breaks the API until Advanced Certificate Manager is purchased.

`staging.executionlab.io` stays proxied; one label is inside Universal SSL.

## Execution safety

Staging must not reach a live account. Three independent controls:

1. `WORKER_MODE=celery`. The entrypoint defaults to `bot`, which runs
   `bot/main.py` — the live trading runtime. Pinned to Celery, whose only
   registered tasks are `backtests.run` and `bot.sync_market_candles`.
2. `BOT_PLACE_TRADES=false` gates `open_positions()` in `bot/main.py`.
3. `IS_TESTNET=true`.

Per-instance runtimes read their own credentials and `is_testnet` flag from
the database. Do not add mainnet credentials to a bot instance here.

## One database, not two

The bot resolves its connection through `BOT_DB_CUTOVER_MODE`, which
`bot/src/infrastructure/database.py` defaults to `shared`: `BOT_DATABASE_URL`
is ignored and the bot's Alembic schema lands in the platform database next to
the backend's. Splitting them is the app's own unfinished cutover. Do not
create a second database until that flag moves to `dedicated`.

## Images are side-loaded

The GHCR repos are private and the operator PAT has no `packages` scope, so
images are imported into containerd on all three nodes and pinned by the
digest containerd assigned. **A node rebuild needs a re-import.**

```bash
SHA=$(git -C ../dydx-trading-bot rev-parse --short HEAD)
R=ghcr.io/cploutarchou/dydx-trading-bot
for s in backend frontend api worker backend-migrator; do
  docker build --provenance=false --sbom=false \
    -f docker/Dockerfile.$( [ "$s" = api ] && echo api || echo "$s" ) \
    -t "$R/$s:staging-$SHA" .
done
docker save $R/backend:staging-$SHA $R/backend-migrator:staging-$SHA \
  $R/frontend:staging-$SHA $R/api:staging-$SHA $R/worker:staging-$SHA \
  -o /tmp/bundle.tar   # ~1 GB; the five images share most layers

ansible-playbook playbooks/31-sideload-image-bundle.yml \
  -e sideload_tar=/tmp/bundle.tar \
  -e '{"sideload_images":["'"$R"'/backend:staging-'"$SHA"'", ...]}'
```

Then pin the reported digests in the manifests and commit.

## Migrations

Explicit Jobs only; `DB_AUTO_MIGRATE` stays `false`. Job names carry the app
commit, so a new revision means new Jobs. The Go migrator has its own image
because `Dockerfile.backend` builds only `./cmd/server`; Alembic already ships
in the bot image, so the bot API image doubles as the bot migrator.

## Admin account

`internal/startup/bootstrap_admin.go` provisions `admin` from
`BOOTSTRAP_ADMIN_*` in the SOPS secret on every backend start. Because the
password is set explicitly, `PasswordChangeRequired` is false — rotation is
manual. To force a new password, set `BOOTSTRAP_ADMIN_RESET_PASSWORD=true`
alongside a new `BOOTSTRAP_ADMIN_PASSWORD` and roll the backend.

## Known zone defects (not fixed here)

- `app`, `crm` and `ib` each have a single A record pointing at k3s-01 only,
  while `api`, `staging` and the apex have all three. They are the app's
  production portal hosts (`Dockerfile.frontend` build args) and nothing
  serves them yet.
- Two SPF TXT records on the apex. RFC 7208 allows one, so both are ignored by
  verifiers. One is Cloudflare Email Routing's; the other,
  `v=spf1 ip4:159.195.82.201 include:executionlab.io ~all`, also includes
  itself, which is a resolution loop.

Cloudflare Email Routing (MX, DKIM, DMARC) is live on this zone. `external-dns`
deliberately does **not** manage `executionlab.io` — its policy is `sync` —
and both Ingresses carry `external-dns.alpha.kubernetes.io/exclude`.
