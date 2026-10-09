# Hold a value to having a producer

## Why

The read model publishes 88 values across sixteen vocabularies. **Fourteen are
emitted by nothing in the running system** — twelve by nothing at all, two only
by a synthetic fixture — and nothing holds that either way.

Measured 2026-10-09:

| Vocabulary | Values | Emitted by nothing |
|---|---|---|
| `Reason` | 12 | `policy_applied`, `wake_rebaseline`, `boot_rebaseline`, `expiry_approaching`, `expired`, and `baseline` only by a fixture |
| `LinkClass` | 5 | `wireless`, `cellular`, `virtual` |
| `ResolverClass` | 4 | `system`, `encrypted`, and `scoped` only by a fixture |
| `ExpiryClass` | 4 | `expiring`, `expired` |

The other twelve vocabularies are fully produced.

The reference says so for each of them, because the last change read the mappers
to write the explanations. What it cannot do is stay true: a value could gain a
producer, or keep none after the collector that was meant to use it arrives, and
nothing would notice. The document would then describe a system that had moved.

**The question is unusually easy to get wrong, which is the argument for a
gate.** This report took four attempts, each wrong answer caught by checking a
value by hand:

1. Counting the declaring file whole said **88 of 88 produced** — every
   vocabulary's own `Valid()` switch names all of its constants.
2. Excluding the declaring file whole said `DiffReason` was 11 of 12 unproduced
   — `diff.go` both declares those values and returns them.
3. Cutting the declaration and the validity switch said `expired` was produced.
4. It is not: the match was `internal/policy`'s own `ReasonExpired`, a different
   constant with the same bare name, and **nothing references
   `connectivity.ReasonExpired`**.

A question that gave four confident wrong answers to someone reading carefully
is one an operator reading the reference will not re-derive.

## What Changes

- Every published value SHALL either have a producer or be written down as
  having none, with the reason it has none.
- A value gaining a producer while still written down as having none SHALL fail,
  and so SHALL a value losing its last producer without being written down.
- The search SHALL be package-aware and SHALL exclude a vocabulary's own
  declaration and validity switch, because those are the three ways this
  measurement goes wrong.
- **No value is added, renamed or removed**, and no runtime behaviour changes.
  A value emitted by nothing is not a defect by itself: a vocabulary fixed
  before its collectors is a deliberate choice this repository has made more
  than once.

## Capabilities

### Modified Capabilities

- `observable-connectivity-state-machine` — the requirement that what the read
  model publishes is explained where it is owned gains the other half: a value
  is also held to being producible, or to being recorded as not yet produced.

## Impact

- `tests/` — one new gate, reading the vocabularies and their producers from the
  code, with a written-down list of the values that have none.
- `docs/connectivity-read-model-reference.md` — read; it already says which
  values nothing emits, and this gate is what keeps that true.
- Nothing is installed, no runtime restarts, no live value enters Git.

## Not in this change

Whether an unproduced value should be removed from its vocabulary. Four of the
`Reason` values describe a rebaseline and an expiry the collectors do not yet
report, and deciding to delete them is a judgement about what those collectors
will do, not about the gate.

The reference's prose and the gate's list will both say which values have no
producer, and nothing holds the two in step. That is a second statement of one
fact, and it is recorded as open rather than solved by writing the prose for the
gate.
