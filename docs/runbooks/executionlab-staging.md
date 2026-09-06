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

## Open items

- **The apex returns HTTP 526.** `executionlab.io` has three proxied A
  records pointing at the nodes, but no Ingress serves that host, so Traefik
  answers with its default self-signed certificate and Cloudflare rejects it.
  This predates the staging deployment. Either give the apex an Ingress or
  set its records to DNS-only; leaving it is a TLS error on the root domain
  for anyone who visits.
- `app`, `crm` and `ib` each have a single A record pointing at k3s-01 only,
  while `api`, `staging` and the apex have all three. They are the app's
  production portal hosts (`Dockerfile.frontend` build args) and nothing
  serves them yet.
## SPF records lost during the initial deployment

Both apex SPF TXT records were present when the deployment started and gone
when it finished:

    v=spf1 include:_spf.mx.cloudflare.net ~all              id 1ab3c11acb02c01c3794d7ee60917e65
    v=spf1 ip4:159.195.82.201 include:executionlab.io ~all   id 738d9b4ddc2f592d5cdc4b90b603a371

The window is **2026-09-06 01:05–01:30 UTC**. Nothing here deleted them. What
the investigation established:

- **external-dns is ruled out.** Its pod had 15 h of unbroken logs covering the
  window, 10,879 lines, with zero mentions of the zone. It logs every change as
  `action=CREATE`/`DELETE`, and `executionlab.io` is absent from its
  `domainFilters`.
- **cert-manager is ruled out.** In v1.21.1 the Cloudflare solver's `CleanUp`
  calls `findTxtRecord(fqdn, content)` and deletes a single record matched on
  **both** name and content. It cannot reach a name it was not solving for.
  Its logs show only issuance for the two staging hosts.
- **Nothing else in the cluster holds the credential.** Only the external-dns
  and cert-manager secrets carry it, and they carry the *same* token value.
- **The token cannot reach anything else.** It is strictly zone-DNS-scoped:
  `/user`, `/user/tokens`, `/memberships`, `/rulesets` and the Email Routing
  endpoint all return 403.
- **The deletion was targeted, not a sweep.** Record timestamps show every
  surviving pre-existing record was last modified in May or June; nothing else
  in the zone was touched on 6 September. DKIM and DMARC are also TXT records
  and both survived. An automated fault that hit exactly the two apex SPF rows
  and nothing else is a very narrow failure mode; a person acting on
  Cloudflare's duplicate-SPF warning is not.

**Unresolved, and only the account audit log can settle it.** The zone-scoped
token cannot read it. In the dashboard: Manage Account → Audit Log, filter to
6 September 2026, 01:05–01:30 UTC, look for two `dns_record` delete entries at
the apex and read the actor.

The Cloudflare Email Routing record was recreated with its original content.
The second was deliberately not recreated: two SPF records is invalid under
RFC 7208 so neither was being honoured, and that one also included itself,
which is a resolution loop. If something genuinely needs to send mail from
159.195.82.201, merge it into the single record rather than adding a second.

### The guard

`dns-guard.yaml` runs hourly and fails if mail delivery (MX, SPF, DKIM,
DMARC) or either staging host stops resolving as expected, including if a
second SPF record reappears. Failures surface on the "k3s Applications"
dashboard under "time since last success" and "failed jobs by CronJob"; those
panels are namespace-scoped, so no dashboard change was needed. It resolves
against public recursive resolvers rather than the Cloudflare API, so it tests
what the world sees and needs no credential.

## Open items

- **The apex returns HTTP 526.** `executionlab.io` has three proxied A
  records pointing at the nodes, but no Ingress serves that host, so Traefik
  answers with its default self-signed certificate and Cloudflare rejects it.
  This predates the staging deployment. Either give the apex an Ingress or
  set its records to DNS-only; leaving it is a TLS error on the root domain
  for anyone who visits.
- `app`, `crm` and `ib` each have a single A record pointing at k3s-01 only,
  while `api`, `staging` and the apex have all three. They are the app's
  production portal hosts (`Dockerfile.frontend` build args) and nothing
  serves them yet.
## SPF records lost during this deployment — unexplained

The apex carried two SPF TXT records before this work:

    v=spf1 include:_spf.mx.cloudflare.net ~all           (Cloudflare Email Routing)
    v=spf1 ip4:159.195.82.201 include:executionlab.io ~all

Both were gone from Cloudflare and from public DNS by the end of the
deployment. No delete was issued against them: the only writes were three
record creations and three updates, all scoped to `api.staging`. The
`external-dns` logs contain no mention of the zone, which matches its
`domainFilters`. The cert-manager logs show only issuance for the two staging
hosts and no deletions.

One candidate was tested and ruled out: the shell helper used for the
Cloudflare calls expanded its JSON body as `${3:+--data "$3"}`, unquoted,
which would word-split a body containing spaces and hand curl extra
arguments it would treat as further URLs. Replayed locally, bash preserves
the inner quoting and the body stays a single argument, so each call issued
exactly one request to exactly one URL.

Circumstantially, this deployment was the first time cert-manager solved
DNS-01 in this zone (`executionlab.io` was added to the ClusterIssuer
`dnsZones` here), and its Cloudflare solver does list and delete TXT records
during challenge cleanup. That is a hypothesis, not a finding — **causation
was not established.** The zone-scoped API token cannot read the account audit
log; the Cloudflare dashboard (Manage Account → Audit Log) will name the
actor.

The Cloudflare Email Routing SPF record has been recreated with its original
content. The second one was deliberately not recreated: two SPF records is
invalid under RFC 7208 so neither was being honoured, and it also included
itself, which is a resolution loop. Recreate it only if something genuinely
needs to send mail from 159.195.82.201, and then merge it into the single
record rather than adding a second.

Watch this zone's TXT records across the next certificate renewal (roughly
60 days) to see whether they disappear again.

## Other known zone defects (not fixed here)

Cloudflare Email Routing (MX, DKIM, DMARC) is live on this zone. `external-dns`
deliberately does **not** manage `executionlab.io` — its policy is `sync` —
and both Ingresses carry `external-dns.alpha.kubernetes.io/exclude`.
