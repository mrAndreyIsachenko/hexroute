# Run the sentinel's recovery planner, without letting it act

## Why

`local-control-plane-foundation` requires the sentinel to demand two
independent failures before **one bounded** recovery attempt, and to enter a
long cooldown after verifying. The planner that does this exists —
`internal/sentinel/recovery.go`, written in a single commit on 24 July and
untouched since — and nothing constructs it.

What runs today observes, and when both evidence sources fail it emits one
warning. Every cycle. Forever. There is no phase, no attempt bound, no
cooldown, because the state machine that holds all three has never executed.
The half of the requirement that makes the sentinel safe is the half that has
never run.

The observe-only path is not held behind a cutover. A controller built with
`RecoveryObserveOnly` produces a plan and returns without acting, and its
restarter may be nil — the constructor allows that explicitly, and refuses a
nil restarter only in the active mode. So the observing half can run now, and
not running it costs the evidence a later cutover would be decided on.

## What Changes

- Construct the recovery planner and controller in observe-only mode inside
  the sentinel's cycle, with no restarter at all, so acting is impossible
  rather than declined.
- Replace the repeated "evidence is ready" warning with the plan: which phase
  the sentinel is in, what it would have done, and that it did nothing.
- Record the bound being reached — the point where an active sentinel would
  have stopped after one attempt — because that is the number a cutover
  decision needs and no log carries it today.
- Leave the active path exactly as it is. This change adds no authority and
  removes none.

## Capabilities

**New Capabilities:** none.

**Modified Capabilities:**

- `local-control-plane-foundation` — adds what the sentinel records while it
  has no authority, beside the requirement that says what it may do when it
  has.

## Impact

- `internal/sentinel` — `Run` builds a planner and controller; the cycle's
  decision reaches them; the summary carries the plan.
- `internal/logging` — the sentinel's evidence event carries a phase and an
  action rather than only the fact that evidence exists.
- `tests/unbuilt_capability_test.sh` — `NewRecoveryPlanner` and
  `NewRecoveryController` leave the list of constructors nothing calls.
  `NewMacOSRootRestarter` stays on it, and its reason changes from "recovery
  has never run" to "the active mode is not authorized".

No live runtime is changed by this proposal. The sentinel is its own binary
and is not the daemon under qualification.
