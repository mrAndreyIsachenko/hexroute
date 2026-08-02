#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"

dependencies="$(go list -deps ./cmd/hexroute-policy)"
for forbidden in \
  github.com/mrAndreyIsachenko/hexroute/internal/ipc \
  github.com/mrAndreyIsachenko/hexroute/internal/rootdaemon \
  github.com/mrAndreyIsachenko/hexroute/internal/userdaemon \
  github.com/mrAndreyIsachenko/hexroute/internal/routeplan \
  github.com/mrAndreyIsachenko/hexroute/internal/pritunlrescue \
  github.com/mrAndreyIsachenko/hexroute/internal/credentials; do
  if grep -Fxq "$forbidden" <<<"$dependencies"; then
    printf 'hexroute-policy imports forbidden runtime package: %s\n' "$forbidden" >&2
    exit 1
  fi
done

printf 'ok: hexroute-policy dependency graph is offline and mutation-free\n'
