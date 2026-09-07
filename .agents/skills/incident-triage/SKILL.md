---
name: incident-triage
description: Use when something is reported down, degraded, failing, or "weird" — an outage, a crashlooping pod, a failed reconcile, an unreachable endpoint. Read-only investigation that produces a diagnosis with evidence before any fix is proposed. No mutating commands during triage.
---

# Incident triage (read-only)

The deliverable of triage is a **diagnosis with evidence**, not a fix.
Fixes come after, through the normal change process, with the operator's
sign-off. This ordering exists because a fix applied to a misdiagnosed
symptom usually widens the incident.

## Hard rule

No mutating commands during triage. Nothing that restarts, deletes,
patches, scales, drains, edits, or reconfigures — on the cluster or on
nodes. Read-only means: `get`, `describe`, `logs`, `events`, `top`,
status endpoints, `journalctl`, `systemctl status`, `ss`, `ip`,
`nft list`, `df`, `free`, metrics queries. If the evidence points at a
fix, write the fix down; do not apply it mid-triage.

## Procedure

1. **Reproduce the symptom yourself** from the same vantage point as the
   reporter (through the public URL, not just from inside). "Works from
   here" vs "fails from there" is itself a finding.
2. **Establish the blast radius**: one pod, one node, one namespace, one
   ingress host, or everything? Check the cluster-wide basics first —
   nodes Ready, control-plane health endpoints, reconciler status,
   non-running pods — before zooming in.
3. **Walk the request path outside-in**: DNS answer → edge/proxy → TLS
   (check the certificate actually served, at the right layer — a
   proxy's edge cert is not your origin cert) → ingress → service →
   pod → app logs. Name the first hop that's wrong.
4. **Check what changed**: recent commits, reconciler revisions, image
   digests, restarts, node events. Most incidents are the last change.
5. **Distinguish "down" from "was down"**: a healthy endpoint plus a
   report of failure means a transient — look at restart counts, rollout
   timestamps, and serial-restart windows, and say so rather than hunting
   a ghost.
6. **Write the diagnosis**: symptom, evidence (exact commands and
   outputs), first broken hop, cause, blast radius, proposed fix, and
   what the fix will restart or risk. Only then, with approval, fix it
   through the normal change path.

## Reporting standard

Report only what was observed. Every claim carries the command that
produced it. If a check couldn't run (missing access, read-only
boundary), report it as not-run with the exact command for the operator —
never as passed.
