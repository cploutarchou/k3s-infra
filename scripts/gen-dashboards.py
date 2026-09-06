#!/usr/bin/env python3
"""Generate the custom Grafana dashboards under
clusters/prod/apps/monitoring/dashboards/ (provisioned via ConfigMap).

Re-run after editing:  python3 scripts/gen-dashboards.py
Queries carry explicit job= filters because k3s exposes kubelet/etcd series
through the apiserver and etcd scrape jobs as well as their own.
"""
import json, os

DS = {"type": "prometheus", "uid": "victoriametrics"}
OUT = os.path.join(os.path.dirname(__file__), "..", "clusters/prod/apps/monitoring/dashboards")

def thresholds(*steps):
    return {"mode": "absolute", "steps": [{"color": c, "value": v} for c, v in steps]}

GREEN_RED = lambda red: thresholds(("green", None), ("red", red))
RED_GREEN = lambda green: thresholds(("red", None), ("green", green))

def target(expr, legend=None, **kw):
    t = {"datasource": DS, "expr": expr, "refId": chr(65 + kw.pop("i", 0))}
    if legend: t["legendFormat"] = legend
    t.update(kw); return t

def panel(ptype, title, targets, w=12, h=8, unit=None, th=None, desc=None, opts=None, fc=None, over=None, **extra):
    p = {"type": ptype, "title": title, "datasource": DS, "gridPos": {"w": w, "h": h},
         "targets": [dict(t, refId=chr(65 + i)) for i, t in enumerate(targets)],
         "fieldConfig": {"defaults": {}, "overrides": over or []}, "options": {}}
    if unit: p["fieldConfig"]["defaults"]["unit"] = unit
    if th: p["fieldConfig"]["defaults"]["thresholds"] = th
    if fc: p["fieldConfig"]["defaults"].update(fc)
    if desc: p["description"] = desc
    if ptype == "timeseries":
        p["options"] = {"legend": {"displayMode": "list", "placement": "bottom"}, "tooltip": {"mode": "multi", "sort": "desc"}}
        p["fieldConfig"]["defaults"].setdefault("custom", {"lineWidth": 1, "fillOpacity": 8, "showPoints": "never"})
    if ptype == "stat":
        p["options"] = {"reduceOptions": {"calcs": ["lastNotNull"]}, "colorMode": "value", "graphMode": "none", "textMode": "auto"}
    if ptype == "gauge":
        p["options"] = {"reduceOptions": {"calcs": ["lastNotNull"]}, "showThresholdMarkers": True}
    if ptype == "table":
        p["options"] = {"cellHeight": "sm", "footer": {"show": False}}
    if opts: p["options"].update(opts)
    p.update(extra); return p

def ts(title, targets, **kw): return panel("timeseries", title, targets, **kw)
def stat(title, expr, w=4, h=4, **kw): return panel("stat", title, [target(expr)], w=w, h=h, **kw)
def gauge(title, expr, w=6, h=6, **kw): return panel("gauge", title, [target(expr)], w=w, h=h, **kw)
def table(title, expr, exclude=(), rename=None, w=12, h=8, **kw):
    tr = [{"id": "labelsToFields", "options": {}},
          {"id": "organize", "options": {"excludeByName": {k: True for k in ("Time", "__name__", "job", "instance") + tuple(exclude)},
                                          "renameByName": rename or {}}}]
    return panel("table", title, [target(expr, format="table", instant=True)], w=w, h=h, transformations=tr, **kw)
def row(title): return {"type": "row", "title": title, "collapsed": False, "gridPos": {"w": 24, "h": 1}, "panels": []}

def layout(panels):
    """Flow panels left-to-right, wrapping at 24 columns; rows force a new line."""
    x = y = line_h = 0; pid = 1
    for p in panels:
        w, h = p["gridPos"]["w"], p["gridPos"]["h"]
        if p["type"] == "row" or x + w > 24:
            y += line_h; x = 0; line_h = 0
        p["gridPos"].update({"x": x, "y": y}); p["id"] = pid; pid += 1
        x += w; line_h = max(line_h, h)
        if p["type"] == "row": y += 1; x = 0; line_h = 0
    return panels

def dashboard(uid, title, panels, variables=(), refresh="1m", rng="now-6h"):
    return {"uid": uid, "title": title, "tags": ["k3s-infra"], "editable": True, "schemaVersion": 39,
            "timezone": "browser", "refresh": refresh, "time": {"from": rng, "to": "now"},
            "templating": {"list": list(variables)}, "panels": layout(panels)}

def var_query(name, query, regex=None, multi=True, label=None):
    v = {"name": name, "label": label or name, "type": "query", "datasource": DS, "refresh": 2,
         "query": {"query": query, "refId": name}, "includeAll": multi, "multi": multi,
         "current": {"selected": True, "text": "All", "value": "$__all"} if multi else {}, "sort": 1}
    if regex: v["regex"] = regex
    return v

PCT = "percentunit"

# --------------------------------------------------------------------------
# 1. Cluster overview / HA posture
# --------------------------------------------------------------------------
cluster = dashboard("k3s-cluster-overview", "k3s Cluster Overview", [
    row("HA posture"),
    stat("Nodes Ready", 'sum(kube_node_status_condition{condition="Ready",status="true"})', th=thresholds(("red", None), ("orange", 2), ("green", 3)),
         desc="3 expected. Quorum is 2 of 3."),
    stat("etcd members with leader", 'sum(etcd_server_has_leader{job="etcd"})', th=thresholds(("red", None), ("orange", 2), ("green", 3)),
         desc="Members currently scraped that see a leader. Below 2 the cluster is read-only."),
    stat("etcd leader changes (24h)", 'max(increase(etcd_server_leader_changes_seen_total{job="etcd"}[24h]))', th=thresholds(("green", None), ("orange", 1), ("red", 3))),
    stat("CNPG primaries", 'sum(1 - cnpg_pg_replication_in_recovery{job="cnpg"})', th=thresholds(("red", None), ("green", 1), ("red", 2)), desc="Exactly 1 expected."),
    stat("CNPG streaming replicas", 'max(cnpg_pg_replication_streaming_replicas{job="cnpg"})', th=thresholds(("red", None), ("orange", 1), ("green", 2))),
    stat("Scrape targets down", 'count(up == 0) or vector(0)', th=GREEN_RED(1)),
    stat("Pods not Running", '(sum(kube_pod_status_phase{phase=~"Pending|Unknown"} == 1) or vector(0)) + (sum(kube_pod_status_phase{phase="Failed"} == 1 unless on(namespace,pod) kube_pod_owner{owner_kind="Job"}) or vector(0))', th=GREEN_RED(1),
         desc="Pending or Unknown pods, plus Failed pods that are not Job pods. Failed Job pods show up per app under Jobs and CronJobs on the Applications dashboard."),
    stat("Container restarts (24h)", 'sum(increase(kube_pod_container_status_restarts_total[24h])) or vector(0)', th=thresholds(("green", None), ("orange", 1), ("red", 5))),
    stat("API server up", 'min(up{job="apiserver"})', th=RED_GREEN(1), fc={"mappings": [{"type": "value", "options": {"0": {"text": "DOWN"}, "1": {"text": "UP"}}}]}),
    stat("Traefik pods ready", 'sum(kube_daemonset_status_number_ready{namespace="traefik"})', th=thresholds(("red", None), ("orange", 2), ("green", 3))),
    stat("kube-vip pods ready", 'sum(kube_daemonset_status_number_ready{namespace="kube-system",daemonset="kube-vip"})', th=thresholds(("red", None), ("orange", 2), ("green", 3))),
    stat("Oldest node uptime", 'time() - max(node_boot_time_seconds{job="node-exporter"})', unit="s", th=thresholds(("green", None))),

    row("Capacity (2/3 rule: requests must stay under 66.7% so two nodes absorb the loss of one)"),
    gauge("CPU requested / allocatable", 'sum(kube_pod_container_resource_requests{resource="cpu"} * on(namespace,pod) group_left() max by(namespace,pod)(kube_pod_status_phase{phase=~"Running|Pending"} == 1)) / sum(kube_node_status_allocatable{resource="cpu"})',
          unit=PCT, th=thresholds(("green", None), ("orange", 0.55), ("red", 0.667)), fc={"min": 0, "max": 1}),
    gauge("Memory requested / allocatable", 'sum(kube_pod_container_resource_requests{resource="memory"} * on(namespace,pod) group_left() max by(namespace,pod)(kube_pod_status_phase{phase=~"Running|Pending"} == 1)) / sum(kube_node_status_allocatable{resource="memory"})',
          unit=PCT, th=thresholds(("green", None), ("orange", 0.55), ("red", 0.667)), fc={"min": 0, "max": 1}),
    stat("Memory headroom after losing one node", '(sum(kube_node_status_allocatable{resource="memory"}) - max(kube_node_status_allocatable{resource="memory"})) - sum(kube_pod_container_resource_requests{resource="memory"} * on(namespace,pod) group_left() max by(namespace,pod)(kube_pod_status_phase{phase=~"Running|Pending"} == 1))',
         unit="bytes", w=6, h=6, th=thresholds(("red", None), ("orange", 1e9), ("green", 3e9)), desc="Allocatable memory on the two remaining nodes minus all live requests."),
    stat("CPU headroom after losing one node", '(sum(kube_node_status_allocatable{resource="cpu"}) - max(kube_node_status_allocatable{resource="cpu"})) - sum(kube_pod_container_resource_requests{resource="cpu"} * on(namespace,pod) group_left() max by(namespace,pod)(kube_pod_status_phase{phase=~"Running|Pending"} == 1))',
         unit="cores", w=6, h=6, th=thresholds(("red", None), ("orange", 1), ("green", 2))),
    ts("Requests vs allocatable over time", [
        target('sum(kube_pod_container_resource_requests{resource="cpu"} * on(namespace,pod) group_left() max by(namespace,pod)(kube_pod_status_phase{phase=~"Running|Pending"} == 1)) / sum(kube_node_status_allocatable{resource="cpu"})', "cpu"),
        target('sum(kube_pod_container_resource_requests{resource="memory"} * on(namespace,pod) group_left() max by(namespace,pod)(kube_pod_status_phase{phase=~"Running|Pending"} == 1)) / sum(kube_node_status_allocatable{resource="memory"})', "memory"),
    ], unit=PCT, fc={"max": 1, "min": 0, "custom": {"lineWidth": 1, "fillOpacity": 8, "showPoints": "never", "thresholdsStyle": {"mode": "line"}}}, th=thresholds(("transparent", None), ("red", 0.667)), w=12, h=6),
    ts("Memory requests by namespace", [target('sum by(namespace)(kube_pod_container_resource_requests{resource="memory"} * on(namespace,pod) group_left() max by(namespace,pod)(kube_pod_status_phase{phase=~"Running|Pending"} == 1))', "{{namespace}}")],
       unit="bytes", w=12, h=6, fc={"custom": {"lineWidth": 1, "fillOpacity": 30, "showPoints": "never", "stacking": {"mode": "normal"}}}),
    ts("CPU requests by namespace", [target('sum by(namespace)(kube_pod_container_resource_requests{resource="cpu"} * on(namespace,pod) group_left() max by(namespace,pod)(kube_pod_status_phase{phase=~"Running|Pending"} == 1))', "{{namespace}}")],
       unit="cores", w=12, h=6, fc={"custom": {"lineWidth": 1, "fillOpacity": 30, "showPoints": "never", "stacking": {"mode": "normal"}}}),

    row("Nodes"),
    ts("CPU utilisation", [target('1 - avg by(node)(rate(node_cpu_seconds_total{job="node-exporter",mode="idle"}[5m]))', "{{node}}")], unit=PCT, fc={"min": 0, "max": 1}, w=8),
    ts("Memory used", [target('1 - node_memory_MemAvailable_bytes{job="node-exporter"} / node_memory_MemTotal_bytes{job="node-exporter"}', "{{node}}")], unit=PCT, fc={"min": 0, "max": 1}, w=8),
    ts("Root filesystem used", [target('1 - node_filesystem_avail_bytes{job="node-exporter",mountpoint="/"} / node_filesystem_size_bytes{job="node-exporter",mountpoint="/"}', "{{node}}")], unit=PCT, fc={"min": 0, "max": 1}, w=8,
       th=thresholds(("transparent", None), ("red", 0.85))),
    ts("Load (1m) per node — 4 cores each", [target('node_load1{job="node-exporter"}', "{{node}}")], w=8, th=thresholds(("transparent", None), ("red", 4)),
       fc={"custom": {"lineWidth": 1, "fillOpacity": 8, "showPoints": "never", "thresholdsStyle": {"mode": "line"}}}),
    ts("Network throughput (eth0 public, eth1 vLAN)", [
        target('sum by(node,device)(rate(node_network_receive_bytes_total{job="node-exporter",device=~"eth0|eth1"}[5m]))', "{{node}} {{device}} rx"),
        target('-sum by(node,device)(rate(node_network_transmit_bytes_total{job="node-exporter",device=~"eth0|eth1"}[5m]))', "{{node}} {{device}} tx"),
    ], unit="Bps", w=8),
    ts("Disk I/O (NVMe)", [
        target('sum by(node)(rate(node_disk_read_bytes_total{job="node-exporter",device=~"nvme.*|sd.*|vd.*"}[5m]))', "{{node}} read"),
        target('-sum by(node)(rate(node_disk_written_bytes_total{job="node-exporter",device=~"nvme.*|sd.*|vd.*"}[5m]))', "{{node}} write"),
    ], unit="Bps", w=8),
    ts("conntrack table usage", [target('node_nf_conntrack_entries{job="node-exporter"} / node_nf_conntrack_entries_limit{job="node-exporter"}', "{{node}}")], unit=PCT, fc={"min": 0, "max": 1}, w=8, h=6),
    ts("Pods running per node", [target('kubelet_running_pods{job="kubelet"}', "{{node}}")], w=8, h=6),
    ts("Pod memory working set per node (cadvisor)", [target('sum by(node)(container_memory_working_set_bytes{job="cadvisor",container!="",namespace!=""})', "{{node}}")], unit="bytes", w=8, h=6),

    row("Control plane (k3s embedded etcd + API server)"),
    ts("etcd WAL fsync p99", [target('histogram_quantile(0.99, sum by(le,instance)(rate(etcd_disk_wal_fsync_duration_seconds_bucket{job="etcd"}[5m])))', "{{instance}}")], unit="s", w=8,
       th=thresholds(("transparent", None), ("red", 0.01)), fc={"custom": {"lineWidth": 1, "fillOpacity": 8, "showPoints": "never", "thresholdsStyle": {"mode": "line"}}}, desc="Sustained >10ms means the disk is too slow for etcd."),
    ts("etcd peer round-trip p99", [target('histogram_quantile(0.99, sum by(le,instance)(rate(etcd_network_peer_round_trip_time_seconds_bucket{job="etcd"}[5m])))', "{{instance}}")], unit="s", w=8),
    ts("etcd DB size", [
        target('etcd_mvcc_db_total_size_in_bytes{job="etcd"}', "{{instance}} total"),
        target('etcd_mvcc_db_total_size_in_use_in_bytes{job="etcd"}', "{{instance}} in use"),
    ], unit="bytes", w=8),
    ts("etcd proposals", [
        target('sum by(instance)(rate(etcd_server_proposals_failed_total{job="etcd"}[5m]))', "{{instance}} failed/s"),
        target('etcd_server_proposals_pending{job="etcd"}', "{{instance}} pending"),
    ], w=8, h=6),
    ts("API server requests by code", [target('sum by(code)(rate(apiserver_request_total{job="apiserver"}[5m]))', "{{code}}")], unit="reqps", w=8, h=6),
    ts("API server p99 latency (non-watch)", [target('histogram_quantile(0.99, sum by(le,verb)(rate(apiserver_request_duration_seconds_bucket{job="apiserver",verb!~"WATCH|CONNECT"}[5m])))', "{{verb}}")], unit="s", w=8, h=6),

    row("Storage and monitoring self-health"),
    table("PVC usage", 'kubelet_volume_stats_used_bytes{job="kubelet"} / kubelet_volume_stats_capacity_bytes{job="kubelet"}', exclude=("node",),
          rename={"Value": "used %", "persistentvolumeclaim": "pvc"}, unit=PCT, w=12, h=8,
          over=[{"matcher": {"id": "byName", "options": "used %"}, "properties": [{"id": "custom.cellOptions", "value": {"type": "gauge", "mode": "basic"}}, {"id": "thresholds", "value": thresholds(("green", None), ("orange", 0.7), ("red", 0.85))}, {"id": "min", "value": 0}, {"id": "max", "value": 1}]}]),
    table("Scrape targets down", 'up == 0', rename={"Value": "up"}, w=12, h=8),
    stat("VictoriaMetrics free disk", 'min(vm_free_disk_space_bytes{job="victoria-metrics"})', unit="bytes", w=6, th=thresholds(("red", None), ("orange", 1e9), ("green", 3e9))),
    stat("VictoriaMetrics data size", 'max(vm_data_size_bytes{job="victoria-metrics"})', unit="bytes", w=6, th=thresholds(("green", None))),
    stat("Ingestion rate", 'sum(rate(vm_rows_inserted_total{job="victoria-metrics"}[5m]))', unit="rowsps", w=6, th=thresholds(("green", None))),
    stat("Scrape targets", 'sum(vm_promscrape_targets{job="victoria-metrics"})', w=6, th=thresholds(("green", None))),
])

# --------------------------------------------------------------------------
# 2. Platform: ingress, TLS, GitOps, DNS
# --------------------------------------------------------------------------
platform = dashboard("k3s-platform", "k3s Platform (Ingress, TLS, GitOps, DNS)", [
    row("Traefik edge"),
    stat("Requests/s (websecure)", 'sum(rate(traefik_entrypoint_requests_total{job="traefik",entrypoint="websecure"}[5m]))', unit="reqps", th=thresholds(("green", None))),
    stat("5xx ratio", 'sum(rate(traefik_service_requests_total{job="traefik",code=~"5.."}[5m])) / sum(rate(traefik_service_requests_total{job="traefik"}[5m]))', unit=PCT, th=thresholds(("green", None), ("orange", 0.01), ("red", 0.05))),
    stat("p95 latency (websecure)", 'histogram_quantile(0.95, sum by(le)(rate(traefik_entrypoint_request_duration_seconds_bucket{job="traefik",entrypoint="websecure"}[5m])))', unit="s", th=thresholds(("green", None), ("orange", 0.5), ("red", 2))),
    stat("Open connections", 'sum(traefik_open_connections{job="traefik"})', th=thresholds(("green", None))),
    stat("Config last reload OK", 'min(traefik_config_last_reload_success{job="traefik"})', th=RED_GREEN(1), fc={"mappings": [{"type": "value", "options": {"0": {"text": "FAILED"}, "1": {"text": "OK"}}}]}),
    stat("Traefik pods scraped", 'count(up{job="traefik"} == 1)', th=thresholds(("red", None), ("orange", 2), ("green", 3))),
    ts("Requests/s by entrypoint", [target('sum by(entrypoint)(rate(traefik_entrypoint_requests_total{job="traefik"}[5m]))', "{{entrypoint}}")], unit="reqps", w=8),
    ts("Requests/s by status class", [
        target('sum(rate(traefik_service_requests_total{job="traefik",code=~"2.."}[5m]))', "2xx"),
        target('sum(rate(traefik_service_requests_total{job="traefik",code=~"3.."}[5m]))', "3xx"),
        target('sum(rate(traefik_service_requests_total{job="traefik",code=~"4.."}[5m]))', "4xx"),
        target('sum(rate(traefik_service_requests_total{job="traefik",code=~"5.."}[5m]))', "5xx"),
    ], unit="reqps", w=8, over=[{"matcher": {"id": "byName", "options": "5xx"}, "properties": [{"id": "color", "value": {"mode": "fixed", "fixedColor": "red"}}]}]),
    ts("Latency percentiles (websecure)", [
        target('histogram_quantile(0.50, sum by(le)(rate(traefik_entrypoint_request_duration_seconds_bucket{job="traefik",entrypoint="websecure"}[5m])))', "p50"),
        target('histogram_quantile(0.95, sum by(le)(rate(traefik_entrypoint_request_duration_seconds_bucket{job="traefik",entrypoint="websecure"}[5m])))', "p95"),
        target('histogram_quantile(0.99, sum by(le)(rate(traefik_entrypoint_request_duration_seconds_bucket{job="traefik",entrypoint="websecure"}[5m])))', "p99"),
    ], unit="s", w=8),
    ts("Requests/s by backend service", [target('sum by(service)(rate(traefik_service_requests_total{job="traefik"}[5m]))', "{{service}}")], unit="reqps", w=8),
    ts("5xx/s by backend service", [target('sum by(service)(rate(traefik_service_requests_total{job="traefik",code=~"5.."}[5m]))', "{{service}}")], unit="reqps", w=8),
    ts("Bandwidth (edge)", [
        target('sum(rate(traefik_entrypoint_requests_bytes_total{job="traefik"}[5m]))', "in"),
        target('-sum(rate(traefik_entrypoint_responses_bytes_total{job="traefik"}[5m]))', "out"),
    ], unit="Bps", w=8),

    row("TLS certificates (cert-manager issues, Traefik serves)"),
    stat("Certificates not Ready", 'sum(certmanager_certificate_ready_status{condition="False"}) or vector(0)', th=GREEN_RED(1), w=6),
    stat("Soonest expiry", 'min(certmanager_certificate_expiration_timestamp_seconds - time())', unit="s", th=thresholds(("red", None), ("orange", 7 * 86400), ("green", 20 * 86400)), w=6,
         desc="Let's Encrypt renews at 30 days remaining. Under 20 days means renewal is failing."),
    stat("ACME requests (1h)", 'sum(increase(certmanager_http_acme_client_request_count[1h])) or vector(0)', th=thresholds(("green", None)), w=6),
    stat("cert-manager sync errors (1h)", 'sum(increase(certmanager_controller_sync_error_count[1h])) or vector(0)', th=GREEN_RED(1), w=6),
    table("Certificate expiry (cert-manager)", 'certmanager_certificate_expiration_timestamp_seconds - time()', exclude=("issuer_group", "issuer_kind"),
          rename={"Value": "time left", "name": "certificate", "issuer_name": "issuer"}, unit="s", w=12, h=10,
          over=[{"matcher": {"id": "byName", "options": "time left"}, "properties": [{"id": "custom.cellOptions", "value": {"type": "color-text"}}, {"id": "thresholds", "value": thresholds(("red", None), ("orange", 7 * 86400), ("green", 20 * 86400))}]}]),
    table("Certificates served by Traefik", 'traefik_tls_certs_not_after{job="traefik"} - time()', exclude=("pod", "serial"),
          rename={"Value": "time left"}, unit="s", w=12, h=10,
          over=[{"matcher": {"id": "byName", "options": "time left"}, "properties": [{"id": "custom.cellOptions", "value": {"type": "color-text"}}, {"id": "thresholds", "value": thresholds(("red", None), ("orange", 7 * 86400), ("green", 20 * 86400))}]}],
          desc="What Traefik actually holds in memory. If a cert renews in the Secret but this stays old, Traefik did not pick it up."),

    row("GitOps (Flux)"),
    stat("Flux resources not Ready", 'sum(gotk_resource_info{ready="False"}) or vector(0)', th=GREEN_RED(1), w=6),
    stat("Flux resources suspended", 'sum(gotk_resource_info{suspended="true"}) or vector(0)', th=thresholds(("green", None), ("orange", 1)), w=6),
    stat("Reconcile errors/min (all controllers)", 'sum(rate(controller_runtime_reconcile_total{job="flux",result="error"}[5m])) * 60 or vector(0)', th=thresholds(("green", None), ("orange", 0.1), ("red", 1)), w=6),
    stat("Flux controllers scraped", 'count(up{job="flux"} == 1) or vector(0)', th=thresholds(("red", None), ("orange", 3), ("green", 4)), w=6),
    table("Kustomizations", 'gotk_resource_info{customresource_kind="Kustomization"}', exclude=("customresource_group", "customresource_kind", "customresource_version", "Value"),
          rename={"exported_namespace": "namespace"}, w=12, h=8),
    table("HelmReleases", 'gotk_resource_info{customresource_kind="HelmRelease"}', exclude=("customresource_group", "customresource_kind", "customresource_version", "Value", "chart_ref_name", "chart_source_name"),
          rename={"exported_namespace": "namespace", "revision": "chart version"}, w=12, h=8),
    table("Sources (GitRepository, HelmRepository, OCIRepository)", 'gotk_resource_info{customresource_kind=~"GitRepository|HelmRepository|OCIRepository"}', exclude=("customresource_group", "customresource_version", "Value"),
          rename={"exported_namespace": "namespace", "customresource_kind": "kind"}, w=12, h=8),
    ts("Reconcile duration p95 by kind", [target('histogram_quantile(0.95, sum by(le,kind)(rate(gotk_reconcile_duration_seconds_bucket{job="flux"}[5m])))', "{{kind}}")], unit="s", w=12, h=8),

    row("DNS (external-dns → Cloudflare)"),
    stat("Last successful sync", 'time() - max(external_dns_controller_last_sync_timestamp_seconds{job="external-dns"})', unit="s", th=thresholds(("green", None), ("orange", 600), ("red", 1800)), w=6),
    stat("Last reconcile", 'time() - max(external_dns_controller_last_reconcile_timestamp_seconds{job="external-dns"})', unit="s", th=thresholds(("green", None), ("orange", 600), ("red", 1800)), w=6),
    stat("Registry errors (1h)", 'sum(increase(external_dns_registry_errors_total{job="external-dns"}[1h])) or vector(0)', th=GREEN_RED(1), w=6),
    stat("Consecutive soft errors", 'max(external_dns_controller_consecutive_soft_errors{job="external-dns"})', th=thresholds(("green", None), ("orange", 1), ("red", 3)), w=6),
    ts("Managed DNS records", [
        target('external_dns_registry_endpoints_total{job="external-dns"}', "registry endpoints"),
        target('external_dns_controller_verified_records{job="external-dns"}', "verified records"),
    ], w=12, h=7),
    ts("Cloudflare API latency (avg)", [target('sum(rate(external_dns_http_request_duration_seconds_sum{job="external-dns"}[5m])) / sum(rate(external_dns_http_request_duration_seconds_count{job="external-dns"}[5m]))', "avg")], unit="s", w=12, h=7),
])

# --------------------------------------------------------------------------
# 3. Databases (CNPG) and backups
# --------------------------------------------------------------------------
db_var = var_query("datname", "label_values(cnpg_backends_total, datname)", label="database")
databases = dashboard("k3s-databases", "k3s Databases (CNPG) and Backups", [
    row("Cluster: postgres (1 primary + 2 replicas, one per node)"),
    stat("Primary", 'cnpg_pg_replication_in_recovery{job="cnpg"} == 0', w=4, opts={"textMode": "name", "colorMode": "none"}, fc={"displayName": "${__field.labels.pod}"}, th=thresholds(("green", None))),
    stat("Streaming replicas", 'max(cnpg_pg_replication_streaming_replicas{job="cnpg"})', th=thresholds(("red", None), ("orange", 1), ("green", 2))),
    stat("Instances scraped", 'count(cnpg_collector_up{job="cnpg"} == 1)', th=thresholds(("red", None), ("orange", 2), ("green", 3))),
    stat("Nodes hosting instances", 'count(count by(node)(kube_pod_info{namespace="databases",created_by_name="postgres"}))', th=thresholds(("red", None), ("orange", 2), ("green", 3)), desc="3 expected: anti-affinity spreads instances across nodes."),
    stat("Manual switchover required", 'max(cnpg_collector_manual_switchover_required{job="cnpg"})', th=GREEN_RED(1), fc={"mappings": [{"type": "value", "options": {"0": {"text": "no"}, "1": {"text": "YES"}}}]}),
    stat("Fencing on", 'max(cnpg_collector_fencing_on{job="cnpg"})', th=GREEN_RED(1), fc={"mappings": [{"type": "value", "options": {"0": {"text": "no"}, "1": {"text": "YES"}}}]}),
    ts("Replication lag (replicas)", [target('max by(pod)(cnpg_pg_replication_lag{job="cnpg"})', "{{pod}}")], unit="s", w=8, th=thresholds(("transparent", None), ("red", 10)),
       fc={"custom": {"lineWidth": 1, "fillOpacity": 8, "showPoints": "never", "thresholdsStyle": {"mode": "line"}}}),
    ts("Replay / flush lag seen from primary", [
        target('max by(application_name)(cnpg_pg_stat_replication_replay_lag_seconds{job="cnpg"})', "{{application_name}} replay"),
        target('max by(application_name)(cnpg_pg_stat_replication_flush_lag_seconds{job="cnpg"})', "{{application_name}} flush"),
    ], unit="s", w=8),
    ts("Total connections by instance", [target('sum by(pod)(cnpg_backends_total{job="cnpg"})', "{{pod}}")], w=8),

    row("WAL archiving and backups (Barman Cloud plugin → Cloudflare R2, bucket k3s-backups; cnpg_collector_* backup metrics stay 0 with the plugin)"),
    stat("Since last WAL archived", 'min(cnpg_pg_stat_archiver_seconds_since_last_archival{job="cnpg"})', unit="s", th=thresholds(("green", None), ("orange", 900), ("red", 3600)), w=6,
         desc="archive_timeout forces a WAL segment at least every 5 minutes."),
    stat("WAL archive failures (24h)", 'max(increase(cnpg_pg_stat_archiver_failed_count{job="cnpg"}[24h])) or vector(0)', th=GREEN_RED(1), w=6),
    stat("Last base backup age", 'time() - max(barman_cloud_cloudnative_pg_io_last_available_backup_timestamp{job="cnpg"} > 0)', unit="s", th=thresholds(("green", None), ("orange", 26 * 3600), ("red", 50 * 3600)), w=6),
    stat("Recovery window (first recoverability point)", 'time() - min(barman_cloud_cloudnative_pg_io_first_recoverability_point{job="cnpg"} > 0)', unit="s", th=thresholds(("orange", None), ("green", 3 * 86400)), w=6,
         desc="How far back point-in-time recovery can go."),
    ts("WAL generated", [target('sum(rate(cnpg_collector_wal_bytes{job="cnpg"}[5m]))', "bytes/s")], unit="Bps", w=8, h=7),
    ts("WAL archiver", [
        target('sum(rate(cnpg_pg_stat_archiver_archived_count{job="cnpg"}[5m])) * 60', "archived/min"),
        target('sum(rate(cnpg_pg_stat_archiver_failed_count{job="cnpg"}[5m])) * 60', "failed/min"),
    ], w=8, h=7, over=[{"matcher": {"id": "byName", "options": "failed/min"}, "properties": [{"id": "color", "value": {"mode": "fixed", "fixedColor": "red"}}]}]),
    ts("Last failed backup (age)", [target('time() - max(barman_cloud_cloudnative_pg_io_last_failed_backup_timestamp{job="cnpg"} > 0)', "age")], unit="s", w=8, h=7, desc="No line means no backup has ever failed."),

    row("Per database"),
    ts("Connections", [target('sum by(datname)(cnpg_backends_total{job="cnpg",datname=~"$datname"})', "{{datname}}")], w=8),
    ts("Database size", [target('max by(datname)(cnpg_pg_database_size_bytes{job="cnpg",datname=~"$datname"})', "{{datname}}")], unit="bytes", w=8),
    ts("Transactions/s (primary)", [
        target('sum by(datname)(rate(cnpg_pg_stat_database_xact_commit{job="cnpg",datname=~"$datname"}[5m]))', "{{datname}} commit"),
        target('sum by(datname)(rate(cnpg_pg_stat_database_xact_rollback{job="cnpg",datname=~"$datname"}[5m]))', "{{datname}} rollback"),
    ], w=8),
    ts("Cache hit ratio", [target('sum by(datname)(rate(cnpg_pg_stat_database_blks_hit{job="cnpg",datname=~"$datname"}[5m])) / (sum by(datname)(rate(cnpg_pg_stat_database_blks_hit{job="cnpg",datname=~"$datname"}[5m])) + sum by(datname)(rate(cnpg_pg_stat_database_blks_read{job="cnpg",datname=~"$datname"}[5m])))', "{{datname}}")],
       unit=PCT, fc={"min": 0, "max": 1}, w=8),
    ts("Rows changed/s", [
        target('sum by(datname)(rate(cnpg_pg_stat_database_tup_inserted{job="cnpg",datname=~"$datname"}[5m]))', "{{datname}} ins"),
        target('sum by(datname)(rate(cnpg_pg_stat_database_tup_updated{job="cnpg",datname=~"$datname"}[5m]))', "{{datname}} upd"),
        target('sum by(datname)(rate(cnpg_pg_stat_database_tup_deleted{job="cnpg",datname=~"$datname"}[5m]))', "{{datname}} del"),
    ], w=8),
    ts("Deadlocks and temp files", [
        target('sum by(datname)(increase(cnpg_pg_stat_database_deadlocks{job="cnpg",datname=~"$datname"}[5m]))', "{{datname}} deadlocks"),
        target('sum by(datname)(increase(cnpg_pg_stat_database_temp_files{job="cnpg",datname=~"$datname"}[5m]))', "{{datname}} temp files"),
    ], w=8),
    ts("Longest running transaction", [target('max by(datname)(cnpg_backends_max_tx_duration_seconds{job="cnpg",datname=~"$datname"})', "{{datname}}")], unit="s", w=12, h=6),
    ts("Transaction ID age (wraparound guard)", [target('max by(datname)(cnpg_pg_database_xid_age{job="cnpg",datname=~"$datname"})', "{{datname}}")], w=12, h=6,
       th=thresholds(("transparent", None), ("red", 1.5e9)), fc={"custom": {"lineWidth": 1, "fillOpacity": 8, "showPoints": "never", "thresholdsStyle": {"mode": "line"}}}),

    row("Instance resources"),
    ts("CPU per instance vs request (no CPU limit set)", [
        target('sum by(pod)(rate(container_cpu_usage_seconds_total{job="cadvisor",namespace="databases",container="postgres"}[5m]))', "{{pod}}"),
        target('max(kube_pod_container_resource_requests{namespace="databases",container="postgres",resource="cpu"})', "request"),
    ], unit="cores", w=8, over=[{"matcher": {"id": "byName", "options": "request"}, "properties": [{"id": "custom.lineStyle", "value": {"fill": "dash", "dash": [10, 10]}}, {"id": "color", "value": {"mode": "fixed", "fixedColor": "red"}}]}]),
    ts("Memory working set vs limit", [
        target('sum by(pod)(container_memory_working_set_bytes{job="cadvisor",namespace="databases",container="postgres"})', "{{pod}}"),
        target('max(kube_pod_container_resource_limits{namespace="databases",container="postgres",resource="memory"})', "limit"),
    ], unit="bytes", w=8, over=[{"matcher": {"id": "byName", "options": "limit"}, "properties": [{"id": "custom.lineStyle", "value": {"fill": "dash", "dash": [10, 10]}}, {"id": "color", "value": {"mode": "fixed", "fixedColor": "red"}}]}]),
    ts("Data volume usage", [target('kubelet_volume_stats_used_bytes{job="kubelet",namespace="databases"} / kubelet_volume_stats_capacity_bytes{job="kubelet",namespace="databases"}', "{{persistentvolumeclaim}}")],
       unit=PCT, fc={"min": 0, "max": 1}, w=8, th=thresholds(("transparent", None), ("red", 0.8))),
], variables=[db_var])

# --------------------------------------------------------------------------
# 4. Applications (templated per namespace/app)
# --------------------------------------------------------------------------
app_var = var_query("app", "label_values(kube_pod_info, namespace)", regex="/^(executionlab-staging|website|gvasiliourolex|mcp|monitoring)$/", multi=False, label="application")
app_var["current"] = {"selected": True, "text": "website", "value": "website"}
# Database names do not always equal the namespace (executionlab-staging owns
# executionlab_staging_platform and executionlab_staging_bot), so the CNPG
# panels select databases through their own variable instead of reusing $app.
db_var = var_query("db", "label_values(cnpg_pg_database_size_bytes, datname)",
                   regex="/^(website|gvasiliourolex|executionlab_staging_.*)$/", multi=True, label="database")
SVC = 'service=~"$app-.*@kubernetes"'
LIMIT_OVER = [{"matcher": {"id": "byRegexp", "options": ".*limit.*"}, "properties": [{"id": "custom.lineStyle", "value": {"fill": "dash", "dash": [10, 10]}}, {"id": "color", "value": {"mode": "fixed", "fixedColor": "red"}}]},
              {"matcher": {"id": "byRegexp", "options": ".*request.*"}, "properties": [{"id": "custom.lineStyle", "value": {"fill": "dot", "dash": [2, 4]}}, {"id": "color", "value": {"mode": "fixed", "fixedColor": "orange"}}]}]
applications = dashboard("k3s-applications", "k3s Applications", [
    row("$app — health"),
    stat("Pods ready / desired", 'sum(kube_deployment_status_replicas_ready{namespace="$app"})', th=thresholds(("red", None), ("green", 1)), w=3,
         fc={"displayName": "ready"}),
    stat("Desired", 'sum(kube_deployment_status_replicas{namespace="$app"})', th=thresholds(("green", None)), w=3),
    stat("Restarts (24h)", 'sum(increase(kube_pod_container_status_restarts_total{namespace="$app"}[24h])) or vector(0)', th=thresholds(("green", None), ("orange", 1), ("red", 5)), w=3),
    stat("OOM kills (last termination)", 'sum(kube_pod_container_status_last_terminated_reason{namespace="$app",reason="OOMKilled"}) or vector(0)', th=GREEN_RED(1), w=3),
    stat("Requests/s", 'sum(rate(traefik_service_requests_total{job="traefik",%s}[5m]))' % SVC, unit="reqps", th=thresholds(("green", None)), w=3),
    stat("5xx ratio (5m)", 'sum(rate(traefik_service_requests_total{job="traefik",%s,code=~"5.."}[5m])) / sum(rate(traefik_service_requests_total{job="traefik",%s}[5m]))' % (SVC, SVC), unit=PCT, th=thresholds(("green", None), ("orange", 0.01), ("red", 0.05)), w=3),
    stat("p95 latency", 'histogram_quantile(0.95, sum by(le)(rate(traefik_service_request_duration_seconds_bucket{job="traefik",%s}[5m])))' % SVC, unit="s", th=thresholds(("green", None), ("orange", 0.5), ("red", 2)), w=3),
    stat("Cert time left", 'min(certmanager_certificate_expiration_timestamp_seconds{namespace="$app"} - time())', unit="s", th=thresholds(("red", None), ("orange", 7 * 86400), ("green", 20 * 86400)), w=3),

    row("HTTP (Traefik service metrics — the apps expose no /metrics of their own)"),
    ts("Requests/s by status code", [target('sum by(code)(rate(traefik_service_requests_total{job="traefik",%s}[5m]))' % SVC, "{{code}}")], unit="reqps", w=8,
       fc={"custom": {"lineWidth": 1, "fillOpacity": 30, "showPoints": "never", "stacking": {"mode": "normal"}}}),
    ts("Latency percentiles", [
        target('histogram_quantile(0.50, sum by(le)(rate(traefik_service_request_duration_seconds_bucket{job="traefik",%s}[5m])))' % SVC, "p50"),
        target('histogram_quantile(0.95, sum by(le)(rate(traefik_service_request_duration_seconds_bucket{job="traefik",%s}[5m])))' % SVC, "p95"),
        target('histogram_quantile(0.99, sum by(le)(rate(traefik_service_request_duration_seconds_bucket{job="traefik",%s}[5m])))' % SVC, "p99"),
    ], unit="s", w=8),
    ts("Errors/s", [
        target('sum(rate(traefik_service_requests_total{job="traefik",%s,code=~"4.."}[5m]))' % SVC, "4xx"),
        target('sum(rate(traefik_service_requests_total{job="traefik",%s,code=~"5.."}[5m]))' % SVC, "5xx"),
    ], unit="reqps", w=8, over=[{"matcher": {"id": "byName", "options": "5xx"}, "properties": [{"id": "color", "value": {"mode": "fixed", "fixedColor": "red"}}]}]),
    ts("Requests/s by method", [target('sum by(method)(rate(traefik_service_requests_total{job="traefik",%s}[5m]))' % SVC, "{{method}}")], unit="reqps", w=8, h=6),
    ts("Bandwidth", [
        target('sum(rate(traefik_service_requests_bytes_total{job="traefik",%s}[5m]))' % SVC, "in"),
        target('-sum(rate(traefik_service_responses_bytes_total{job="traefik",%s}[5m]))' % SVC, "out"),
    ], unit="Bps", w=8, h=6),
    ts("Requests/s by backend service (all services in $app)", [target('sum by(service)(rate(traefik_service_requests_total{job="traefik",%s}[5m]))' % SVC, "{{service}}")], unit="reqps", w=8, h=6),

    row("Pods"),
    ts("CPU per pod vs requests/limits", [
        target('sum by(pod)(rate(container_cpu_usage_seconds_total{job="cadvisor",namespace="$app",container!=""}[5m]))', "{{pod}}"),
        target('sum(kube_pod_container_resource_requests{namespace="$app",resource="cpu"} * on(namespace,pod) group_left() max by(namespace,pod)(kube_pod_status_phase{phase="Running"} == 1))', "requests (running pods)"),
        target('sum(kube_pod_container_resource_limits{namespace="$app",resource="cpu"} * on(namespace,pod) group_left() max by(namespace,pod)(kube_pod_status_phase{phase="Running"} == 1))', "limits (running pods)"),
    ], unit="cores", w=8, over=LIMIT_OVER),
    ts("Memory working set per pod vs requests/limits", [
        target('sum by(pod)(container_memory_working_set_bytes{job="cadvisor",namespace="$app",container!=""})', "{{pod}}"),
        target('sum(kube_pod_container_resource_requests{namespace="$app",resource="memory"} * on(namespace,pod) group_left() max by(namespace,pod)(kube_pod_status_phase{phase="Running"} == 1))', "requests (running pods)"),
        target('sum(kube_pod_container_resource_limits{namespace="$app",resource="memory"} * on(namespace,pod) group_left() max by(namespace,pod)(kube_pod_status_phase{phase="Running"} == 1))', "limits (running pods)"),
    ], unit="bytes", w=8, over=LIMIT_OVER),
    ts("Pods by phase", [target('sum by(phase)(kube_pod_status_phase{namespace="$app"} == 1)', "{{phase}}")], w=8,
       fc={"custom": {"lineWidth": 1, "fillOpacity": 30, "showPoints": "never", "stacking": {"mode": "normal"}}}),
    ts("Container restarts", [target('sum by(pod)(increase(kube_pod_container_status_restarts_total{namespace="$app"}[1h]))', "{{pod}}")], w=8, h=6),
    ts("Network per pod", [
        target('sum by(pod)(rate(container_network_receive_bytes_total{job="cadvisor",namespace="$app"}[5m]))', "{{pod}} rx"),
        target('-sum by(pod)(rate(container_network_transmit_bytes_total{job="cadvisor",namespace="$app"}[5m]))', "{{pod}} tx"),
    ], unit="Bps", w=8, h=6),
    table("Pods and nodes", 'kube_pod_info{namespace="$app"}', exclude=("Value", "uid", "host_network", "created_by_kind", "host_ip", "pod_ip", "priority_class", "namespace"), rename={"created_by_name": "owner"}, w=8, h=6),

    row("Jobs and CronJobs"),
    ts("CronJob: time since last success", [target('time() - kube_cronjob_status_last_successful_time{namespace="$app"}', "{{cronjob}}")], unit="s", w=8, h=7),
    ts("Failed jobs by CronJob (running total in window)", [target('sum by(owner_name)(kube_job_status_failed{namespace="$app"} * on(namespace,job_name) group_left(owner_name) kube_job_owner{namespace="$app",owner_kind="CronJob"})', "{{owner_name}}")], w=8, h=7),
    ts("Active jobs", [target('sum by(cronjob)(kube_cronjob_status_active{namespace="$app"})', "{{cronjob}}")], w=8, h=7),
    table("Jobs (last 24h, failed first)", 'sort_desc(max by(job_name)(kube_job_status_failed{namespace="$app"} and on(job_name) (time() - kube_job_created{namespace="$app"} < 86400)))',
          rename={"Value": "failed pods", "job_name": "job"}, w=12, h=7),
    table("CronJob schedule", 'kube_cronjob_info{namespace="$app"}', exclude=("Value", "namespace", "concurrency_policy"), w=12, h=7),

    row("Database (CNPG, datname = $db) and storage"),
    stat("DB connections", 'sum(cnpg_backends_total{job="cnpg",datname=~"$db"})', th=thresholds(("green", None), ("orange", 40), ("red", 80)), w=4),
    stat("DB size", 'max(cnpg_pg_database_size_bytes{job="cnpg",datname=~"$db"})', unit="bytes", th=thresholds(("green", None)), w=4),
    stat("Commits/s", 'sum(rate(cnpg_pg_stat_database_xact_commit{job="cnpg",datname=~"$db"}[5m]))', th=thresholds(("green", None)), w=4),
    stat("Deadlocks (24h)", 'sum(increase(cnpg_pg_stat_database_deadlocks{job="cnpg",datname=~"$db"}[24h])) or vector(0)', th=GREEN_RED(1), w=4),
    stat("Cache hit ratio", 'sum(rate(cnpg_pg_stat_database_blks_hit{job="cnpg",datname=~"$db"}[5m])) / (sum(rate(cnpg_pg_stat_database_blks_hit{job="cnpg",datname=~"$db"}[5m])) + sum(rate(cnpg_pg_stat_database_blks_read{job="cnpg",datname=~"$db"}[5m])))', unit=PCT, th=thresholds(("red", None), ("orange", 0.9), ("green", 0.99)), w=4),
    stat("PVC usage (max in $app)", 'max(kubelet_volume_stats_used_bytes{job="kubelet",namespace="$app"} / kubelet_volume_stats_capacity_bytes{job="kubelet",namespace="$app"})', unit=PCT, th=thresholds(("green", None), ("orange", 0.7), ("red", 0.85)), w=4),
    ts("DB connections and size", [
        target('sum(cnpg_backends_total{job="cnpg",datname=~"$db"})', "connections"),
        target('max(cnpg_pg_database_size_bytes{job="cnpg",datname=~"$db"})', "size"),
    ], w=12, h=7, over=[{"matcher": {"id": "byName", "options": "size"}, "properties": [{"id": "unit", "value": "bytes"}, {"id": "custom.axisPlacement", "value": "right"}]}]),
    ts("PVC usage in $app", [target('kubelet_volume_stats_used_bytes{job="kubelet",namespace="$app"} / kubelet_volume_stats_capacity_bytes{job="kubelet",namespace="$app"}', "{{persistentvolumeclaim}}")],
       unit=PCT, fc={"min": 0, "max": 1}, w=12, h=7),
], variables=[app_var, db_var])

for name, d in [("cluster-overview", cluster), ("platform", platform), ("databases", databases), ("applications", applications)]:
    path = os.path.join(OUT, f"{name}.json")
    with open(path, "w") as f:
        json.dump(d, f, indent=1); f.write("\n")
    print(f"{path}: {len(d['panels'])} panels, {os.path.getsize(path)} bytes")
