#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
dockerfile="$repo_root/Dockerfile"
dockerignore="$repo_root/.dockerignore"
compose_contract="$repo_root/deploy/container/compose.contract.yaml"

require_pattern() {
  local pattern="$1"
  local file="$2"
  if ! rg --quiet "$pattern" "$file"; then
    printf 'missing container contract pattern %q in %s\n' "$pattern" "$file" >&2
    exit 1
  fi
}

require_pattern \
  '^ARG GO_BUILDER_IMAGE=golang:1\.25\.12-alpine3\.23@sha256:[0-9a-f]{64}$' \
  "$dockerfile"
require_pattern '^FROM \$\{GO_BUILDER_IMAGE\} AS builder$' "$dockerfile"
require_pattern '^FROM scratch AS runtime$' "$dockerfile"
require_pattern '^USER 65532:65532$' "$dockerfile"
require_pattern '^ENTRYPOINT \["/usr/local/bin/hexroute-ingest"\]$' "$dockerfile"
require_pattern '^CMD \["--check"\]$' "$dockerfile"
require_pattern '^COPY cmd/hexroute-ingest ' "$dockerfile"
require_pattern '^COPY internal ' "$dockerfile"

if rg --quiet '^[[:space:]]*(ADD|RUN[[:space:]].*(apk|apt|curl|wget))[[:space:]]' \
  "$dockerfile"; then
  printf 'runtime build unexpectedly downloads or installs ad-hoc content\n' >&2
  exit 1
fi
if rg --quiet '^COPY[[:space:]]+\.[[:space:]]' "$dockerfile"; then
  printf 'Dockerfile copies the unrestricted repository context\n' >&2
  exit 1
fi

[[ "$(sed -n '1p' "$dockerignore")" == '**' ]] || {
  printf '.dockerignore must default-deny the build context\n' >&2
  exit 1
}
require_pattern '^!cmd/hexroute-ingest/\*\*$' "$dockerignore"
require_pattern '^!internal/\*\*$' "$dockerignore"

require_pattern '^[[:space:]]+read_only: true$' "$compose_contract"
require_pattern '^[[:space:]]+user: "65532:65532"$' "$compose_contract"
require_pattern '^[[:space:]]+- ALL$' "$compose_contract"
require_pattern '^[[:space:]]+- no-new-privileges:true$' "$compose_contract"
require_pattern '^[[:space:]]+- /tmp:rw,noexec,nosuid,nodev,size=16777216,uid=65532,gid=65532,mode=0700$' \
  "$compose_contract"
require_pattern '^[[:space:]]+api:$' "$compose_contract"
require_pattern '^[[:space:]]+worker:$' "$compose_contract"

printf 'ok: cloud container source contract is pinned and hardened\n'
