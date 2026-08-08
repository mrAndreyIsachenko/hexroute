#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"

export GOCACHE="${GOCACHE:-${TMPDIR:-/tmp}/hexroute-go-cache}"

dependencies="$(go list -deps ./cmd/hexroute-policy-installer)"
for forbidden in \
  os/exec \
  net/http \
  github.com/mrAndreyIsachenko/hexroute/internal/ipc \
  github.com/mrAndreyIsachenko/hexroute/internal/rootdaemon \
  github.com/mrAndreyIsachenko/hexroute/internal/userdaemon \
  github.com/mrAndreyIsachenko/hexroute/internal/routeplan \
  github.com/mrAndreyIsachenko/hexroute/internal/pritunlplan \
  github.com/mrAndreyIsachenko/hexroute/internal/pritunlrescue \
  github.com/mrAndreyIsachenko/hexroute/internal/credentials \
  github.com/mrAndreyIsachenko/hexroute/internal/resumeexecutor; do
  if grep -Fxq "$forbidden" <<<"$dependencies"; then
    printf 'hexroute-policy-installer imports forbidden authority: %s\n' "$forbidden" >&2
    exit 1
  fi
done

installer=internal/policyinstaller/run.go
for contract in \
  '/Library/Application Support/Hexroute/observe-root/config/root-observe.json' \
  'Library/Application Support/Hexroute/observe-user/config/user-observe.json' \
  'policystore.InitializeRoot()' \
  'policystore.InitializeCurrentUser()' \
  'unix.O_NOFOLLOW' \
  'stat.Nlink != 1'; do
  if ! grep -Fq "$contract" "$installer"; then
    printf 'policy installer lost fixed-store or no-symlink contract: %s\n' "$contract" >&2
    exit 1
  fi
done

./bin/hexroute-policy-installer --check
printf 'ok: policy installer is fixed-store, no-symlink, and mutation-authority isolated\n'
