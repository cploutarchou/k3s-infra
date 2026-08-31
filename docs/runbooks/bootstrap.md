# Runbook: cluster bootstrap (day 0)

Operator-run, in this order. Nothing here is automated.

## 0. Prerequisites

- SSH key `~/.ssh/k3s-infra` authorized as root on all three nodes.
- `ansible-core` + collections: `ansible-galaxy collection install
  community.general community.sops`.
- `age`, `sops`, `flux` CLI, `kubectl` locally.

## 1. Generate the age key and wire SOPS

```sh
age-keygen -o age.agekey            # keep OUT of git
# put the public key into .sops.yaml (both rules), commit that change
```

## 2. R2 credentials

Create an R2 API token scoped to bucket `k3s-backups` (EU). Fill in:

- `ansible/vault/r2-credentials.sops.yml` (see `ansible/vault/README.md`)
- `clusters/prod/infrastructure/configs/postgres/r2-backup-credentials.sops.yaml`
  (from the `.example`, then `sops -e -i`, uncomment in kustomization)

Cloudflare API token (Zone:DNS:Edit on cpdevlab.com) likewise into the two
`cloudflare-api-token-secret.sops.yaml` files (cert-manager + external-dns).

## 3. Ansible day-0

```sh
cd ansible
ansible-playbook playbooks/00-hardening.yml
ansible-playbook playbooks/10-network.yml     # eth1 vLAN + nftables
ansible-playbook playbooks/20-k3s.yml -e @vault/r2-credentials.sops.yml
```

k3s installs serially: k3s-01 (`cluster-init`), then 02, then 03 join via
`https://10.0.0.11:6443`. Verify from k3s-01 (read-only):

```sh
ssh -i ~/.ssh/k3s-infra root@159.195.82.201 k3s kubectl get nodes -o wide
```

All three nodes Ready, INTERNAL-IP in 10.0.0.0/24.

## 4. Flux bootstrap

```sh
flux bootstrap github \
  --owner=cploutarchou --repository=k3s-infra \
  --branch=master --path=clusters/prod --personal
```

This replaces the placeholder `gotk-components.yaml` / `gotk-sync.yaml`.
Then create the decryption secret from the age key:

```sh
kubectl -n flux-system create secret generic sops-age \
  --from-file=age.agekey=age.agekey
```

(One-time bootstrap command run by the operator — after this, all cluster
changes go through git.)

## 5. Verify

```sh
flux get kustomizations              # flux-system, infra-controllers, infra-configs, apps Ready
kubectl get nodes -o wide
kubectl get pods -A --field-selector=status.phase!=Running
kubectl get --raw=/healthz/etcd
kubectl -n databases get cluster postgres   # 3 instances, 1 primary
```

Check R2: an etcd snapshot appears within 6 h under `etcd/<node>/`; CNPG
WALs under `postgres/` once the cluster is up.
