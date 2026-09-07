---
name: gitops-workflow
description: Use when changing Kubernetes cluster state in a GitOps repo (Flux/Argo) — adding or editing manifests, deploying an app, bumping a chart or image. Enforces the manifest → validate → commit/PR → reconcile loop and the manifest hygiene rules (digest pins, requests/limits, probes, SOPS-only secrets). Never kubectl apply.
---

# GitOps workflow

The cluster is reconciled from git. If a change is not in git, it does not
exist; if it is only on the cluster, it is drift and will be reverted.

## The loop

1. **Read current state** — the manifests in git first, `kubectl get`
   (read-only) second. Git is the source of truth; the cluster is a cache.
2. **Edit manifests** in the repo. One directory per application, with its
   own kustomization registered in the parent so the reconciler sees it.
3. **Validate locally before committing** — schema validation
   (kubeconform with a CRD catalog), `kustomize build` on every
   kustomization you touched, and `helm template` for any chart whose
   values you changed. A change that only fails at reconcile time wasted a
   round trip.
4. **Commit/PR** with a conventional message. Trivial in-session changes
   may go straight to the default branch if the repo's policy allows;
   anything non-trivial gets a PR.
5. **Reconcile and watch** — trigger the reconciler rather than waiting
   out the interval, then watch the object's Ready condition until it
   converges or fails. Applied is not done; Ready is done.
6. **Report what changed**: files, what restarts, capacity impact,
   anything left for a human.

## Manifest hygiene (every workload, no exceptions)

- **Images pinned by digest** (`repo@sha256:…`), never `:latest`. Resolve
  the digest from the registry at pin time; keep the tag alongside as a
  human-readable comment or field.
- **Resource requests and limits on every container.** Track a capacity
  budget: total requests must leave enough headroom that losing one node
  is survivable (e.g. stay under 2/3 of cluster allocatable on a 3-node
  cluster).
- **Readiness probe on every Deployment.** Liveness probes only where a
  hang is a real failure mode — a bad liveness probe causes restarts, not
  healing.
- **Secrets only via SOPS** (age or KMS). A plain `kind: Secret` with
  base64 data in git is a leak, not a secret — flag it, never write it.
  Ship `.example` templates for secrets; keep their kustomization entries
  commented out until the encrypted file exists, so builds never break on
  a missing secret.
- **Kustomize overlays, not templated copies.** Layer environments;
  don't fork files.
- **Dependency-order the reconciliation**: CRD-installing controllers in
  one layer, resources using those CRDs in a dependent layer, apps last.

## Hard lines

- No `kubectl apply/delete/patch/edit/scale/drain/rollout` and no
  `helm install/upgrade` against the cluster. The only sanctioned cluster
  "write" is asking the reconciler to reconcile now.
- Never delete a PVC, StatefulSet, or database custom resource without
  stating what data is lost and getting confirmation.
- Auth, RBAC, and firewall changes are security-relevant: call them out
  and wait for confirmation.
