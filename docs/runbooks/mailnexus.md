# mailnexus

MailNexus: self-hosted Plunk 0.14.0 (transactional email on Amazon SES) with
ntfy, PostgreSQL 16 and Redis, in namespace `mailnexus`
(`clusters/prod/apps/mailnexus/`). Hosts: `api.cpdevlab.com` (API),
`app.cpdevlab.com` (dashboard, admin IPs only), `docs.cpdevlab.com`.
executionlab-staging sends its mail through this Plunk, so a Plunk outage
delays its mail too.

## Shape

- Every workload is a single replica pinned to the node labelled
  `mailnexus.io/data=true` (k3s-01), because its volumes are local-path:
  `postgres-data`, `plunk-data`, `redis-data`. Flux never prunes the
  namespace or these volumes (`kustomize.toolkit.fluxcd.io/prune: disabled`).
- The node label is not managed by this repository. A rebuilt or replaced
  node needs it again:
  `kubectl label node <node> mailnexus.io/data=true` (operator).
- Plunk reads `mailnexus-config` and `mailnexus-secrets` only at startup.
  After changing either, bump `mailnexus.io/config-revision` in `plunk.yaml`;
  the rollout restarts Plunk (Recreate: the API and dashboard are down for
  1-4 minutes).
- Secrets: `mailnexus-secrets.sops.yaml` (key list in the `.example`).
  Change one value with
  `printf '%s' "$v" | jq -Rs . | sops set --value-stdin clusters/prod/apps/mailnexus/mailnexus-secrets.sops.yaml '["stringData"]["<KEY>"]'`.
- Network policies: ingress is denied by default; Traefik reaches Plunk, only
  Plunk reaches the datastores, and the backup job reaches PostgreSQL. The
  cluster enforces them.

## Dashboard allowlist (managed by hand)

`app.cpdevlab.com` goes through the Traefik middleware
`mailnexus/dashboard-allowlist` (`ipAllowList.sourceRange`). It is not in
Git because it holds the admin IP and this repository is public; Flux
neither applies nor prunes it. Change it with
`kubectl -n mailnexus edit middleware.traefik.io dashboard-allowlist`
(operator), and keep a copy in the private mailnexus repository.

## Email images (R2)

Images uploaded in the dashboard's email editor go to the R2 bucket
`mailnexus-uploads` (default jurisdiction) and are served publicly from its
custom domain `https://uploads.cpdevlab.com/<project>/<file>`. Email
attachments are not stored there; they live in PostgreSQL.

- `S3_PUBLIC_URL` is written into every template and every sent email at
  upload time. Never change it, and never remove the custom domain.
- Credentials: `S3_ACCESS_KEY_ID` / `S3_ACCESS_KEY_SECRET` in
  `mailnexus-secrets`, an R2 token with Object Read & Write on
  `mailnexus-uploads` only.
- Expected log lines on every start: `[S3] Failed to set bucket policy`
  (R2 does not implement PutBucketPolicy; the bucket is public through the
  custom domain instead) and possibly `[S3] Failed to initialize bucket`
  (the token cannot check buckets). Neither is an outage. Do not give Plunk
  an admin token to silence them.
- Smoke test after any change: upload an image in a template, open the
  returned URL in a private window, and send a test email that shows it.
- MinIO, which held these images before 2026-10, is retired; the images it
  would have served were never reachable from outside the cluster.

## PostgreSQL backups

- **What:** `mailnexus-postgres-backup` CronJob, daily 03:45 UTC (clear of
  the CNPG base backup at 03:30). `pg_dump -Fc` of database `plunk`,
  encrypted with `openssl enc -aes-256-cbc -pbkdf2 -iter 10000 -md sha256
  -salt` and `BACKUP_PASSPHRASE`: the same artifact as the mailnexus repo's
  manual `scripts/k8s-backup.sh`, so its `scripts/k8s-restore.sh` restores
  either. Before upload the dump is decrypted in a pipe and read back in
  full, and its table count must equal the live database's.
- **Where:** R2 bucket `mailnexus-backups` (EU jurisdiction), key
  `postgres/YYYY/MM/DD/<stamp>/{postgres.dump.enc,manifest.txt}`. A
  directory with a `manifest.txt` is complete; the manifest holds the
  dump's size, SHA-256, MD5, table count and the server and pg_dump
  versions. Each upload is checked against the stored object's size, MD5
  and SHA-256.
- **Retention:** the bucket's lifecycle rule expires `postgres/` after 35
  days, and a bucket lock keeps `postgres/` from being deleted or
  overwritten for 14 days. The job deletes nothing.
- **Passphrase:** `BACKUP_PASSPHRASE` in `mailnexus-postgres-backup` is the
  only key to every backup. Keep it in the password manager as well; if it
  is lost together with the age key, no backup can be restored. Rotating it
  orphans older backups until they expire.
- **Did it run?** `kubectl -n mailnexus get jobs -l app.kubernetes.io/name=mailnexus-postgres-backup`,
  then `kubectl -n mailnexus logs job/<name> -c dump` and `-c upload`: the
  last upload line is `summary key=… size=… md5=… sha256=… verified=yes`.
  Run one now (operator):
  `kubectl -n mailnexus create job --from=cronjob/mailnexus-postgres-backup mailnexus-postgres-backup-manual-$(date +%s)`.
- **Alerting:** `HEARTBEAT_URL` in `mailnexus-postgres-backup` is an Uptime
  Kuma push monitor (heartbeat 25 h). Only a run whose upload was read back
  and matched pushes. Without it, nothing alerts.
- **Restore test:** `mailnexus-postgres-restore-test`, Sundays 05:15 UTC,
  with a read-only token (`mailnexus-restore-test`). It restores the newest
  backup into a throwaway PostgreSQL inside its pod (never the live
  database) and checks it: 19+ tables, applied migrations, at least one
  user and project, newest backup under 30 h old. Its own push monitor has
  an 8-day interval.

## Restore the database from R2

Destructive: it replaces the live `plunk` database. Restore into a scratch
environment first when investigating. Needs a workstation with cluster
access, `rclone`, OpenSSL 1.1.1 or later, the mailnexus repo, the
`BACKUP_PASSPHRASE`, and an R2 token with read access to
`mailnexus-backups`. Avoid 03:40-04:15 UTC.

1. Configure a read-only remote for this shell (values read silently):

   ```sh
   export RCLONE_CONFIG_R2MNX_TYPE=s3 RCLONE_CONFIG_R2MNX_PROVIDER=Cloudflare \
     RCLONE_CONFIG_R2MNX_REGION=auto RCLONE_CONFIG_R2MNX_NO_CHECK_BUCKET=true \
     RCLONE_CONFIG_R2MNX_ENDPOINT=https://035087c37abeda0a744ab1c4c482d19f.eu.r2.cloudflarestorage.com
   read -rs RCLONE_CONFIG_R2MNX_ACCESS_KEY_ID && export RCLONE_CONFIG_R2MNX_ACCESS_KEY_ID
   read -rs RCLONE_CONFIG_R2MNX_SECRET_ACCESS_KEY && export RCLONE_CONFIG_R2MNX_SECRET_ACCESS_KEY
   ```

2. List the newest complete backups and pick one:

   ```sh
   rclone lsf -R --files-only --include '/*/*/*/*/manifest.txt' r2mnx:mailnexus-backups/postgres | sort | tail -n 7
   ```

3. Copy it into the layout the restore script expects:

   ```sh
   cd ~/workspace/mailnexus
   KEY=2026/10/04/20261004T034500Z   # from step 2, without /manifest.txt
   STAMP=${KEY##*/}
   rclone copy "r2mnx:mailnexus-backups/postgres/$KEY" "backups/$STAMP"
   ```

4. Check it before decrypting; the two hashes must match:

   ```sh
   grep -E '^(created|server_version|tables|size|sha256)=' "backups/$STAMP/manifest.txt"
   sha256sum "backups/$STAMP/postgres.dump.enc"
   ```

5. Suspend Flux for the restore, or a reconcile scales Plunk back up while
   its database is being replaced: `flux suspend kustomization apps`.

6. Restore. The script scales Plunk to 0, drops and recreates `plunk`, runs
   `pg_restore --no-owner` and scales Plunk back to 1. Pass the directory as
   `SRC=`:

   ```sh
   read -rs BACKUP_PASSPHRASE && export BACKUP_PASSPHRASE   # skip if it is in .env
   make k8s-restore SRC="backups/$STAMP"
   ```

   `bad decrypt` means a wrong passphrase; the script stops before touching
   the cluster.

7. `flux resume kustomization apps`, then check `kubectl -n mailnexus get
   pods`, log in at `https://app.cpdevlab.com` and check projects and
   contacts. Delete `backups/$STAMP` from the workstation afterwards.

**k3s-01 lost:** the local-path volumes are gone with it, but their claims
stay bound to volumes pinned to the dead node, so the pods stay Pending.
Label another node `mailnexus.io/data=true`, then delete the claims
`postgres-data`, `plunk-data` and `redis-data` (operator; their data is
already lost). Flux recreates them, empty, on the labelled node, and the
workloads start there. Restore the newest backup with the steps above, and
recreate the hand-managed `dashboard-allowlist` middleware from the private
mailnexus repository. Email images are unaffected (they are in R2).
