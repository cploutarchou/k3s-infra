#!/usr/bin/env bash
# Create a SOPS-encrypted Kubernetes Secret without the values touching
# disk, shell history or the process list: each value is read silently
# from the terminal and the YAML is piped straight into `sops encrypt`,
# which applies the creation rules in .sops.yaml.
#
#   ./scripts/sops-new-secret.sh <clusters/.../name.sops.yaml> <namespace> <secret-name> KEY [KEY? ...]
#
# A key ending in "?" is optional: an empty answer leaves it out.
# Run from the repository root. Refuses to overwrite an existing file;
# edit an existing secret with `sops <file>` instead.
set -euo pipefail

die() {
  printf 'error: %s\n' "$*" >&2
  exit 1
}

[ $# -ge 4 ] || die "usage: $0 <clusters/.../name.sops.yaml> <namespace> <secret-name> KEY [KEY? ...]"
path=$1 namespace=$2 name=$3
shift 3

case $path in
  clusters/*.sops.yaml) ;;
  *) die "the path must be clusters/...*.sops.yaml so the .sops.yaml creation rule applies" ;;
esac
[ -f .sops.yaml ] || die "run from the repository root (no .sops.yaml here)"
[ -e "$path" ] && die "$path already exists; edit it with: sops $path"
[ -d "$(dirname "$path")" ] || die "directory $(dirname "$path") does not exist"
[ -t 0 ] || die "run this in an interactive terminal: values are typed, never piped"
command -v sops >/dev/null || die "sops is not installed"

dns_label='^[a-z0-9]([-a-z0-9]*[a-z0-9])?$'
[[ $namespace =~ $dns_label ]] || die "invalid namespace: $namespace"
[[ $name =~ $dns_label ]] || die "invalid secret name: $name"

yaml="apiVersion: v1
kind: Secret
metadata:
  name: ${name}
  namespace: ${namespace}
type: Opaque
stringData:
"
count=0
for spec in "$@"; do
  key=${spec%\?}
  optional=0
  [ "$key" != "$spec" ] && optional=1
  [[ $key =~ ^[A-Za-z_][A-Za-z0-9_]*$ ]] || die "invalid key: $key"
  value=
  IFS= read -r -s -p "${key}$([ "$optional" -eq 1 ] && printf ' (optional, Enter to skip)'): " value
  printf '\n' >&2
  if [ -z "$value" ]; then
    [ "$optional" -eq 1 ] && continue
    die "$key needs a value"
  fi
  # Single-quoted YAML scalar: the only escape is a doubled quote.
  yaml+="  ${key}: '${value//\'/\'\'}'"$'\n'
  count=$((count + 1))
done
unset value
[ "$count" -gt 0 ] || die "no values given"

tmp="${path}.tmp.$$"
trap 'rm -f "$tmp"' EXIT
printf '%s' "$yaml" | sops encrypt --filename-override "$path" --input-type yaml --output-type yaml >"$tmp"
unset yaml
grep -q '^sops:' "$tmp" || die "sops produced no metadata; nothing written"
# Every stringData entry must be an ENC[...] value, and all of them there.
read -r enc plain < <(awk '
  /^stringData:/ { in_data = 1; next }
  /^[^ ]/        { in_data = 0 }
  in_data && /^ +[A-Za-z_][A-Za-z0-9_]*:/ { if ($0 ~ /: ENC\[/) e++; else p++ }
  END { print e + 0, p + 0 }' "$tmp")
[ "$plain" -eq 0 ] || die "$plain stringData value(s) not encrypted; nothing written"
[ "$enc" -eq "$count" ] || die "$enc encrypted value(s) for $count entered; nothing written"
mv "$tmp" "$path"
trap - EXIT
printf 'wrote %s: %d encrypted value(s) for secret %s/%s\n' "$path" "$count" "$namespace" "$name"
