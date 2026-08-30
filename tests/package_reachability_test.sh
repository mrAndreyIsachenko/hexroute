#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"

export GOCACHE="${GOCACHE:-${TMPDIR:-/tmp}/hexroute-go-cache}"

# A package no binary contains has never run outside `go test`. Tests can prove
# a package behaves; only a binary can prove the product does anything with it.
# This repository has shipped whole subsystems that passed every gate and were
# reachable from nothing, so reachability is checked here rather than assumed.
#
# Two lists below. `test_only` is packages that are not supposed to be in a
# binary — guards that exist to be run by `go test`. `unwired` is debt: written,
# gated as done, and not connected. An entry there is a promise to connect it,
# and removing one is how that promise is kept.
#
# Adding a package to either list is a decision someone has to write down. That
# is the point: a new package cannot become unreachable quietly.

test_only=(
  secretguard      # canary fixtures; asserts serializers refuse secrets
  repositoryguard  # asserts the public-repository boundary over the work tree
)

unwired=(
  # connectivityhost is the seam; it is reachable. Nothing else here is.

  # Cloud features the worker does not schedule. docs/roadmap.md claims the
  # cloud implements SLOs and incident bundles; the worker runs neither job.
  slo
  incidentbundle

  # Local capabilities held behind their own cutover gates.
  credentials     # opaque Keychain handles, for the user-domain cutover
  pritunlrescue   # typed rescue contract, for the OTP-watchdog cutover
  resumeexecutor  # operator resume enforcement
  policyadvisor   # redacted policy observability
)

contains() {
  local needle="$1"; shift
  local item
  for item in "$@"; do
    [ "$item" = "$needle" ] && return 0
  done
  return 1
}

linked="$(for binary in cmd/*/; do
  go list -deps "./$binary" 2>/dev/null
done | grep "hexroute/internal/" | sort -u)"

if [ -z "$linked" ]; then
  printf 'could not resolve binary dependencies\n' >&2
  exit 1
fi

status=0

# Every package is either reachable from a binary or written down as one of the
# two exceptions.
for directory in internal/*/; do
  package="$(basename "$directory")"
  compgen -G "$directory*.go" >/dev/null || continue
  if printf '%s\n' "$linked" \
    | grep -qx "github.com/mrAndreyIsachenko/hexroute/internal/$package"; then
    if contains "$package" "${unwired[@]}"; then
      printf 'internal/%s is now reachable from a binary; remove it from the unwired list\n' \
        "$package" >&2
      status=1
    fi
    if contains "$package" "${test_only[@]}"; then
      printf 'internal/%s is declared test-only but a binary links it\n' "$package" >&2
      status=1
    fi
    continue
  fi
  if contains "$package" "${test_only[@]}" || contains "$package" "${unwired[@]}"; then
    continue
  fi
  printf 'internal/%s is in no binary and is on neither list\n' "$package" >&2
  printf '  connect it to a cmd/, or record why it is unreachable\n' >&2
  status=1
done

# A listed package that no longer exists leaves the list lying about the tree.
for package in "${test_only[@]}" "${unwired[@]}"; do
  [ -d "internal/$package" ] || {
    printf 'internal/%s is listed here but no longer exists\n' "$package" >&2
    status=1
  }
done

[ "$status" -eq 0 ] || exit 1

printf 'ok: every internal package is in a binary or recorded as unwired (%d unwired)\n' \
  "${#unwired[@]}"
