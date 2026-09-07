---
name: verify-before-claiming
description: Use whenever pinning a version, chart, image tag, digest, or dashboard ID; whenever writing values for a chart you haven't rendered; and whenever reporting results of checks. Verify against live sources instead of model memory, render before committing, and never report a check as passed unless it actually ran.
---

# Verify before claiming

Model memory about versions, schemas, and IDs is stale by construction.
Live sources are one HTTP request away. The cost asymmetry is total: a
verification query costs seconds; a wrong pin costs a broken reconcile, a
crashlooping pod, or a silently missing backup.

## Rules

1. **Every version pin comes from a live source at pin time.** Helm chart
   versions from the repo's `index.yaml`; image tags and digests from the
   registry API (`/v2/<name>/tags/list`, then a manifest HEAD for the
   digest); anything with an ID (Grafana dashboards, release assets) from
   the hosting service's API. Record the date of verification in a
   comment.
2. **Render before committing.** `helm template` the chart at the pinned
   version with your actual values; `kustomize build` the tree;
   kubeconform against a CRD catalog. If it won't render locally it won't
   reconcile remotely.
3. **Never report an unrun check as passed.** If a tool is missing or a
   boundary prevents running it, say "skipped — not run" and give the
   operator the exact command. A validation script should track skips
   separately from passes and say so in its summary.
4. **Verify the claim after the change**, not just before: query the
   thing itself (the API, the endpoint, the registry) rather than assuming
   the change took effect.

## Why this is not paranoia — three real catches from one day

- **Chart schema break**: a Traefik chart bumped several majors rejected
  the previous values outright (`redirections` had moved under
  `ports.web.http`, a `logs:` block became `log:`) — the chart's own
  values schema failed the render. Caught by `helm template` locally;
  would otherwise have shipped a HelmRelease that could never install.
- **Major-series jump**: kube-vip's "latest" was assumed to be a newer
  0.x; the registry's tag list showed the project had moved to a 1.x
  series entirely. Memory would have pinned a stale major. Caught by
  querying the registry before pinning.
- **Enum drift in a CRD**: a backup configuration used a compression
  value the CRD schema didn't allow for that field (valid for WAL, not
  for base backups). Caught by kubeconform with the CRD catalog —
  a runtime-only discovery otherwise.

The same day also produced a runtime-only catch (an operator's modern
images had dropped a bundled binary, breaking in-tree backups): local
validation cannot catch everything, which is exactly why post-change
verification (rule 4) exists.
