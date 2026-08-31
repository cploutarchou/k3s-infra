# Runbook: etcd snapshot restore (break-glass)

**Human-only procedure.** Automation (including AI agents) must never touch
etcd; this exists so a person can act under pressure. Restoring rolls the
cluster state back to the snapshot time — anything created since is lost
(Flux will re-create whatever is in git; data in PVs is NOT part of etcd).

## When

- Quorum permanently lost (2+ nodes unrecoverable), or
- etcd data corrupted on a majority of members.

If only one node is lost, do NOT restore — replace the node and let etcd
re-sync (quorum of 2/3 keeps the cluster writable).

## Procedure

1. Stop k3s on **all** servers:

   ```sh
   systemctl stop k3s        # on k3s-01, k3s-02, k3s-03
   ```

2. Pick the snapshot. Local snapshots live in
   `/var/lib/rancher/k3s/server/db/snapshots/`; R2 holds
   `k3s-backups/etcd/<node>/`. List what k3s knows about:

   ```sh
   k3s etcd-snapshot list
   ```

3. On ONE node (say k3s-01), restore. For an S3 snapshot the same
   `etcd-s3-*` flags/credentials as in `/etc/rancher/k3s/config.yaml`
   apply:

   ```sh
   k3s server \
     --cluster-reset \
     --cluster-reset-restore-path=<SNAPSHOT_NAME_OR_PATH>
   ```

   Wait for "Managed etcd cluster membership has been reset"; the process
   exits.

4. Start k3s on the restore node: `systemctl start k3s`. Verify it comes
   up single-node: `k3s kubectl get nodes`.

5. On the OTHER two nodes, wipe the stale etcd state, then rejoin:

   ```sh
   rm -rf /var/lib/rancher/k3s/server/db
   systemctl start k3s
   ```

   They rejoin via the `server:` URL in their config.

6. Verify: 3 Ready nodes, `kubectl get --raw=/healthz/etcd` returns ok,
   `flux get kustomizations` all Ready (Flux reconciles drift from git),
   CNPG cluster healthy (`kubectl -n databases get cluster postgres`).

7. Take a fresh snapshot immediately:

   ```sh
   k3s etcd-snapshot save
   ```

## Aftermath

- Audit anything created between snapshot and restore (git history is the
  source of truth; out-of-git changes are gone by design).
- If postgres data is also suspect, CNPG point-in-time recovery from R2 is
  a separate procedure — do not improvise it during an etcd incident.
