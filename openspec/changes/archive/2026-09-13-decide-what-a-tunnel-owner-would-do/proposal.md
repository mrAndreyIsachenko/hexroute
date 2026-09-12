## Why

Roadmap item 8 hands the tunnel from Twilight to this runtime. The grill settled
that the build and the switch are separate changes, in that order, because the
alternative makes booting the supervisor out the first occasion on which the
decision rule was ever applied to a live machine — and if it is wrong about a
wake or a carrier change, that is learned without a network.

Measured from the supervisor's own log over 61 days, six things cause it to act,
and three of them are the same act for different reasons:

| cause | in 61 days |
|---|---|
| the carrier's routes changed | 19 |
| a wake gap | 11 |
| the payload path failed | 2 |
| the sing-box process exited | 0 |
| the internet came back | 0 |
| the routes drifted | every tick |

This runtime already observes all six. It has no rule that turns them into a
decision, and no probe that can tell traffic traversing the tunnel from a socket
that answers.

## What Changes

A planner decides what a tunnel owner would do, and the runtime records that
decision beside what Twilight actually did. It performs nothing: Twilight keeps
the tunnel throughout.

A payload probe joins the endpoint probes, because the sixth cause is the only
one that distinguishes a tunnel that is up from one that carries traffic — and
because roadmap item 8 requires exactly that distinction as the evidence that
completes the switch.

## Capabilities

### New Capabilities

- `tunnel-supervision`: this runtime decides what a tunnel owner would do, and
  is judged against what the owner did.

## Impact

- `internal/tunnelplan` (new) — the decision, pure and testable.
- `internal/observe` — a payload probe that proves traversal rather than reach.
- `internal/rootdaemon` — the cycle carries the decision and the state it needs
  across ticks.
- Nothing executes. The safety allowlist already names `restart sing_box` and
  `apply scoped_routes`, and no policy grants either: the capability that would
  is the change after this one.
