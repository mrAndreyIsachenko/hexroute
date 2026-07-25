#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
policy="$repo_root/scripts/terraform-state-policy.sh"
fixture="$(mktemp -d "${TMPDIR:-/tmp}/hexroute-state-policy.XXXXXX")"
trap 'rm -rf "$fixture"' EXIT

"$policy" "$repo_root"

mkdir -p "$fixture/.terraform" "$fixture/nested"
: >"$fixture/.terraform.lock.hcl"
: >"$fixture/.terraform/environment"
"$policy" "$fixture" >/dev/null

for artifact in \
  'nested/terraform.tfstate' \
  'nested/terraform.tfstate.backup' \
  'nested/change.tfplan' \
  'nested/.terraform.tfstate.lock.info' \
  'nested/crash.123.log'; do
  : >"$fixture/$artifact"
  if "$policy" "$fixture" >/dev/null 2>&1; then
    printf 'policy accepted prohibited artifact: %s\n' "$artifact" >&2
    exit 1
  fi
  rm "$fixture/$artifact"
done

printf 'terraform local-artifact policy passed\n'
