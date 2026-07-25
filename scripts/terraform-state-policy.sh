#!/usr/bin/env bash
set -euo pipefail

root="${1:-$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)}"
root="$(cd "$root" && pwd -P)"
artifacts=()

while IFS= read -r -d '' path; do
  artifacts+=("${path#"$root"/}")
done < <(
  find "$root" \
    -path "$root/.git" -prune -o \
    -path '*/.terraform' -prune -o \
    \( -type f -o -type l \) \
    \( \
      -name '*.tfstate' -o \
      -name '*.tfstate.*' -o \
      -name '.terraform.tfstate.lock.info' -o \
      -name '*.tfplan' -o \
      -name 'crash.log' -o \
      -name 'crash.*.log' \
    \) \
    -print0
)

if [[ "${#artifacts[@]}" -gt 0 ]]; then
  printf 'local Terraform artifacts are prohibited under %s:\n' "$root" >&2
  printf '  %s\n' "${artifacts[@]}" >&2
  exit 1
fi

printf 'ok: no local Terraform state, plan or crash artifacts\n'
