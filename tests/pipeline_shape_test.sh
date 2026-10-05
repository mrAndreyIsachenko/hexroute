#!/usr/bin/env bash
# A gate must not pipe into a command that stops reading.
#
# These scripts run under `set -euo pipefail`, and `grep -q` exits on its first
# match. When the input is larger than the pipe buffer — 16 KiB to start with on
# macOS — the writer is still writing, takes SIGPIPE, and `pipefail` makes the
# pipeline's status 141. The condition then reads as "no match" although the
# match was found, so the gate makes a false claim about the repository rather
# than reporting that it could not answer.
#
# Measured 2026-10-05: `tests/roadmap_drift_test.sh` failed on a runner with
# "no change is open, but the roadmap does not say so" while printing, in the
# same output, the section it had read with `None.` on its own line. The Active
# Changes section is about 49 KiB. The same pipeline returns 0 at 1 KiB and 141
# at 2.5 MiB, so the size of the input decides whether a gate tells the truth.
#
# Two things are checked. No script under `pipefail` pipes into `grep -q`, and
# the form that replaced those pipelines answers correctly on an input no pipe
# buffer can hold.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"
failed=0

# --- No pipefail script pipes into a reader that may stop early -------------
for script in tests/*.sh scripts/*.sh scripts/*/*.sh; do
  [ -f "$script" ] || continue
  grep -q 'set -[a-z]*o pipefail\|set -euo pipefail' "$script" || continue
  if offenders="$(grep -nE '\| *grep -[a-zA-Z]*q' "$script")"; then
    printf '%s pipes into grep -q under pipefail:\n%s\n' "$script" "$offenders" >&2
    failed=1
  fi
done

if [ "$failed" -ne 0 ]; then
  printf '\n' >&2
  printf 'A matched grep reads as no match when the writer is cut off mid-write.\n' >&2
  printf 'Use a here-string for a variable, or drop -q so grep drains the pipe.\n' >&2
  exit 1
fi

# --- The replacement form answers on an input no pipe buffer can hold -------
# The match is the first line, which is where an early-exiting reader does the
# most damage, and the input is far past any pipe buffer.
big="None.
$(for _ in $(seq 1 40000); do printf 'filler that pushes this past any pipe buffer\n'; done)"
if [ "${#big}" -lt 1000000 ]; then
  printf 'the fixture is %d bytes, which is not past a pipe buffer\n' "${#big}" >&2
  exit 1
fi
if ! grep -qiE '^None\.|^None$' <<<"$big"; then
  printf 'the here-string form missed a match it was given\n' >&2
  exit 1
fi

printf 'ok: no gate pipes into a reader that stops early (%d bytes answered)\n' \
  "${#big}"
