#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
doc="$repo_root/docs/testing/reconciler-qualification.md"
makefile="$repo_root/Makefile"

test -s "$doc"

for term in \
  'make check' \
  'make test' \
  'make race' \
  'make fuzz' \
  'fuzz-smoke' \
  'crash-recovery' \
  'replay' \
  'IPC' \
  'schema-boundary' \
  'secret-canary' \
  'capability-leak' \
  'does not enable production adapters' \
  'Twilight stays the production owner' \
  'separate grill session' \
  'OpenSpec change' \
  'independently executable rollback' \
  'Readiness is not raw health' \
  '`ready`, `temporarily_blocked` and `denied`' \
  'semantic no-op was proven' \
  'Telemetry gap repair is upload-only' \
  '`telemetry_gap_unrecoverable`' \
  'WithGapRepairEnabled(false)' \
  'TestUploaderCanDisableGapRepairWithoutDisablingBaseUpload' \
  'TestStartupSurfaceRequiresExplicitSyntheticFeatureGate' \
  'TestShadowCloudLossAndEngineFailureDoNotMutateProtectedState' \
  'rollback does not stop, restart or reconfigure production networking'; do
  grep -Fq "$term" "$doc"
done

grep -Fq 'fuzz:' "$makefile"
grep -Eq '^check: .* fuzz ' "$makefile"
grep -Fq 'tests/reconciler_qualification_documentation_test.sh' "$makefile"
grep -Fq 'docs/testing/reconciler-qualification.md' "$repo_root/README.md"

printf 'ok: reconciler qualification documentation and gate wiring are complete\n'
