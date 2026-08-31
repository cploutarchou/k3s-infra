#!/usr/bin/env bash
# Local validation for k3s-infra: kubeconform on clusters/, kustomize build
# checks, ansible-lint on ansible/, SOPS hygiene, Go build for mcp/.
# Skips (with a warning) any tool that isn't installed — it never claims a
# check passed that didn't run.
set -uo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"
FAIL=0
SKIPPED=()

section() { printf '\n=== %s ===\n' "$1"; }
have() { command -v "$1" >/dev/null 2>&1; }

# Schemas for CRDs (Flux, cert-manager, CNPG) come from the datree CRD
# catalog; unknown CRDs are skipped rather than failed.
CRD_CATALOG='https://raw.githubusercontent.com/datreeio/CRDs-catalog/main/{{.Group}}/{{.ResourceKind}}_{{.ResourceAPIVersion}}.json'

section "kubeconform (clusters/)"
if have kubeconform; then
  find clusters -name '*.yaml' \
    ! -name '*.sops.yaml' ! -name '*.example' ! -name 'kustomization.yaml' -print0 |
    xargs -0 kubeconform -strict -summary \
      -schema-location default \
      -schema-location "$CRD_CATALOG" \
      -ignore-missing-schemas || FAIL=1
else
  SKIPPED+=("kubeconform (go install github.com/yannh/kubeconform/cmd/kubeconform@latest)")
fi

section "kustomize build (every kustomization under clusters/)"
KUSTOMIZE=""
if have kustomize; then KUSTOMIZE="kustomize build"; elif have kubectl; then KUSTOMIZE="kubectl kustomize"; fi
if [ -n "$KUSTOMIZE" ]; then
  while IFS= read -r kfile; do
    dir="$(dirname "$kfile")"
    if ! $KUSTOMIZE "$dir" >/dev/null; then
      echo "FAIL: $dir"
      FAIL=1
    else
      echo "ok:   $dir"
    fi
  done < <(find clusters -name kustomization.yaml)
else
  SKIPPED+=("kustomize/kubectl (kustomize build check)")
fi

section "ansible-lint (ansible/)"
if have ansible-lint; then
  (cd ansible && ansible-lint) || FAIL=1
else
  SKIPPED+=("ansible-lint (pipx install ansible-lint)")
fi

section "ansible syntax check"
if have ansible-playbook; then
  (cd ansible && ansible-playbook --syntax-check playbooks/site.yml) || FAIL=1
else
  SKIPPED+=("ansible-playbook (pipx install ansible-core)")
fi

section "SOPS hygiene (no plaintext secrets in git)"
# Any kind: Secret under clusters/ must either be a *.example template or a
# SOPS-encrypted file (contains a sops: metadata block with ENC[ values).
BAD_SECRETS=0
while IFS= read -r f; do
  case "$f" in *.example) continue ;; esac
  if grep -qE '^kind: Secret' "$f" && ! grep -q 'ENC\[' "$f"; then
    echo "PLAINTEXT SECRET: $f"
    BAD_SECRETS=1
  fi
done < <(grep -rlE '^kind: Secret' clusters --include='*.yaml' 2>/dev/null || true)
if [ "$BAD_SECRETS" -ne 0 ]; then FAIL=1; else echo "ok: no plaintext Secret manifests"; fi

section "mcp (go build + vet)"
if have go; then
  (cd mcp && go build ./... && go vet ./...) || FAIL=1
else
  SKIPPED+=("go (mcp build)")
fi

section "summary"
if [ "${#SKIPPED[@]}" -gt 0 ]; then
  echo "SKIPPED checks (tool not installed — NOT passed):"
  printf '  - %s\n' "${SKIPPED[@]}"
fi
if [ "$FAIL" -ne 0 ]; then
  echo "RESULT: FAIL"
  exit 1
fi
echo "RESULT: OK (excluding skipped)"
