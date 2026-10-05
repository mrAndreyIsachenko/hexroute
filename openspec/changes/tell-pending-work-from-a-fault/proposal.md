# Tell pending work from a fault

## Why

The root daemon reported `DEGRADED` continuously for seven hours while nothing
was wrong. Measured 2026-10-05: degraded from 04:12:45Z to 11:36Z unbroken,
`consecutive_failures: 0`, `attempts: 0`, `link_present: true`,
`link_failures: 0`, `payload_failures: 0`, the kernel agreeing with every route
the runtime had observed.

The cycle is degraded because its route plan is not empty, and in `observe-only`
no plan is ever applied, so no plan is ever empty. A state that is always on
cannot report a fault: this runtime would read `DEGRADED` through a real outage
exactly as it reads now, and an operator who learned that over seven hours has
learned to ignore it.

Three claims the report cannot back:

- **Pending work is called degradation.** `CycleHealthy` requires
  `Failures == 0 && len(plan.Operations) == 0`. The second clause asks the
  runtime to have no work, not to be well.
- **The reason is a label, not an event.** `rootOperatorReason` maps every
  degraded cycle to `probe_failed`. No probe failed. An operator reading that
  field goes looking for a failing probe, and this one did.
- **The counters belong to a machine that is not deciding.**
  `nextRootOperatorSnapshot` writes `summary.State` and `summary.Failures`
  straight into the snapshot, so `consecutive_failures` and `attempts` are
  reported in the shape of the threshold machine in `internal/control` while
  that machine's transitions never ran. `DEGRADED` with zero counters is a
  combination its transitions cannot produce, which is what made the state look
  like a defect in the reporter rather than a true reading.

The user daemon does **not** have this defect, and the difference says what
the root one is missing. Its health comes from the transitions of the machine
in `internal/control`, and its extra clause requiring no standing action is
redundant with that: a plan carries an action only after recovery was approved,
which leaves the machine recovering rather than healthy. Every observed case
pairs a healthy state with no action and none pairs it with one. The root
daemon's state is synthesized from the cycle instead, which is how a condition
about workload got into it.

## What Changes

- A cycle's health SHALL distinguish work the runtime has **no authority to
  apply** from work it **attempted and could not do**. A plan the runtime is
  not permitted to execute is a pending proposal, not degradation.
- A degraded cycle SHALL carry a reason naming what failed. A state-to-reason
  mapping that cannot be wrong because it says the same thing every time is
  removed.
- Quantities reported beside a state SHALL come from the path that produced
  that state, or SHALL be absent. A counter that is structurally always zero
  SHALL NOT be published in the shape of a counter that accumulates.
- The closed vocabulary of cycle results SHALL NOT grow. Health stays an
  answer about soundness, and standing work becomes a quantity of its own
  beside it: a count of operations the runtime proposes and has no authority to
  apply, which no reader has to interpret and which does not decide health.
  Every existing reader — the journal, `ctl status`, `connectivity-watch` —
  keeps the three words it already knows.
- **No route is corrected by this change.** The two ingress routes standing on
  the opposite links from their configured `preferred_link` stay where they
  are; this change stops calling that a fault and leaves correcting it to the
  authority that change 3 established.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `local-control-plane-foundation` — the requirements covering what a runtime
  reports about itself and what observe-only output means. A cycle's reported
  health and the reason beside it gain requirements; the observe-only proposal
  requirement gains the scenario that a standing proposal is not a fault.

## Impact

- `internal/rootdaemon/cycle.go` — the `CycleHealthy` condition and the
  vocabulary of cycle states.
- `internal/rootdaemon/run.go` — `nextRootOperatorSnapshot`,
  `rootOperatorReason`, `emitSummary`.
- `internal/userdaemon/run.go` — unchanged. Its `ActionNone` clause is redundant
  rather than wrong, and removing a clause no test fails without is not this
  change's work.
- `internal/control` — whether the published snapshot keeps fields the
  transitions did not produce.
- `docs/macos/root-observe.md`, `docs/macos/user-observe.md` — the operator's
  reading of a degraded cycle, which currently sends them after a probe.
- **HEX-11** is reinterpreted, not closed: the planner proposing the same two
  operations every cycle is correct behaviour for a runtime with no authority
  to apply them. What was read as planner noise was the health state
  mislabelling it. HEX-11's remaining question is whether a standing proposal
  should be re-derived every cycle at all.
- Nothing in the data plane moves: AdGuard, Twilight and both Codex paths are
  untouched, and the runtime stays `observe-only` throughout.
