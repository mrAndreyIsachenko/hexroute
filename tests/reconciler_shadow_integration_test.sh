#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"

export GOCACHE="${GOCACHE:-${TMPDIR:-/tmp}/hexroute-go-cache}"

go test ./internal/reconciler \
  -run 'TestShadowCloudLossAndEngineFailureDoNotMutateProtectedState|TestShadowPeersDisableCrossDomain|TestShadowIPCAuthenticatesPeer' \
  -count=1

dependencies="$(go list -deps ./internal/reconciler)"
for forbidden in \
  github.com/mrAndreyIsachenko/hexroute/internal/cloudruntime \
  github.com/mrAndreyIsachenko/hexroute/internal/cloudingest \
  github.com/mrAndreyIsachenko/hexroute/internal/database \
  github.com/mrAndreyIsachenko/hexroute/internal/databasemigrate \
  github.com/mrAndreyIsachenko/hexroute/internal/command \
  github.com/mrAndreyIsachenko/hexroute/internal/credentials \
  github.com/mrAndreyIsachenko/hexroute/internal/observe \
  github.com/mrAndreyIsachenko/hexroute/internal/pritunlplan \
  github.com/mrAndreyIsachenko/hexroute/internal/pritunlrescue \
  github.com/mrAndreyIsachenko/hexroute/internal/routeplan \
  github.com/mrAndreyIsachenko/hexroute/internal/rootdaemon \
  github.com/mrAndreyIsachenko/hexroute/internal/userdaemon \
  github.com/mrAndreyIsachenko/hexroute/internal/userobserve \
  net/http \
  os/exec; do
  if printf '%s\n' "$dependencies" | rg -x --fixed-strings "$forbidden" >/dev/null; then
    printf 'error: reconciler shadow integration imports forbidden path: %s\n' "$forbidden" >&2
    exit 1
  fi
done

printf 'ok: reconciler shadow loss/failure paths are offline and production-state neutral\n'
