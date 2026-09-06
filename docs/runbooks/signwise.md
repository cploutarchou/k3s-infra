# signwise (signwise.cpdevlab.com)

Next.js 16 website + CMS for Signwise Outdoor Advertising
(github.com/cploutarchou/signwise). Manifests: `clusters/prod/apps/signwise/`.

| Piece | Where |
| --- | --- |
| Web (standalone `server.js`, :3000) | `web-deployment.yaml`, image `ghcr.io/cploutarchou/signwise` |
| Migrations (`pnpm db:migrate`) | initContainer on the web pod, image `ghcr.io/cploutarchou/signwise-tools` |
| Seed (`pnpm db:seed`) | `seed-job.yaml`, same tools image; bump the Job suffix to re-run |
| Database | `signwise` on the shared CNPG cluster; role + password in `signwise-db-credentials.sops.yaml` (databases ns) |
| Uploads | `signwise-uploads` PVC (5Gi local-path) at `/app/uploads`, `MEDIA_STORAGE=local` |
| Secrets | `signwise-secret.sops.yaml` (DATABASE_URL, AUTH_SECRET, SECURITY_SALT), `signwise-seed-secret.sops.yaml` (SEED_ADMIN_*) |
| DNS / TLS | external-dns from the Ingress (proxied), cert-manager DNS-01 |
| Dashboards | Grafana → k3s-infra → k3s Applications, application = `signwise` |

## Images are side-loaded

Same interim path as the other apps (docs/decisions.md). `NEXT_PUBLIC_SITE_URL`
is a build argument, so a hostname change is a rebuild.

```bash
cd ../signwise
R=ghcr.io/cploutarchou; V=1.0.0
docker build --provenance=false --sbom=false --target runner \
  --build-arg NEXT_PUBLIC_SITE_URL=https://signwise.cpdevlab.com -t $R/signwise:$V .
docker build --provenance=false --sbom=false --target tools \
  --build-arg NEXT_PUBLIC_SITE_URL=https://signwise.cpdevlab.com -t $R/signwise-tools:$V .
docker save $R/signwise:$V $R/signwise-tools:$V -o /var/tmp/signwise-bundle.tar

cd ../k3s-infra/ansible
ansible-playbook playbooks/31-sideload-image-bundle.yml \
  -e sideload_tar=/var/tmp/signwise-bundle.tar \
  -e '{"sideload_images":["'$R'/signwise:'$V'","'$R'/signwise-tools:'$V'"]}'
```

Pin the digests the playbook reports in `web-deployment.yaml` and
`seed-job.yaml`, then commit.

## Admin account

Created once by the seed from `signwise-seed` (only when no user exists).
Read the login with `sops -d clusters/prod/apps/signwise/signwise-seed-secret.sops.yaml`
and sign in at `/admin/login`. To reset later, run the tools image with
`args: ["admin:create", "--", "--email", ..., "--password", ...]` as a Job.

## Open items

- `EMAIL_PROVIDER=console`: enquiries are stored in the database and logged,
  not emailed. Switch to `resend` by adding `RESEND_API_KEY` to the signwise
  secret and setting `EMAIL_PROVIDER=resend` (cpdevlab.com is a verified
  Resend domain; `EMAIL_TO` defaults to the CMS notification email).
- Rate limiting keys on `X-Forwarded-For`, which Traefik rewrites to the
  Cloudflare edge IP (no `forwardedHeaders.trustedIPs`); the same applies to
  every app behind this Traefik.
- No Uptime Kuma monitor yet (its monitors live in the UI, not in git).
