## Why

macOS ships bash 3.2, and in it `set -e` does not fire on a failing `[[ ]]`.
A conditional written as a statement evaluates, reports false, and the script
continues to its success message.

Thirty-three assertions in eleven gates under `tests/` are written that way,
and `make check` — the only job that runs `shell-test` — runs on macOS. The
Linux job runs the Go tests and the secret gate and no shell gate at all. So
these assertions have never been enforced on any runner.

This was found by a deliberately broken build passing a new gate twice. The
first time it looked like a mistake in the mutation. Checking the instrument
rather than the result is what turned it up.

An assertion that cannot fail is worse than no assertion: it is a claim in the
repository that something is checked, and every reader after it believes the
claim.

## What Changes

Every assertion in the gates says what to do when it fails, so that it fails on
any shell and names itself when it does. A gate refuses the shape that cannot
fail, so it cannot come back.

## Capabilities

### Modified Capabilities

- `repository-and-deployment-boundary`: a gate's assertion must be able to fail,
  and must say which assertion failed.

## Impact

- `tests/*.sh` — thirty-three assertions in eleven gates.
- `tests/assertion_shape_test.sh` (new) — refuses the shape.
- `docs/roadmap.md` — the figure recorded on 2026-09-10 was sixty-seven in
  seventeen, which counted conditionals inside `if`, `while` and intentional
  `&&` lists. The number that was ever an inert assertion is thirty-three.
