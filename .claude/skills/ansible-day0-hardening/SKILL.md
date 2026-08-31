---
name: ansible-day0-hardening
description: Use when writing Ansible for day-0 provisioning or hardening of Linux servers that will form a cluster — SSH lockdown, firewall, private inter-node networking, and the ordering/safety patterns (serial joins, fail-fast, check-mode-clean playbooks).
---

# Ansible day-0 hardening

Patterns for taking fresh root servers to a hardened, cluster-ready state,
where every mutation is a playbook and the dry run is trustworthy.

## SSH: key-only, validated before restart

Drop a hardening file into `sshd_config.d/` rather than editing the main
config, and always use `validate: sshd -t -f %s` on the template task — a
broken sshd config on a remote root server is a locked door.

Baseline: `PasswordAuthentication no`, `KbdInteractiveAuthentication no`,
`AuthenticationMethods publickey`, root login key-only if root is used at
all, low `MaxAuthTries`, no X11/agent forwarding.

## Firewall: default drop, tiny public surface

nftables with input `policy drop`. Allow: established/related, loopback,
ICMP/ICMPv6 (path-MTU discovery breaks silently without it), the exact
public service ports (typically 22/80/443), and the private cluster
network as a trusted source on its interface. Leave the forward chain to
the container networking layer — kube-proxy/CNI manage their own chains.
Use `validate: nft -c -f %s` on the ruleset template.

## Private cluster networking

All inter-node traffic (control plane, database replication, overlay
networks, metrics endpoints) rides a private interface/VLAN, never public
IPs. Configure it persistently, then **assert it before anything depends
on it**: a task that fails the play if the interface lacks its address,
and a peer-reachability ping (warn-only on first bring-up). Cluster
install tasks assert the private network again as a precondition. Watch
the MTU — virtual private networks usually carry overhead.

## Ordering and safety

- **`serial: 1` for anything quorum-based** (etcd, database clusters,
  rolling restarts of control-plane services). Parallel joins can corrupt
  cluster bootstrap; parallel restarts can lose quorum.
- **`any_errors_fatal: true` on every play** — one broken host must stop
  the run, not leave a half-configured fleet.
- **Make `--check --diff` genuinely runnable on fresh hosts**: guard
  verify-tasks with `when: not ansible_check_mode`, give templates
  fallbacks for values that only exist after a real run (join tokens),
  and bootstrap `python3-apt`/interpreter deps with a `raw` task marked
  `check_mode: false` (minimal images ship without them; the apt module
  needs it even to check). Show the operator the dry-run diff before the
  real run.
- Handlers restart services; flush handlers before any wait-for-healthy
  task so the wait tests the new config.
- Secrets for playbooks come from SOPS-encrypted vars files, decrypted to
  memory-backed paths (`/dev/shm`) for the run and removed after — never
  plaintext in the repo, never in shell history.
- Mind tmpfs `/tmp` on modern distros: cap its size, and point heavy
  writers (container runtimes, snapshot staging) at disk-backed dirs via
  service env overrides.
- Verify after: effective sshd settings (`sshd -T`), live ruleset
  (`nft list ruleset`), interface state, service health — and report what
  was actually observed, not what the playbook intended.
