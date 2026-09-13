# Let a decision carry its grounds

## Why

A recorded tunnel decision says what was decided and which causes held, and
nothing about what they were decided from. `rebuild_tunnel` because
`link_returned` reads as convincingly when it is right as when it is wrong, and
on 2026-09-12 it was wrong six times in half an hour.

Working out why took the connectivity archive laid beside the decisions and
matched on time: it was that archive, not the decision, that said one outer
endpoint was configured and none answered. That worked only because one runtime
writes both into one store.

The whole point of these decisions is comparison against a runtime that will not
have that store. And when this rule is granted authority, the record is what
anyone would have to explain a rebuilt tunnel from.

## What Changes

- A decision records the observations it was reached from: whether the cycle saw
  everything, whether the process was running, how long the machine slept, how
  many outer endpoints were configured and how many answered, what the rule
  believed about the link and how many failures stood behind that belief, what
  the payload probe said and how many failures stood behind it, and how many
  route operations were planned.
- The carrier is recorded as a digest and a count, never as the destinations it
  is made of.
- Every cause that can hold has a ground in the record that a reader can check it
  against.

## Impact

- Affected specs: `tunnel-supervision`
- Affected code: `internal/tunnelplan`, `internal/rootdaemon`, `internal/event`,
  `internal/connectivityhost`
- Not in scope: what the record is compared against. Twilight writes its own
  state transitions with its own vocabulary, and aligning the two is the
  cutover's problem rather than this one's.
