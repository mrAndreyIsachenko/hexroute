# Design

## Context

Two halves of one requirement were built together and only one was connected.
The sentinel observes and reports that its two evidence sources have both
failed; the planner that turns that into a bounded, phased decision is
constructed by nothing.

The consequence is not that recovery is missing — recovery is deliberately
unauthorized before cutover. It is that the *bound* is missing. An operator
reading today's logs sees the same warning every cycle and cannot tell a host
that has been failing for a minute from one that has been failing for a day,
or know at what point an authorized sentinel would have stopped trying.

## Goals / Non-Goals

**Goals:**

- Make the phase machine and the attempt bound execute, so the evidence a
  cutover would be decided on exists before the decision.
- Record what an authorized sentinel would have done, at the moment it would
  have done it.

**Non-Goals:**

- No authority is added. The active path is untouched.
- No new evidence source. The gate is the one the requirement already states.
- Nothing about the root daemon changes. The sentinel is a separate binary and
  is not the daemon under qualification.

## Decisions

### The observing sentinel holds no restarter at all

`NewRecoveryController` accepts a nil restarter in observe-only mode and
refuses one in active mode. The observing sentinel is built with nil.

Passing a real restarter and relying on the mode check would make "it does not
act" a property of one branch. Passing nil makes it a property of the object:
there is nothing to call. The difference matters on the day someone changes
that branch for a reason that looks good.

### The plan replaces the warning rather than joining it

Today `EventSentinelEvidence` fires whenever both sources have failed, which
is every cycle for as long as the condition holds. That is a line that stops
being read after the third one.

The plan carries a phase, so the same underlying condition produces
`monitoring`, then `verifying`, then `cooldown` — and the transitions are what
an operator wants. Repeating the phase every cycle would restore exactly the
problem, so a plan is recorded when the phase or the action changes.

### The bound is its own record

An authorized sentinel spends its one attempt and stops. The observing one has
no attempt to spend, so the moment is invisible unless it is written down
separately. That moment is the number a cutover decision needs: how often,
over a soak, would the sentinel have restarted the daemon, and would it have
stopped when it should.

## Risks / Trade-offs

- **The planner runs for the first time in production.** It has tests, and it
  has never executed against a real host. Observe-only is the mitigation: the
  worst outcome of a wrong plan is a wrong log line.
- **Log volume.** Recording on transition rather than per cycle bounds it,
  and the existing per-cycle summary is unchanged.

## Open Questions

- Whether one data-path target is enough, and what the observation said about
  it. The configuration takes exactly one endpoint and the cycle makes one
  probe. A second would need a combining rule, and the right one is "both
  failed": more evidence and fewer false positives, and nothing is lost
  because a genuinely broken tunnel fails both.

  It is not built now because a flaky single target costs nothing on its own —
  the gate needs a stale heartbeat as well, so a target that blinks while the
  daemon is healthy produces no plan at all. What it could cost is the
  coincidence: a genuinely stale heartbeat while the one target happens to be
  down for its own reasons. Observing, that is a wrong log line; after cutover
  it would be a wrong restart.

  So the second target is a question for the cutover, and the observation is
  what should answer it. The number to look for is how often
  `sentinel_recovery_plan` selected `restart_hexrouted` while the root daemon
  was in fact healthy. Building a second probe before that number exists would
  be guessing at the thing this observation was set up to measure.

- Whether the recorded plans should also enter the local event archive rather
  than only the log. They are typed events in everything but name, and the
  archive is what answers about last week. Deferred: the archive has no
  producer outside the connectivity journal yet, and adding a second one is a
  decision worth taking on its own.
