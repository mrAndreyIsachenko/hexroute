#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"

# A failing gate is cheap: it names the thing and stops. The expensive kind
# passes while checking less than its name claims, because nothing ever asks it
# to prove otherwise.
#
# Three shapes of that were found here within two days, green the whole time:
#
#   a test named for surviving a process that never restarted one;
#   a test whose important half was skipped because the fixture it built itself
#     could not produce the input, reported as one skipped test rather than as
#     one half that had never run;
#   a shell check written so that set -e ended the script before the line that
#     would have caught the failure.
#
# The first needs someone to read a name against a body. The other two are
# mechanical, and so is the package-level form of the second.

python3 - <<'PY'
import os
import re
import sys

status = 0


def fail(message):
    global status
    print(message, file=sys.stderr)
    status = 1


# --- 1. A skip must be about the environment, not about the fixture ---------
#
# A test that skips because this machine cannot run it is honest: nothing was
# checked and nothing could be. A test that skips because the fixture it built
# itself did not produce the input checked nothing and says so in a way that
# reads identically.
#
# Recorded reasons are environmental. A new one has to be written down here,
# which is the moment the question gets asked.
ENVIRONMENTAL = {
    "PostgreSQL integration DSNs are not configured",
    "PostgreSQL integration DSNs are not set",
    "HEXROUTE_TEST_POSTGRES_ADMIN_DSN is not set",
    "no continuous clock on this platform",
    "no continuous clock: %v",
    "sing-box is not installed",
    "running as root cannot demonstrate a foreign owner",
    "no supplemental group available",
    "no second group to inherit from",
    "cannot set a differing parent group: %v",
    "cannot assign supplemental group: %v",
}

# Packages whose every test needs something this machine may not have. They
# print ok when it is absent, so each names the gate that does run it — the
# claim is checkable rather than a shrug.
INTEGRATION_ONLY = {
    "internal/retention": "make postgres-test",
    "internal/databasemigrate": "make postgres-test",
}

skip_pattern = re.compile(r"t\.Skipf?\(\s*\"([^\"]*)\"")

for root, directories, files in os.walk("."):
    directories[:] = [d for d in directories
                      if not d.startswith(".") and d not in ("vendor",)]
    for name in files:
        if not name.endswith("_test.go"):
            continue
        path = os.path.join(root, name).lstrip("./")
        for number, line in enumerate(open(path), start=1):
            found = skip_pattern.search(line)
            if not found:
                continue
            if found.group(1) in ENVIRONMENTAL:
                continue
            fail(f"{path}:{number} skips for a reason not recorded as "
                 f"environmental:\n  {found.group(1)}\n"
                 f"  a fixture that cannot produce its input is a broken "
                 f"fixture, not a reason\n  to check nothing — fail instead, "
                 f"or record the reason in this guard")

# --- 2. A shell check that set -e reaches first -----------------------------
#
# A bare command followed by `status=$?` never reaches the second line when the
# command fails: errexit ends the script first. The check reads as a check and
# is unreachable in the only case it exists for.
#
# errexit state is tracked rather than guessed from the previous line, because
# the legitimate form of this puts `set +e` above a command that spans several
# lines, and a guard that looked one line back would call every one of those a
# defect.
capture = re.compile(r"^\s*[A-Za-z_][A-Za-z0-9_]*=\$\?\s*$")

shell_files = []
for root, directories, files in os.walk("."):
    directories[:] = [d for d in directories if not d.startswith(".")]
    for name in files:
        if name.endswith(".sh"):
            shell_files.append(os.path.join(root, name).lstrip("./"))

for path in sorted(shell_files):
    lines = open(path).read().splitlines()
    if not any(re.match(r"^\s*set -[eu]*e", line) for line in lines):
        continue
    errexit = False
    for index, line in enumerate(lines):
        stripped = line.strip()
        if re.match(r"^set -[a-z]*e", stripped):
            errexit = True
        elif re.match(r"^set \+[a-z]*e", stripped):
            errexit = False
        if not capture.match(line) or not errexit:
            continue
        # Walk back over continuations to the start of the command.
        start = index - 1
        while start > 0 and lines[start - 1].rstrip().endswith("\\"):
            start -= 1
        command = " ".join(lines[start:index])
        if any(token in command for token in ("||", "&&", "if ", "while ",
                                              "until ", "then")):
            continue
        fail(f"{path}:{index + 1} captures $? after a bare command while "
             f"errexit is on;\n  the failing case ends the script before this "
             f"line, so the check never\n  runs — use `cmd || status=$?`, or "
             f"put `set +e` above the command")

# --- 3. A package where every test can skip still prints ok -----------------
#
# `ok` is the same line for a package that ran everything and one that ran
# nothing. tests/postgres_coverage_test.sh catches the wiring case for the
# database; this catches the shape wherever it appears.
for root, directories, files in os.walk("internal"):
    directories[:] = [d for d in directories if not d.startswith(".")]
    tests = [f for f in files if f.endswith("_test.go")]
    if not tests:
        continue
    total = 0
    skipping = 0
    for name in tests:
        source = open(os.path.join(root, name)).read()
        starts = [(m.start(), m.group(1))
                  for m in re.finditer(r"^func (Test\w+)\(", source, re.M)]
        for index, (offset, _) in enumerate(starts):
            end = starts[index + 1][0] if index + 1 < len(starts) else len(source)
            total += 1
            if re.search(r"\bt\.Skipf?\(", source[offset:end]):
                skipping += 1
    if total == 0 or skipping != total:
        if root in INTEGRATION_ONLY and total > 0:
            fail(f"{root} is recorded as integration-only and now has tests "
                 f"that run without it;\n  remove it from that list")
        continue
    if root in INTEGRATION_ONLY:
        continue
    fail(f"{root}: every one of its {total} tests can skip itself, so the "
         f"package prints ok\n  whether it ran or not — record the gate that "
         f"does run it, or give it a test\n  that needs nothing")

# A named gate that no longer exists would leave the exemption unchecked.
makefile = open("Makefile").read()
for package, gate in INTEGRATION_ONLY.items():
    if not os.path.isdir(package):
        fail(f"{package} is recorded as integration-only and does not exist")
    target = gate.split()[-1]
    if f"\n{target}:" not in makefile:
        fail(f"{package} names {gate}, and there is no such target")

sys.exit(status)
PY

printf 'ok: no test skips over its own fixture, no shell check is unreachable, no package passes by running nothing\n'
