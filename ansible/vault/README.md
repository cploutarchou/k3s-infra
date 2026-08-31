# Encrypted Ansible vars (SOPS + age)

R2 credentials for etcd snapshots live in `r2-credentials.sops.yml`,
encrypted with SOPS/age (rule in the repo-root `.sops.yaml`). Create it like
this once the R2 API token exists:

```sh
cat > ansible/vault/r2-credentials.sops.yml <<'EOF'
r2_access_key_id: <ACCESS_KEY_ID>
r2_secret_access_key: <SECRET_ACCESS_KEY>
EOF
sops -e -i ansible/vault/r2-credentials.sops.yml
```

Then run the k3s playbook with:

```sh
ansible-playbook playbooks/20-k3s.yml -e @vault/r2-credentials.sops.yml
```

(with `sops exec-env` or the community.sops vars plugin, e.g.
`sops -d ansible/vault/r2-credentials.sops.yml > /dev/shm/r2.yml` for a
one-off run — never write decrypted files into the repo).

Until the credentials exist, the k3s role configures the snapshot schedule
but skips the S3 upload settings and warns.
