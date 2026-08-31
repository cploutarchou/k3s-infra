# Decisions

Short log of choices that aren't obvious from the manifests.

- **k3s v1.34.11+k3s1 from day 0** — ships etcd v3.6.14 (verified in the
  release notes). Bootstrapping straight onto 1.34 avoids the etcd
  3.5 → 3.6 zombie-member upgrade hazard that clusters upgrading in place
  have to navigate.
- **etcd on all three nodes** — 3×server embedded etcd gives quorum 2/3
  with the smallest footprint; netcup nodes are homogeneous, no reason for
  dedicated workers at this scale.
- **All cluster traffic on the vLAN** — etcd and the API never traverse
  public interfaces; nftables can stay simple (public = 22/80/443 only).
  MTU 1400 to fit the vLAN overhead.
- **kube-vip for the API VIP only** (`svc_enable false`) — ingress doesn't
  need a VIP: Traefik hostPorts + Cloudflare proxy over three A records
  already survive a node loss (Cloudflare retries/health-checks origins).
- **Traefik DaemonSet + hostPorts instead of servicelb/LoadBalancer** —
  no cloud LB exists; hostPorts keep the path short and identical per node.
- **DNS-01 (not HTTP-01)** — records are Cloudflare-proxied, HTTP-01
  origin checks are unreliable behind the proxy; DNS-01 also allows
  wildcard certs.
- **CNPG backups via the Barman Cloud Plugin** — operator 1.30 with
  `-standard-` images has no barman binaries in-pod, so in-tree
  `spec.backup.barmanObjectStore` fails at WAL archive time. The
  `plugin-barman-cloud` HelmRelease + an `ObjectStore` CR + the cluster's
  `plugins:` stanza are the supported path; ScheduledBackups use
  `method: plugin`.
- **external-dns needs explicit target annotations** — with Traefik's
  publishedService disabled (hostPort ingress), ingresses carry no status
  address and external-dns generates no endpoints for them;
  `--default-targets` never kicks in. Every public ingress carries
  `external-dns.alpha.kubernetes.io/target: <the three node IPs>`.
- **Apex and www DNS migrated to external-dns (2026-08-31)** — the
  pre-existing manual records (apex A, unproxied → 159.195.82.201; www
  CNAME → apex) were deleted on operator instruction; both names now ride
  the Uptime Kuma ingress and external-dns publishes them proxied across
  all three nodes. Restoring the old state means recreating those two
  records by hand and removing the hosts from the kuma ingress.
- **external-dns default-targets = node public IPs** — Traefik has no
  LoadBalancer status to publish; explicit targets keep records correct.
- **R2 EU endpoint** — bucket jurisdiction is EU, so the S3 endpoint is
  `https://<account>.eu.r2.cloudflarestorage.com` (the plain
  `<account>.r2.cloudflarestorage.com` endpoint does not serve EU-pinned
  buckets).
- **Postgres 17.x, not 18.x** — the shared cluster is pinned to the latest
  17.x standard image (digest-pinned). 17 is a mature release series with
  several minor releases of hardening behind it; 18 is still early in its
  bugfix cycle, and a shared cluster serving every app is the wrong place
  to absorb new-major surprises. Major upgrades are deliberate, tested
  events (CNPG supports in-place major upgrades), not a side effect of
  image bumps.
- **One shared CNPG cluster** — 8 GB nodes can't afford per-app postgres;
  one cluster with 1 GB limit, one database per app, WAL-archived to R2.
- **MCP image is side-loaded, registry push pending (2026-08-31)** — the
  operator's fine-grained PAT has no packages permission, so
  `ghcr.io/cploutarchou/k3s-infra-mcp` (0.2.0, digest `sha256:5c9d6078…`)
  is imported into containerd on all three nodes (tag + digest refs) via
  `ansible/playbooks/30-sideload-image.yml` instead of being pushed to
  GHCR. The HelmRelease pins the digest with
  `imagePullPolicy: IfNotPresent` and no pull secret, so the digest rule
  holds. Follow-up: push the image with a packages-scoped token, wire the
  private-registry pull secret (chart already supports
  `imagePullSecrets`), and remove this note. Until then a node rebuild
  needs a re-import, and image updates mean a new side-load on every node.
- **MCP writes only via PRs + flux reconcile** — the server holds
  list/get/watch RBAC plus `patch` on Flux kinds only; it cannot apply or
  delete anything. Cluster changes stay reviewable in git.
- **/tmp tmpfs capped at 1 GiB, k3s TMPDIR on NVMe** — Debian 13 defaults
  /tmp to RAM; unbounded it competes with workloads on 8 GB nodes.
