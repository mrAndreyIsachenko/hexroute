#!/usr/bin/env python3
"""Every published value has something that emits it, or is written down here.

A vocabulary fixed before the collectors that will use it is a deliberate
choice this repository has made more than once. Which of its values are not yet
reachable is a recorded fact, not something a reader works out from the code.

This measurement is unusually easy to get wrong. It gave four confident wrong
answers before it was written down, each caught by checking one value by hand:

  1. Counting the declaring file whole said 88 of 88 values were produced —
     every vocabulary's own Valid() switch names all of its constants.
  2. Excluding the declaring file whole said DiffReason was 11 of 12 unproduced,
     because diff.go declares those values and returns them.
  3. Cutting the declaration and the validity switch said `expired` was
     produced.
  4. It is not. The match was internal/policy's own ReasonExpired, a different
     constant with the same bare name, and nothing references
     connectivity.ReasonExpired.

So: a bare name counts only inside the declaring package, the qualified name
counts outside it, and the declaring file is read with its const blocks and
validity switches removed. A file named as a fixture is not a producer — a value
kept alive by the thing that stands in for production is not produced.

Usage: value_producers.py --hold DIR[,DIR...] --search DIR[,DIR...]
                          [--recorded FILE]

--recorded replaces the written-down list with one read from a file, one
`Vocabulary.value reason` per line. It exists so this gate's own fixtures can
supply their own list; without it the list below is used.
"""
import os
import re
import sys

# Values that nothing emits. Each entry is a decision someone wrote down, and
# the gate refuses a value that gains a producer while still listed, so this
# cannot quietly absorb a growing share of a vocabulary.
UNPRODUCED = {
    # The rebaseline reasons: a wake or a boot does set rebaseline_required on
    # a component row, but no collector carries either as a fact's reason.
    "Reason.wake_rebaseline": "no collector reports a wake as a reason; the row carries rebaseline_required instead",
    "Reason.boot_rebaseline": "no collector reports a boot as a reason; the row carries rebaseline_required instead",
    # Expiry: the session collector reports an expiry_class and never a reason
    # about expiry.
    "Reason.expiry_approaching": "session_expiry reports expiry_class, not a reason",
    "Reason.expired": "session_expiry reports expiry_class, not a reason",
    # Policy application is not something a collector observes.
    "Reason.policy_applied": "no collector observes a policy being applied",
    # Only internal/connectivity/fixture.go emits it, which is synthetic.
    "Reason.baseline": "emitted only by the synthetic fixture; the acceptor marks baselines on the stream instead",
    # The observer cannot tell one kind of link from another, so the mapper
    # reports `wired` for any link that is up.
    "LinkClass.wireless": "the observer does not distinguish link kinds; up reads as wired",
    "LinkClass.cellular": "the observer does not distinguish link kinds; up reads as wired",
    "LinkClass.virtual": "the observer does not distinguish link kinds; up reads as wired",
    # There is no DNS collector at all, and a test asserts none can emit one.
    "ResolverClass.system": "no DNS collector exists; a test asserts none can emit one",
    "ResolverClass.encrypted": "no DNS collector exists; a test asserts none can emit one",
    "ResolverClass.scoped": "emitted only by the synthetic fixture; no DNS collector exists",
    # The session collector reports valid or none and never the two between.
    "ExpiryClass.expiring": "session_expiry reports valid while a session is active and none otherwise",
    "ExpiryClass.expired": "session_expiry reports valid while a session is active and none otherwise",
}

TYPE = re.compile(r"^type (\w+) string$", re.M)
CONST_BLOCK = re.compile(r"^const \(\n(?:.*?\n)*?^\)$", re.M)
VALID_FUNC = re.compile(r"^func \([^)]*\) [Vv]alid\w*\(\)[^{]*\{\n(?:.*?\n)*?^\}$", re.M)
PACKAGE = re.compile(r"^package (\w+)$", re.M)


def go_files(roots):
    for root in roots:
        for base, _dirs, files in os.walk(root):
            for name in sorted(files):
                if name.endswith(".go"):
                    yield os.path.join(base, name)


def is_fixture(path):
    return "fixture" in os.path.basename(path)


def declarations(paths):
    """Every typed string vocabulary and its constants, read from the code."""
    found = {}
    for path in paths:
        source = open(path).read()
        for name in TYPE.findall(source):
            constants = re.findall(rf"^\t(\w+)\s+{name} = \"([a-z_.]+)\"$", source, re.M)
            if constants:
                found[name] = (path, PACKAGE.search(source).group(1), constants)
    return found


def strip_declarations(source):
    """A declaring file without its const blocks or validity switches."""
    return VALID_FUNC.sub("", CONST_BLOCK.sub("", source))


def main(hold, search, recorded):
    held_files = [p for p in go_files(hold) if not p.endswith("_test.go")]
    vocabularies = declarations(held_files)
    if not vocabularies:
        print(f"no typed string vocabulary was found under {', '.join(hold)};",
              "this gate is not looking at anything", file=sys.stderr)
        return 1

    sources = {
        path: open(path).read()
        for path in go_files(search)
        if not path.endswith("_test.go")
    }

    status = 0
    produced = written_down = 0
    for name, (home, package, constants) in sorted(vocabularies.items()):
        home_dir = os.path.dirname(home)
        emitted = set()
        for constant, value in constants:
            bare = re.compile(rf"\b{constant}\b")
            qualified = re.compile(rf"\b{package}\.{constant}\b")
            for path, source in sources.items():
                if is_fixture(path):
                    continue
                body = strip_declarations(source) if path == home else source
                pattern = bare if os.path.dirname(path) == home_dir else qualified
                if pattern.search(body):
                    emitted.add(value)
                    break
        if not emitted:
            # An indirect emitter is what a textual search cannot see, and this
            # is what that would look like for a whole vocabulary. Refusing is
            # the condition on which the search was chosen over a resolver.
            print(f"{name}: nothing emits any of its {len(constants)} values;",
                  "this is a broken read rather than an unused vocabulary", file=sys.stderr)
            status = 1
            continue
        for _constant, value in constants:
            key = f"{name}.{value}"
            if value in emitted:
                produced += 1
                if key in recorded:
                    print(f"{key} is emitted and is still written down as having no producer",
                          file=sys.stderr)
                    status = 1
            elif key in recorded:
                written_down += 1
            else:
                print(f"{key} is emitted by nothing and is not written down",
                      file=sys.stderr)
                status = 1

    stale = sorted(
        key for key in recorded
        if key.split(".", 1)[0] not in vocabularies
        or key.split(".", 1)[1] not in [v for _c, v in vocabularies[key.split(".", 1)[0]][2]]
    )
    for key in stale:
        print(f"{key} is written down and no vocabulary declares it", file=sys.stderr)
        status = 1

    if status != 0:
        return 1
    print(f"ok: every published value has a producer or is written down "
          f"({len(vocabularies)} vocabularies, {produced + written_down} values, "
          f"{produced} produced, {written_down} written down)")
    return 0


if __name__ == "__main__":
    arguments = sys.argv[1:]
    recorded = dict(UNPRODUCED)
    if len(arguments) == 6 and arguments[4] == "--recorded":
        recorded = {}
        for line in open(arguments[5]):
            line = line.strip()
            if not line or line.startswith("#"):
                continue
            key, _, reason = line.partition(" ")
            recorded[key] = reason
        arguments = arguments[:4]
    if len(arguments) == 4 and arguments[0] == "--hold" and arguments[2] == "--search":
        raise SystemExit(main(arguments[1].split(","), arguments[3].split(","), recorded))
    print(__doc__, file=sys.stderr)
    raise SystemExit(2)
