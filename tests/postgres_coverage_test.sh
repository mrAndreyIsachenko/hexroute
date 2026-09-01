#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"

# A test that needs PostgreSQL skips without a DSN, and a package whose tests
# all skip still prints `ok`. So a package left out of the PostgreSQL gate is
# invisible twice over: absent from a run that needs docker, and indistinguish-
# able from a passing one when it is present.
#
# internal/cloudconnectivity was left out this way. Its two tests had never run
# on any machine, and one of them is the only mechanized form of the claim that
# the projection schema refuses an address, a path or a digest.
#
# This runs without docker, so the wiring is checked on every gate rather than
# only where the database is available.

gate=tests/postgres_migrations_test.sh
[ -f "$gate" ] || {
  printf '%s is missing\n' "$gate" >&2
  exit 1
}

python3 - "$gate" <<'PY'
import os
import re
import sys

gate = sys.argv[1]
script = open(gate).read()

# Each block is `go test [-v] ./internal/<package> [flags] -run '<pattern>'`,
# with the pattern optional: a package run without one runs everything.
blocks = {}
for match in re.finditer(
        r"go test\s+(?:-\S+\s+)*\./internal/(\w+)\s*\\?\s*"
        r"(?:\n\s*)?(?:-run\s+'([^']*)'|-run\s+(\S+))?",
        script):
    package = match.group(1)
    pattern = match.group(2) if match.group(2) is not None else match.group(3)
    blocks.setdefault(package, []).append(pattern)

needle = "PostgreSQL integration DSNs are not configured"
status = 0
for root, directories, files in os.walk("internal"):
    directories[:] = [d for d in directories if not d.startswith(".")]
    package = os.path.basename(root)
    wanted = []
    for name in files:
        if not name.endswith("_test.go"):
            continue
        source = open(os.path.join(root, name)).read()
        if needle not in source:
            continue
        # Per function, not per file. A pure unit test living beside the
        # integration ones needs no DSN and runs in the ordinary gate; the
        # tests this is about are the ones that skip themselves without one.
        starts = [(match.start(), match.group(1))
                  for match in re.finditer(r"^func (Test\w+)\(", source, re.M)]
        for index, (offset, test) in enumerate(starts):
            end = starts[index + 1][0] if index + 1 < len(starts) else len(source)
            if needle in source[offset:end]:
                wanted.append(test)
    if not wanted:
        continue
    if package not in blocks:
        print(f"internal/{package} has PostgreSQL tests and the gate never "
              f"runs it, so they skip on every machine", file=sys.stderr)
        status = 1
        continue
    for test in sorted(set(wanted)):
        # Go matches -run unanchored, and a block with no pattern runs all.
        if any(pattern is None or re.search(pattern, test)
               for pattern in blocks[package]):
            continue
        print(f"{test} in internal/{package} is not matched by any -run "
              f"pattern in {gate}, so it never runs", file=sys.stderr)
        status = 1

sys.exit(status)
PY

printf 'ok: every PostgreSQL-gated test is actually run by the PostgreSQL gate\n'
