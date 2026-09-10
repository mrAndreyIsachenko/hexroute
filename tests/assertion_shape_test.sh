#!/usr/bin/env bash
# An assertion in a gate must be able to fail.
#
# macOS ships bash 3.2, and in it `set -e` does not fire on a failing `[[ ]]`.
# A conditional written as a statement evaluates, reports false, and the script
# runs on to its success message. `make check` is the only job that runs the
# shell gates and it runs on macOS, so an assertion in that shape has never been
# enforced on any runner.
#
# Thirty-three of them stood in ten gates. Converting them found exactly one
# that was false — an assertion added the day before, about where an installer
# asks its question — which is the whole argument for this gate: the shape hides
# the one that matters among the ones that happen to hold.
#
# Two shapes are left alone deliberately. A conditional heading an `&&` list is
# inert too, but in these scripts every one is control flow — `break`,
# `continue`, a flag — and rewriting it would change what the script does. A
# conditional that is the last statement of a function is that function's
# return value, and a caller of it does fail.
set -euo pipefail

fail() {
  printf 'assertion-shape: %s\n' "$1" >&2
  exit 1
}

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

# The behaviour this gate exists for, measured rather than assumed. If a future
# runner's shell does fire on a bare conditional, this says so and the gate can
# be retired rather than left as folklore.
if bash -c 'set -euo pipefail; [[ 1 == 2 ]]; exit 0' 2>/dev/null; then
  inert="yes"
else
  inert="no"
fi

/usr/bin/python3 - "$inert" <<'PY'
import glob
import os
import sys

inert = sys.argv[1]
offenders = []

for path in sorted(glob.glob("tests/*.sh")):
    lines = open(path).read().splitlines()
    for index, line in enumerate(lines):
        stripped = line.strip()
        if not (stripped.startswith("[[") and stripped.endswith("]]")):
            continue
        # The last statement of a function is its return value, and whoever
        # calls it does fail on a false one.
        following = ""
        for later in lines[index + 1:]:
            if later.strip():
                following = later
                break
        if following == "}":
            continue
        offenders.append((path, index + 1, stripped))

if offenders:
    print("a bare conditional cannot fail the gate it is written in:")
    for path, line, text in offenders:
        print("  %s:%d  %s" % (path, line, text))
    print("")
    print("Give each one something to do when it fails, so that it names itself:")
    print("  [[ -f \"$plist\" ]] || fail \"[[ -f '$plist' ]]\"")
    raise SystemExit(1)

print("assertion-shape: %d gates, no assertion that cannot fail "
      "(bare conditional inert here: %s)" % (len(glob.glob("tests/*.sh")), inert))
PY
