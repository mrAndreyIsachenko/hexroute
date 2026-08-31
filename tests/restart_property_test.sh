#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"

export GOCACHE="${GOCACHE:-${TMPDIR:-/tmp}/hexroute-go-cache}"

# Four of the defects the shadow soak found were the same shape: a component
# that starts clean, runs correctly, and misbehaves only when a second process
# opens what the first one left behind. Every one of them cost a live restart
# on the operator's own machine to find, because no test started twice.
#
# internal/restartguard holds the property. This census keeps its list honest:
# a connectivity package that writes durable state and is named nowhere is a
# component that would arrive uncovered, which is exactly how the last four
# arrived.
#
# `covered` is asserted by the guard itself. `elsewhere` is a package whose
# restart behavior is proved by its own tests; each entry names them, so the
# claim can be checked rather than believed.

guard=internal/restartguard/restart_test.go

covered=(
  connectivityjournal
  connectivityhost
  connectivityqualification
  connectivitywatch
)

elsewhere=(
  # connectivitycheckpoint: the lineage is the thing these tests are about,
  # and it is restarted in each of them.
  #   TestASilentRestartIsStillRefused
  #   TestARecordedRestartIsAcceptedAndSaysWhatItAbandoned
  #   TestARestartMustNameTheLineageItActuallyAbandons
  connectivitycheckpoint

  # connectivitysoak: the harness that restarts other components. Its own
  # scratch root is created per session and is not meant to outlive it; what
  # its tests prove is that the reopen it drives resumes what was left.
  #   TestAnUndamagedLineageOfTheSameShapeResumesLatest
  #   TestEachStoreFaultIsRefusedOnItsOwnTerms
  connectivitysoak
)

contains() {
  local needle="$1"
  shift
  local item
  for item in "$@"; do
    [ "$item" = "$needle" ] && return 0
  done
  return 1
}

[ -f "$guard" ] || {
  printf '%s is missing; the restart property has no home\n' "$guard" >&2
  exit 1
}

status=0

# Every connectivity package that keeps durable state is covered or accounted
# for. Writing to disk is the tell: a package that only reads cannot carry
# anything across a restart.
for directory in internal/connectivity*/; do
  package="$(basename "$directory")"
  compgen -G "$directory*.go" >/dev/null || continue
  writes=0
  for source in "$directory"*.go; do
    case "$source" in
    *_test.go) continue ;;
    esac
    if grep -qE 'os\.(OpenFile|WriteFile|Rename)' "$source"; then
      writes=1
      break
    fi
  done
  if [ "$writes" -eq 0 ]; then
    if contains "$package" "${covered[@]}" || contains "$package" "${elsewhere[@]}"; then
      printf 'internal/%s is listed as keeping durable state but writes nothing\n' \
        "$package" >&2
      status=1
    fi
    continue
  fi
  if contains "$package" "${elsewhere[@]}"; then
    continue
  fi
  if ! contains "$package" "${covered[@]}"; then
    printf 'internal/%s writes durable state and is in neither list\n' "$package" >&2
    printf '  add it to internal/restartguard, or record which tests restart it\n' >&2
    status=1
    continue
  fi
  # A package the guard claims to cover must actually be named in it.
  if ! grep -q "internal/$package\"" "$guard" \
    && ! grep -q "\"$package" "$guard"; then
    printf 'internal/%s is listed as covered but %s never names it\n' \
      "$package" "$guard" >&2
    status=1
  fi
done

# A listed package that no longer exists leaves the list lying about the tree.
for package in "${covered[@]}" "${elsewhere[@]}"; do
  [ -d "internal/$package" ] || {
    printf 'internal/%s is listed here but no longer exists\n' "$package" >&2
    status=1
  }
done

# The tests named as discharging an exemption have to exist.
for named in \
  connectivitycheckpoint:TestASilentRestartIsStillRefused \
  connectivitycheckpoint:TestARecordedRestartIsAcceptedAndSaysWhatItAbandoned \
  connectivitycheckpoint:TestARestartMustNameTheLineageItActuallyAbandons \
  connectivitysoak:TestAnUndamagedLineageOfTheSameShapeResumesLatest \
  connectivitysoak:TestEachStoreFaultIsRefusedOnItsOwnTerms; do
  package="${named%%:*}"
  test_name="${named##*:}"
  if ! grep -rqE "func $test_name\(" "internal/$package"/*_test.go 2>/dev/null; then
    printf '%s is named as covering internal/%s and does not exist\n' \
      "$test_name" "$package" >&2
    status=1
  fi
done

[ "$status" -eq 0 ] || exit 1

go test ./internal/restartguard/ >/dev/null

printf 'ok: every connectivity package keeping durable state is restarted twice (%d covered, %d elsewhere)\n' \
  "${#covered[@]}" "${#elsewhere[@]}"
