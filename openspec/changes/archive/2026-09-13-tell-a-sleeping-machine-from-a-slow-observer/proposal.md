# Tell a sleeping machine from a slow observer

## Why

The wake gap is measured as the interval between this runtime's own
observations, so a runtime that is slow reports a machine that slept.

Measured on 2026-09-12, from the daemon's own log across one cycle: the
observation finished at 22:02:43.391, the fold finished at 22:03:15.796, and the
decision was recorded thirteen milliseconds later. The first fold after a
reinstall cost 32.4 seconds. The loop then slept its interval and observed again
at 22:04:16, so ninety-three seconds passed between two observations against a
threshold of ninety, and the cycle named a wake gap on a machine that had been
awake throughout.

The cause is true and its subject is wrong. It is the third defect this rule's
soak found, and the only one that would fire on a machine that is behaving: a
restart manufactures a rebuild on the cycle after it. Granting this rule
authority with that standing means installing the daemon tears down the tunnel
it was installed to watch.

The interval it measures is also longer than it need be. The loop starts its
timer after the work rather than aiming at a period, so every slow cycle pushes
the next observation out by however long the slow one took, and the two defects
compound: a 32-second fold becomes a 93-second gap.

## What Changes

- The gap the rule acts on is the time the machine spent asleep, measured
  directly rather than inferred from an absence of observations. On this
  platform the monotonic clock stops across sleep and the wall clock does not,
  so the difference between them is the sleep and nothing else.
- The observation loop aims at a period. A cycle that overran does not push the
  next one out; it starts the next immediately.
- A cycle carries two clocks rather than one, so that a test can make a machine
  sleep without sleeping.

## Impact

- Affected specs: `tunnel-supervision`, `observable-connectivity-state-machine`
- Affected code: `internal/tunnelplan`, `internal/rootdaemon`
- Not in scope: the three minutes the daemon runs before it logs
  `daemon_started`. It is measured and recorded and it is a different question —
  what opening two spools and an archive costs — and nothing observes during it
  whichever way the wake gap is measured.
