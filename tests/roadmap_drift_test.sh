#!/usr/bin/env bash
# The roadmap must agree with the repository about what is open and what is owed.
#
# This gate exists because it did not agree. On 2026-09-04 docs/roadmap.md still
# called `add-observable-connectivity-state-machine` active and
# `add-local-event-archive` unstarted; both had been archived the day before.
# Its debt section still listed `incidentbundle`, which had left the unwired
# list when the maintenance worker began calling it. Every other claim in this
# repository is gated — spec drift, package reachability, unbuilt constructors —
# and the one document a reader opens to ask "what is next" was gated by
# nothing, so it answered with a state that had not existed for days.
#
# Two things are checked, both mechanical. The Active Changes section must name
# exactly the changes OpenSpec reports as open. The Debt section must name
# exactly the packages tests/package_reachability_test.sh records as unwired.
# Prose around them is free; the names are not.

set -euo pipefail
cd "$(dirname "$0")/.."

roadmap=docs/roadmap.md
[ -f "$roadmap" ] || { printf '%s is missing\n' "$roadmap" >&2; exit 1; }

failed=0

# --- Active Changes -----------------------------------------------------
open_changes=$(OPENSPEC_TELEMETRY=0 openspec list --json 2>/dev/null \
	| python3 -c 'import json,sys
try:
    data = json.load(sys.stdin)
except Exception:
    sys.exit(0)
items = data if isinstance(data, list) else data.get("changes", [])
for item in items:
    name = item.get("name") if isinstance(item, dict) else item
    if name:
        print(name)' | sort)

section=$(sed -n '/^## Active Changes$/,/^## /p' "$roadmap")
if [ -z "$open_changes" ]; then
	printf '%s' "$section" | grep -qiE '^None\.|^None$' || {
		printf 'no change is open, but the roadmap does not say so\n' >&2
		failed=1
	}
else
	while read -r name; do
		[ -z "$name" ] && continue
		printf '%s' "$section" | grep -qF "$name" || {
			printf 'change %s is open and the roadmap does not name it\n' \
				"$name" >&2
			failed=1
		}
	done <<< "$open_changes"
fi

# --- Debt ---------------------------------------------------------------
# The unwired list is the authority; the roadmap must name each entry and
# nothing that has left it.
unwired=$(sed -n '/^unwired=(/,/^)/p' tests/package_reachability_test.sh \
	| sed -e 's/#.*//' -e 's/[[:space:]]//g' \
	| grep -vE '^unwired=\(|^\)$' | grep -v '^$' | sort -u)

debt=$(sed -n '/^## Debt$/,$p' "$roadmap")
for package in $unwired; do
	printf '%s' "$debt" | grep -qF "\`$package\`" || {
		printf '%s is unwired and the roadmap does not list it as debt\n' \
			"$package" >&2
		failed=1
	}
done

# A package named as present debt that is no longer unwired. Only the bullet
# lines count: the closing paragraph records what has left, by design.
bullets=$(printf '%s' "$debt" | grep '^- ')
for name in $(printf '%s' "$bullets" | grep -oE '`[a-z][a-z0-9]*`' | tr -d '`' | sort -u); do
	printf '%s\n' "$unwired" | grep -qFx "$name" || {
		printf '%s is listed as debt and is no longer unwired\n' "$name" >&2
		failed=1
	}
done

[ "$failed" -ne 0 ] && { printf '\nUpdate %s.\n' "$roadmap" >&2; exit 1; }
printf 'ok: the roadmap agrees with the open changes and the unwired list\n'
