#!/usr/bin/env bash
# The coverage comparison must know every role the planner does.
#
# It decides whether a tunnel handover is safe: a destination it cannot place on
# one side of the tunnel or the other is a destination whose treatment nobody
# compared. When `inherited` was added to the planner and not to it, it reported
# five disagreements that did not exist — on the very run that was meant to prove
# the handover safe.
#
# Comparing the two lists is the whole of this gate. It does not check that the
# comparison is right, only that it has not been left behind.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"

planner=internal/routeplan/planner.go
comparison=scripts/ops/tunnel-route-coverage.py

for file in "$planner" "$comparison"; do
  [ -f "$file" ] || { printf '%s is missing\n' "$file" >&2; exit 1; }
done

# The roles the planner accepts, from their declarations.
planner_roles="$(sed -n 's/^\tRole[A-Za-z]* *Role = "\([a-z_]*\)".*/\1/p' "$planner" | sort -u)"
# The roles the comparison can place.
comparison_roles="$(sed -n 's/.*role == "\([a-z_]*\)".*/\1/p;s/.*role in ("\([a-z_]*\)", "\([a-z_]*\)").*/\1\n\2/p' "$comparison" | sort -u)"

if [ -z "$planner_roles" ]; then
  printf 'no roles were read from %s; this gate is not looking at anything\n' "$planner" >&2
  exit 1
fi

missing="$(comm -23 <(printf '%s\n' "$planner_roles") <(printf '%s\n' "$comparison_roles"))"
extra="$(comm -13 <(printf '%s\n' "$planner_roles") <(printf '%s\n' "$comparison_roles"))"

failed=0
if [ -n "$missing" ]; then
  printf 'the coverage comparison cannot place these roles: %s\n' "$(echo $missing)" >&2
  printf '  it would report them as disagreeing with the supervisor when they do not\n' >&2
  failed=1
fi
if [ -n "$extra" ]; then
  printf 'the coverage comparison knows roles the planner does not: %s\n' "$(echo $extra)" >&2
  failed=1
fi
[ "$failed" = 0 ] || exit 1
printf 'ok: the coverage comparison knows every role the planner does\n'
