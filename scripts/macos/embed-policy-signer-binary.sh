#!/usr/bin/env bash
set -euo pipefail

if [[ $# -ne 1 || -z "$1" ]]; then
  printf 'usage: %s OUTPUT\n' "$0" >&2
  exit 2
fi

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
output="$1"

go_bin=""
for candidate in /opt/homebrew/bin/go /usr/local/bin/go; do
  if [[ -x "$candidate" ]]; then
    go_bin="$candidate"
    break
  fi
done
if [[ -z "$go_bin" ]]; then
  printf 'error: Go toolchain not found\n' >&2
  exit 1
fi

mkdir -p "$(dirname "$output")"
cd "$repo_root"
CGO_ENABLED=1 GOCACHE="${HEXROUTE_POLICY_GOCACHE:-/private/tmp/hexroute-policy-go-cache}" \
  "$go_bin" build -trimpath -o "$output" ./cmd/hexroute-policy
chmod 0755 "$output"
