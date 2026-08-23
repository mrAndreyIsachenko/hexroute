#!/usr/bin/env bash
set -euo pipefail

root="${1:-${OPENSPEC_DRIFT_ROOT:-.}}"
changes_dir="$root/openspec/changes"
specs_dir="$root/openspec/specs"

usage() {
  cat <<'USAGE'
usage: scripts/openspec-drift-check.sh [ROOT]

Fails when the OpenSpec store has drifted from the repository:

  - a change whose tasks are all complete is still sitting in
    openspec/changes/ instead of being synced into the baseline and archived
  - an in-progress change already has its ADDED requirements in the baseline

`openspec validate --strict` cannot see either state: both stores are
individually well-formed. Reconcile with the openspec-reconcile skill.

Environment:
  OPENSPEC_DRIFT_ROOT   repository root to inspect (default: .)
  OPENSPEC_DRIFT_ALLOW  set to 1 to bypass during an active outage
USAGE
}

case "${1:-}" in
  -h|--help) usage; exit 0 ;;
esac

if [ "${OPENSPEC_DRIFT_ALLOW:-0}" = "1" ]; then
  echo "openspec-drift-check: bypassed via OPENSPEC_DRIFT_ALLOW=1"
  exit 0
fi

if [ ! -d "$changes_dir" ]; then
  echo "openspec-drift-check: no OpenSpec store at $changes_dir" >&2
  exit 1
fi

# Requirement titles declared by a change's delta specs.
# section=ADDED lists only added requirements; section=ALL lists every
# requirement outside a REMOVED block.
delta_requirements() {
  local file="$1" mode="$2"
  awk -v mode="$mode" '
    /^## / { section = $0; next }
    /^### Requirement: / {
      title = substr($0, 18)
      if (mode == "ADDED" && section !~ /ADDED/) next
      if (mode == "ALL" && section ~ /REMOVED/) next
      print title
    }
  ' "$file"
}

drift=0
warned=0

for change in "$changes_dir"/*/; do
  name="$(basename "$change")"
  [ "$name" = "archive" ] && continue
  [ -d "$change" ] || continue

  tasks="$change/tasks.md"
  [ -f "$tasks" ] || continue

  open="$(grep -c '^- \[ \]' "$tasks" || true)"
  closed="$(grep -c '^- \[x\]' "$tasks" || true)"

  if [ "$open" -eq 0 ] && [ "$closed" -gt 0 ]; then
    drift=1
    echo "drift: $name has $closed/$closed tasks complete but is not archived"
    for delta in "$change"specs/*/spec.md; do
      [ -f "$delta" ] || continue
      capability="$(basename "$(dirname "$delta")")"
      baseline="$specs_dir/$capability/spec.md"
      if [ ! -f "$baseline" ]; then
        echo "       capability '$capability' has no baseline spec"
        continue
      fi
      missing=0
      while IFS= read -r title; do
        grep -qxF "### Requirement: $title" "$baseline" || missing=$((missing + 1))
      done < <(delta_requirements "$delta" ALL)
      [ "$missing" -gt 0 ] &&
        echo "       capability '$capability' is missing $missing delta requirement(s)"
    done
    continue
  fi

  if [ "$open" -gt 0 ]; then
    for delta in "$change"specs/*/spec.md; do
      [ -f "$delta" ] || continue
      capability="$(basename "$(dirname "$delta")")"
      baseline="$specs_dir/$capability/spec.md"
      [ -f "$baseline" ] || continue
      while IFS= read -r title; do
        if grep -qxF "### Requirement: $title" "$baseline"; then
          warned=1
          echo "warn:  $name has $open open task(s) but '$capability' already declares" \
               "the added requirement '$title'"
        fi
      done < <(delta_requirements "$delta" ADDED)
    done
  fi
done

if [ "$drift" -ne 0 ]; then
  cat >&2 <<'REMEDY'

The baseline specs no longer describe the implemented system. Run the
openspec-reconcile skill to sync the delta specs and archive the changes,
or set OPENSPEC_DRIFT_ALLOW=1 to bypass while restoring service.
REMEDY
  exit 1
fi

if [ "$warned" -ne 0 ]; then
  echo "openspec-drift-check: no blocking drift; review the warnings above"
  exit 0
fi

echo "openspec-drift-check: baseline specs and changes are consistent"
