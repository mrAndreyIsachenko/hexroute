# Design: tell pending work from a fault

## Context

See proposal.md — Why. Three facts measured from the code and the machine
decide the shape of this change.

- `Summary` (`internal/rootdaemon/cycle.go`) carries `Failures uint32` and no
  cause. Fifteen call sites increment it; none records which observation
  failed. The reason published beside the health today is therefore not a
  weakened cause, it is the absence of one.
- `control.Snapshot` validates `SchemaVersion != SnapshotSchemaVersion` as
  invalid, and the user daemon loads its persisted snapshot at start where any
  error other than "not found" is fatal. A version bump would stop the new
  binary on the old file and the old binary on the new one: neither the install
  nor its rollback would start.
- The health result reaches the operator through `ctl status` and
  `ctl diagnostics`, which already answer with fields from outside the snapshot
  (`last_tick`, `last_reason`, `consecutive_failures`, `attempts`).

## Goals / Non-Goals

Goals: health answers soundness; a standing proposal is counted, not graded; a
reason names a recorded failure; a published quantity comes from the path that
maintains it.

Non-Goals: correcting any route; granting the runtime authority it does not
have; changing the plan the planner produces; changing what is observed or how
often. Nothing in this change moves a packet.

## Decisions

### The pending count is current, not persisted

`pending_operations` is published in the `ctl status` and `ctl diagnostics`
payloads, computed from the cycle that produced the health being reported. It
is **not** added to `control.Snapshot`.

Why not the snapshot: the snapshot is persisted and version-validated, and the
user daemon treats an unreadable snapshot as fatal, so a field there costs a
migration in both directions for a quantity that means nothing after a restart.
A count of operations standing right now has no value to carry across one.

Alternatives: adding the field to `Snapshot` with `SnapshotSchemaVersion = 2`
and a reader that accepts both versions — rejected as a migration bought for no
benefit; putting the count in the journal record — rejected with the proposal,
and the proposals are already journalled as their own events; putting it in
`connectivity-watch.json` — rejected because the operator asking "what now"
asks `ctl`.

### Health is a function of failures alone

A cycle is sound when `Failures == 0`. The `len(plan.Operations) == 0` clause
is removed, and the user daemon's `ActionNone` clause with it.

This stays correct after cutover. A runtime that may apply its plan and does
not succeed records a failure, and the failure is what makes it unsound — not
the leftover work. Making health depend on authority instead would keep the
confusion and add a mode to reason about.

### The cycle keeps the failure it reports

`Summary` gains the recorded cause of its first failure in configuration order.
Configuration order is already the rule for which failure a cycle reports when
several fail — "An observation cycle waits once for what it can wait for
together" fixes it — so this reuses a settled rule rather than inventing one.

`rootOperatorReason` stops deriving a reason from the state. A sound cycle
reports `ReasonProbeSucceeded`, a suspended one `ReasonIntentionalSleep`, and
an unsound one reports the cause the cycle kept. `ReasonProbeFailed` remains in
the vocabulary and becomes what it says: a probe that failed.

### A quantity absent rather than zero

The root path never increments `Attempts`: no recovery is attempted from it. It
is published today as `attempts: 0`, which reads as a budget with nothing spent
rather than a budget this path does not keep.

The reported payload is therefore assembled from the fields the producing path
maintains, and the root report omits `attempts`. The shared `Snapshot` keeps the
field, because the sentinel and recovery paths do maintain it.

Alternative: `omitempty` on the struct — rejected, because zero is a legitimate
reading for a path that does count, and the two cases must stay distinguishable.

## Risks / Trade-offs

- **An operator who watched `DEGRADED` for the route divergence now sees
  `sound`.** → The divergence keeps its own publication: the proposal events and
  the new count. Both daemons' observe documents must say, in the section that
  currently sends the reader after a probe, that a standing proposal is read
  from `pending_operations`.
- **Two shapes to keep in sync** once the report is assembled per role rather
  than serialized from the snapshot. → One test asserting every field in a
  role's reported payload is written by that role's path, and that the fields it
  does not maintain are absent.
- **A reader parsing `attempts` from the root report breaks.** → The only
  readers are `ctl` and the two observe documents, both in this repository and
  both updated here. The IPC schema test covers the payload shape.
- **The first cycle after install reports sound while the route divergence is
  unchanged**, which looks like the change hid a fault. → The install record
  should capture `pending_operations` before and after, so the quantity that
  replaced the grade is visible in the same reading.

## Migration Plan

No state migration: no persisted schema changes, and `SnapshotSchemaVersion`
stays 1. Install is the ordinary binary install; rollback is the previous
binary against the same unchanged state, which is why the count was kept out of
the snapshot.

The health a reader sees changes at the first cycle after the new binary starts.
That is the whole point of the change and needs no step of its own.

## Open Questions

- Whether `pending_operations` counts plan operations or distinct targets. The
  plan's own count is taken for now; a target that needs two operations is not
  yet known to occur, and the scenarios hold either way.
