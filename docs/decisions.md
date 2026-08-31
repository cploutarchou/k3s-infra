# Decisions

Short log of choices that aren't obvious from the manifests.

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
- **external-dns default-targets = node public IPs** — Traefik has no
  LoadBalancer status to publish; explicit targets keep records correct.
- **R2 EU endpoint** — bucket jurisdiction is EU, so the S3 endpoint is
  `https://<account>.eu.r2.cloudflarestorage.com` (the plain
  `<account>.r2.cloudflarestorage.com` endpoint does not serve EU-pinned
  buckets).
- **One shared CNPG cluster** — 8 GB nodes can't afford per-app postgres;
  one cluster with 1 GB limit, one database per app, WAL-archived to R2.
- **MCP writes only via PRs + flux reconcile** — the server holds
  list/get/watch RBAC plus `patch` on Flux kinds only; it cannot apply or
  delete anything. Cluster changes stay reviewable in git.
- **/tmp tmpfs capped at 1 GiB, k3s TMPDIR on NVMe** — Debian 13 defaults
  /tmp to RAM; unbounded it competes with workloads on 8 GB nodes.
