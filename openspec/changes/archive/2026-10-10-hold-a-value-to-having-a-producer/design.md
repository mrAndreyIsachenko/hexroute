# Design: hold a value to having a producer

## Context

See proposal.md — Why, including the four wrong answers the measurement gave
before it was right. Those four are the design: a gate that gets this wrong is
worse than none, because it would report a vocabulary fully produced when a
third of one is not.

What makes it wrong, in the order the attempts found it:

- a vocabulary's own `Valid()` switch names every value, so the declaring file
  cannot be read as written;
- the declaring file also holds real emitters — `diff.go` returns the values it
  declares — so it cannot be skipped either;
- a bare constant name matches another package's constant of the same name:
  `internal/policy` has its own `ReasonExpired`, and nothing references
  `connectivity.ReasonExpired`.

## Goals / Non-Goals

Goals: every published value held to having a producer or to being recorded as
having none; the record failing in both directions; the search right about the
three ways it goes wrong.

Non-Goals: deciding whether an unproduced value should be removed; holding the
reference's prose in step with the gate's list; reading anything outside the
sixteen connectivity vocabularies.

## Decisions

### The search is package-aware and cuts declarations

Inside the declaring package a bare name counts. Outside it, only
`<package>.<Const>` counts. The declaring file is read with its `const` blocks
and its `valid()`/`Valid()` methods removed, so a validity switch is not an
emitter and a returning function still is.

Alternative: `go` tooling — `go list`, or a type-checked pass that resolves each
identifier properly. It is the right instrument and it is rejected here for a
reason worth stating: this repository has twice been bitten by `go list`
answering incompletely on a runner, once accusing a live package of being
unreachable, and a gate that cannot answer must refuse rather than conclude. A
textual search that is explicit about its three exclusions is auditable by
reading it; a resolver that fails halfway is not.

That is a trade, not a free choice: a value referenced through a variable or
built by string concatenation would be missed. Nothing in these vocabularies is
used that way — they are compared and assigned as constants — and the gate
refuses if it finds no emitters at all for a whole vocabulary, which is what
that failure would look like.

### A fixture is not a producer

Two values are emitted only by `internal/connectivity/fixture.go`. A fixture is
a synthetic shape, not the running system, so those count as unproduced and are
written down. Counting them would mean a value stays "produced" by the very
thing that exists to stand in for production.

### The list is in the gate, beside the code it reads

Like `unwired` and `test_only` in `tests/package_reachability_test.sh`: a list
someone has to write down, with the reason per entry, and adding to it is a
decision rather than a quiet default.

Alternative: derive it from the reference, which already says which values
nothing emits. Rejected for now — the reference says it in prose, and making the
prose machine-readable writes the document for the gate. The cost is that the
two say the same thing and nothing holds them in step, which the proposal
records as open rather than hiding.

### The gate says what it held

The number of vocabularies, values, producers and recorded exceptions, so a
reader can see it is looking at something. A gate that silently checked nothing
is how two of this week's gates nearly passed on an empty read.

## Risks / Trade-offs

- **The textual search misses an indirect emitter** → the gate refuses a whole
  vocabulary with no emitters at all, which is what that would look like at
  scale; and the three exclusions are written in the gate where they can be
  read.
- **The list becomes a place to put inconvenient values** → each entry carries
  its reason, and a value that gains a producer fails until it leaves the list,
  so the list cannot quietly absorb a growing share of the vocabulary.
- **Fourteen entries is a lot to start with** → it is the measurement, not a
  concession. A smaller starting list would mean either removing values or
  calling a fixture a producer.

## Migration Plan

One gate and its list. Nothing is installed, no runtime restarts, rollback is
the commit.

## Open Questions

- Whether the same gate should cover vocabularies outside these sixteen. The
  repository publishes typed string vocabularies in other packages — the control
  machine's reasons, the planner's operation kinds — and the argument for
  holding them is the same. Answering it is a scan of its own.
