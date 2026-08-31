# Ansible — day-0 and node lifecycle

Run from this directory. Nothing here runs automatically; every play is
operator-triggered.

```sh
ansible-playbook playbooks/site.yml                       # full day-0
ansible-playbook playbooks/00-hardening.yml               # SSH, sysctl, /tmp
ansible-playbook playbooks/10-network.yml                 # eth1 vLAN + nftables
ansible-playbook playbooks/20-k3s.yml -e @vault/r2-credentials.sops.yml
```

Order matters: hardening → network → k3s. The k3s play refuses to run if
eth1 doesn't hold the node's 10.0.0.x address, and it joins servers
`serial: 1` (etcd members must join one at a time).

Requires: `ansible-core`, collections `community.general` + `community.sops`
(`ansible-galaxy collection install community.general community.sops`).

R2 snapshot credentials: see `vault/README.md`. Without them the play still
works — snapshots stay node-local and a warning is printed.
