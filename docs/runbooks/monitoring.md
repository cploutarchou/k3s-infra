# Monitoring stack and dashboards

Everything lives in `clusters/prod/apps/monitoring/`.

| Component | What it does |
| --- | --- |
| VictoriaMetrics single (`vmsingle:8428`) | TSDB + scraper, 30d retention, 10Gi local-path on the node it runs on |
| node-exporter (hostNetwork :9100), kube-state-metrics | node and object state |
| Grafana (grafana.cpdevlab.com) | stateless; datasource and dashboards are provisioned, nothing is saved in the pod |
| Uptime Kuma (status.cpdevlab.com) | external HTTP checks |

## Scrape jobs

`victoria-metrics`, `node-exporter`, `kube-state-metrics`, `kubelet`, `cadvisor`,
`apiserver`, `etcd` (:2381 on the vLAN), `cnpg` (:9187), `traefik` (:9100 pod
port), `flux` (controllers :8080), `cert-manager` (:9402), `external-dns` (:7979).

k3s is one binary, so the `apiserver` and `etcd` jobs also return kubelet and
cadvisor series. Always filter with `job="kubelet"`, `job="cadvisor"`,
`job="etcd"` or `job="apiserver"` to avoid double counting.

Flux object state (`gotk_resource_info`) comes from kube-state-metrics
custom-resource-state, configured in `kube-state-metrics.yaml`.

Backup metrics: the CNPG cluster uses the Barman Cloud plugin, so
`barman_cloud_cloudnative_pg_io_*` carry the real backup timestamps. The legacy
`cnpg_collector_*backup*` gauges stay at 0 and must not be used.

## Dashboards

Community dashboards are downloaded from grafana.com at pod start (folder
General): Node Exporter Full, Kubernetes Views Global, etcd, CloudNativePG,
Traefik.

Custom dashboards (folder `k3s-infra`) are generated:

```sh
python3 scripts/gen-dashboards.py     # writes clusters/prod/apps/monitoring/dashboards/*.json
./scripts/validate.sh
```

The JSON files become the `grafana-dashboards-custom` ConfigMap through the
configMapGenerator in `kustomization.yaml`, mounted by the Grafana chart via
`dashboardsConfigMaps`. Edit the generator, not the JSON.

| Dashboard | Covers |
| --- | --- |
| k3s Cluster Overview | HA posture (nodes, etcd leader, CNPG primary/replicas, scrape targets down), 2/3 capacity rule with headroom after losing a node, per-node CPU/mem/disk/net/conntrack, etcd fsync/RTT/db size, API server, PVC usage, VictoriaMetrics self-health |
| k3s Platform | Traefik edge traffic/latency/5xx by service, cert-manager and Traefik-served cert expiry, Flux Kustomization/HelmRelease/source state and reconcile timing, external-dns sync and errors |
| k3s Databases | CNPG topology, replication lag, WAL archiving, base backups and recovery window, per-database connections/size/transactions/cache hit/xid age, instance resources |
| k3s Applications | One templated view per namespace (website, gvasiliourolex, mcp, monitoring): HTTP via Traefik service metrics, pods vs requests/limits, CronJobs and Jobs, database and PVC |

Grafana has no persistence: a dashboard edited in the UI is lost on the next
pod restart. Change the generator and open a PR instead.

## Known gaps

- VictoriaMetrics cannot scrape host endpoints (node-exporter, kubelet,
  cadvisor, etcd) on the node it runs on: pod traffic to the node's own vLAN
  IP arrives on `cni0` with a pod source address, and the nftables input chain
  only accepts `iifname eth1 ip saddr 10.0.0.0/24`. Fix belongs in
  `ansible/roles/nftables` and is a firewall change (needs operator sign-off).
- The apps expose no `/metrics`; HTTP panels use Traefik's service metrics.
- k3s etcd snapshot uploads to R2 are not exposed as metrics; check with
  `k3s etcd-snapshot ls` on a node.
