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
  restartguard     # asserts durable state survives two process restarts
)

unwired=(
  # connectivityhost is the seam; it is reachable. Nothing else here is.

  # incidentbundle: nothing creates a bundle, so expiry has nothing to expire.
  # Creation needs private object storage configured outside this repository,
  # which is why wiring only the expiry job would be motion without effect.
  incidentbundle

  # Local capabilities held behind their own cutover gates.
  credentials     # opaque Keychain handles, for the user-domain cutover
  pritunlrescue   # typed rescue contract, for the OTP-watchdog cutover

  # resumeexecutor is not merely unconnected: the seam is complete. It already
  # satisfies operator.ResumePolicyExecutor, and the only thing missing is the
  # call that installs it — which also sets enforceResume, moving every
  # operator resume off the pre-enforcement path and onto this one. Wiring it
  # is a behaviour change for resume as a whole, not an added capability, and
  # that is what makes it a cutover rather than a connection.
  resumeexecutor

  # eventarchive is the durable local retention the host does not have. It is
  # complete and tested; what it lacks is a producer, and the reason is not a
  # cutover.
  #
  # The host emits no typed event stream today. internal/telemetry, which owns
  # upload and the acknowledgement that empties the spool, is reachable only
  # from cmd/hexroute-ingest — the cloud side — and telemetry.NewUploader is
  # called from tests and nowhere else. The one durable event producer on this
  # machine is the connectivity journal, and it cannot gain a second sink while
  # a qualification soak is measuring it.
  #
  # Connecting the archive to a stream that does not exist would make it
  # reachable and still never run, which is the distinction this list is for.
  eventarchive

  # policyadvisor turns repeated denials into an unsigned draft an operator
  # reviews by hand. It has no producer at all, and the evidence a producer
  # would read is not real yet: the reconciler shadow store this host runs is
  # declared synthetic-only, so a draft built from it would be a suggestion
  # derived from nothing that happened. Connecting it before that changes
  # would be motion without effect, which is the one thing this list exists to
  # keep from being mistaken for progress.
  policyadvisor
)

contains() {
  local needle="$1"; shift
  local item
  for item in "$@"; do
    [ "$item" = "$needle" ] && return 0
  done
  return 1
}

# A failure here must not be answered with a conclusion about the product.
#
# Discarding one binary's stderr and carrying on produces exactly the packages
# that binary alone reaches, reported as unreachable — which is how a transient
# `go list` failure on a runner once accused internal/connectivitywatch of
# being in no binary while cmd/hexroute-connectivity-watch imported it. A tool
# that could not answer is a different thing from an answer, and only one of
# them is worth failing a build over.
resolved=""
for binary in cmd/*/; do
  if ! output="$(go list -deps "./$binary" 2>&1)"; then
    printf 'go list could not resolve %s, so reachability is unknown\n' \
      "$binary" >&2
    printf 'this is a failure to compute the answer, not a finding about any package\n' >&2
    printf '%s\n' "$output" >&2
    exit 1
  fi
  resolved="$resolved$output
"
done

linked="$(printf '%s' "$resolved" | grep "hexroute/internal/" | sort -u)"

if [ -z "$linked" ]; then
  printf 'go list resolved every binary and named no internal package\n' >&2
  printf 'this is a broken query, not a repository without internal packages\n' >&2
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

# eventarchive's reason rests on no host binary reaching the upload path. If
# one does, the host has a stream to archive and the entry above stops being
# true.
#
# The host binaries are named rather than inferred. The first version of this
# check asked about a cmd/ that does not exist, so go list failed, the failure
# was discarded, and the check would have passed forever.
host_binaries=(hexrouted hexroute-userd hexroutectl hexroute-sentinel)

if contains eventarchive "${unwired[@]}"; then
  for host in "${host_binaries[@]}"; do
    [ -d "cmd/$host" ] || {
      printf 'cmd/%s is named as a host binary and does not exist\n' "$host" >&2
      status=1
      continue
    }
    if go list -deps "./cmd/$host" \
      | grep -qx "github.com/mrAndreyIsachenko/hexroute/internal/telemetry"; then
      printf 'cmd/%s now reaches the upload path\n' "$host" >&2
      printf '  eventarchive is unwired because the host emits no stream;\n' >&2
      printf '  connect it now, or record what still blocks it\n' >&2
      status=1
    fi
  done
fi

# policyadvisor's reason is the one entry here that rests on a fact elsewhere
# in the tree rather than on the package's own absence. Facts drift, so it is
# checked: when the shadow store stops being synthetic, the advisor's evidence
# becomes real and the entry above stops being true.
if contains policyadvisor "${unwired[@]}"; then
  if ! grep -q "synthetic-only and exports no execution path" \
    internal/rootdaemon/run.go; then
    printf 'the reconciler shadow store is no longer declared synthetic-only\n' >&2
    printf '  policyadvisor is unwired because its evidence was not real;\n' >&2
    printf '  connect it now, or record what still blocks it\n' >&2
    status=1
  fi
fi

# A listed package that no longer exists leaves the list lying about the tree.
for package in "${test_only[@]}" "${unwired[@]}"; do
  [ -d "internal/$package" ] || {
    printf 'internal/%s is listed here but no longer exists\n' "$package" >&2
    status=1
  }
done

[ "$status" -eq 0 ] || exit 1

# The cost of the debt is reported rather than left to be measured. A list of
# five names reads the same whether it is fifty lines or fifteen hundred, and
# the number is what says whether the promise is still worth keeping.
debt_lines=0
for package in "${unwired[@]}"; do
  for source in "internal/$package"/*.go; do
    case "$source" in
    *_test.go) continue ;;
    esac
    [ -f "$source" ] || continue
    lines="$(wc -l <"$source")"
    debt_lines=$((debt_lines + lines))
  done
done

printf 'ok: every internal package is in a binary or recorded as unwired (%d unwired, %d lines that have never run)\n' \
  "${#unwired[@]}" "$debt_lines"
