#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")/.."

imports="$(go list -f '{{join .Imports "\n"}}' ./internal/resumeplan ./internal/resumeexecutor)"
for forbidden in \
  github.com/mrAndreyIsachenko/hexroute/internal/command \
  github.com/mrAndreyIsachenko/hexroute/internal/credentials \
  github.com/mrAndreyIsachenko/hexroute/internal/observe \
  github.com/mrAndreyIsachenko/hexroute/internal/pritunlplan \
  github.com/mrAndreyIsachenko/hexroute/internal/pritunlrescue \
  github.com/mrAndreyIsachenko/hexroute/internal/routeplan \
  github.com/mrAndreyIsachenko/hexroute/internal/rootdaemon \
  github.com/mrAndreyIsachenko/hexroute/internal/userdaemon \
  github.com/mrAndreyIsachenko/hexroute/internal/userobserve \
  net \
  os \
  os/exec \
  syscall; do
  if printf '%s\n' "$imports" | rg -x --fixed-strings "$forbidden" >/dev/null; then
    printf 'error: operator resume imports forbidden execution path: %s\n' "$forbidden" >&2
    exit 1
  fi
done

printf 'ok: operator resume plan and executor are isolated from data-plane execution paths\n'
