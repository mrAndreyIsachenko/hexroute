#!/usr/bin/env bash
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
ROOT_DIR="$HOME/Library/Application Support/Hexroute/policy-qualification"
BINARY="$ROOT_DIR/bin/hexroute-policy-qualification"
ROOT_SOCKET="/var/run/hexroute-observe/hexrouted.sock"
USER_SOCKET="$HOME/Library/Application Support/Hexroute/observe-user/state/userd.sock"
TEMP_DIR="$(/usr/bin/mktemp -d /private/tmp/hexroute-policy-faults.XXXXXX)"
trap '/bin/rm -rf "$TEMP_DIR"' EXIT

[[ -x "$BINARY" ]] || { printf 'error: qualification agent is not installed\n' >&2; exit 1; }

import_fault() {
  local kind="$1"
  local package="$2"
  local test_name="$3"
  local report="$TEMP_DIR/$kind.txt"
  (
    cd "$REPO_ROOT"
    go test "$package" -run "^$test_name$" -count=1 -v
  ) >"$report"
  "$BINARY" import-fault \
    --root "$ROOT_DIR" \
    --root-socket "$ROOT_SOCKET" \
    --user-socket "$USER_SOCKET" \
    --interval 60s \
    --max-gap 180s \
    --kind "$kind" \
    --report "$report"
}

import_fault invalid_signature ./internal/signing TestVerifierRejectsInvalidSignatureAndStaleTimestamp
import_fault selector_conflict ./internal/policy TestComposeRejectsCompleteConflictingCandidateWithRedactedReport
import_fault stale_generation ./internal/policy TestGenerationRejectedOperatorResumeLeaseCannotReplay
import_fault cross_domain_crash ./internal/ctl TestPolicyCoordinatorConvergesAcrossDeterministicCrashBoundaries

printf 'ok: four controlled policy fault outcomes imported\n'
