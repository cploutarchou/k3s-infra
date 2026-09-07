---
description: Read-only incident triage — diagnose with evidence before any fix
argument-hint: [what is wrong]
allowed-tools: Bash, Read, Grep, Glob
skills: incident-triage
---

Investigate: $ARGUMENTS

Run the incident-triage procedure: a read-only investigation that
produces a diagnosis with evidence before any fix is proposed. No
mutating commands during triage — no kubectl apply/delete/patch/scale,
no helm install/upgrade, no node changes.

If an incident-triage subagent is available, delegate the investigation
to it; otherwise follow the incident-triage skill procedure yourself.

Report: symptom; evidence (each claim with the exact command that
produced it); first broken hop; most likely cause; blast radius; a
proposed fix expressed as a git change (never applied by you); what
that fix would restart or risk; and any checks you could not run,
listed as not-run with the command an operator would use.
