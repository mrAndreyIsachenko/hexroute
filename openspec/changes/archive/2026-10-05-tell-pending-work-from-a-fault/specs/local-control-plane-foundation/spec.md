## ADDED Requirements

### Requirement: A cycle's health answers soundness, not workload

A runtime's reported health SHALL answer whether the runtime is sound. It SHALL
NOT answer whether the runtime has work outstanding. Work a runtime has
proposed and has no authority to apply SHALL NOT make it unhealthy, however
long the proposal stands.

A runtime with no mutation authority never applies a proposal, so a proposal it
is right to make stands for as long as the condition holds. Health that counts
a standing proposal reports the same value forever and therefore reports
nothing: measured 2026-10-05, a root runtime read degraded for seven unbroken
hours with no failure of any kind, and would have read the same through an
outage.

Standing work SHALL be reported as a quantity of its own beside the health: a
count of operations the runtime proposes and may not apply. The closed
vocabulary of health results SHALL NOT grow to carry it, so that every reader
of a health result keeps the words it already knows.

A health result of anything other than sound SHALL carry a reason naming what
failed. A reason derived from the health result itself SHALL NOT be published,
because a field that says the same thing every time its state occurs adds no
information and sends a reader after a cause that was never claimed.

Quantities published beside a health result SHALL come from the path that
produced that result, or SHALL be absent. A quantity whose value is
structurally fixed SHALL NOT be published in the shape of one that accumulates.

#### Scenario: A proposal the runtime may not apply

- **WHEN** a cycle completes with no failure and proposes operations it has no authority to apply
- **THEN** it reports the runtime as sound
- **AND** it reports the number of operations standing unapplied

#### Scenario: A cycle that failed

- **WHEN** a cycle fails an observation
- **THEN** it reports the runtime as unsound
- **AND** the reason names the observation that failed

#### Scenario: A reason is not a restatement

- **WHEN** a runtime publishes a reason beside an unsound health result
- **THEN** the reason is the recorded cause of a failure, not a value mapped from the health result

#### Scenario: A counter that cannot count

- **WHEN** a health result is published by a path that does not accumulate a failure counter
- **THEN** that counter is absent from the published record rather than present and zero

#### Scenario: A standing proposal and a fault at once

- **WHEN** a cycle both fails an observation and proposes operations it may not apply
- **THEN** it reports the runtime as unsound for the failure
- **AND** the count of unapplied operations is reported unchanged beside it

## MODIFIED Requirements

### Requirement: Non-executable observe-only reconciliation output

The local control plane MAY publish normalized snapshots, desired-state diffs
and generation-bound reconciliation proposals, but it SHALL expose no IPC,
command or callback that executes a proposal. Existing component observations,
Twilight production processes, AdGuard and normal and fallback Codex paths SHALL
remain unchanged throughout rollout and rollback.

A proposal this plane publishes and cannot execute SHALL NOT be reported as a
divergence from health. The plane is built to see work it may not do, so seeing
it is the plane working.

#### Scenario: Root observes a route divergence

- **WHEN** the normalized diff reports a missing, unexpected or divergent route
- **THEN** the runtime records an observe-only proposal
- **AND** it does not add, delete or replace any route

#### Scenario: A divergence it may not correct

- **WHEN** the runtime holds an observe-only proposal for a route it has no authority to change
- **THEN** its reported health is unaffected by the proposal standing

#### Scenario: State-machine integration is disabled

- **WHEN** the new reducer and aggregate status path are rolled back
- **THEN** existing component observe-only paths continue operating
- **AND** no network inverse action or Twilight restart is required
