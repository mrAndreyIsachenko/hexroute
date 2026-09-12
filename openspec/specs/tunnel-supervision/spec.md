# Tunnel Supervision Specification

## Purpose

Define what a tunnel owner would decide, so that the decision can be compared
against the runtime that owns the tunnel before this one is allowed to act. The
decision is reached on every cycle and performed on none of them: a rule that
has never been applied to a live machine is not evidence that it is right, and a
rule first applied at the moment another runtime is booted out is applied where
being wrong costs the network.

## Requirements

### Requirement: A tunnel owner's decision is reached before it is held

This runtime SHALL decide what a tunnel owner would do on every observation
cycle, and SHALL perform none of it while another runtime owns the tunnel.

The decision SHALL be reached from the observations the cycle already takes,
and SHALL name which of its causes produced it rather than reporting only that
it would act.

#### Scenario: A cause is present

- **WHEN** an observation cycle finds a cause to rebuild the tunnel
- **THEN** the decision names that cause
- **AND** nothing is performed

#### Scenario: No cause is present

- **WHEN** a cycle finds no cause
- **THEN** the decision is to do nothing, and that is recorded as a decision rather than as silence

### Requirement: The causes are the ones that keep a machine on the network

The runtime SHALL reach a decision to rebuild the tunnel when the sing-box
process is gone, when the interval since the previous cycle exceeds the wake
threshold, when the set of interfaces carrying the configured destinations has
changed, when connectivity has returned after being absent past its threshold,
or when the payload path has failed past its threshold. It SHALL reach a
decision to reapply routes when the observed routes differ from the planned
ones.

Absence SHALL be counted rather than observed. Connectivity counts as absent
only once the outer path has been unreachable for a configured number of
consecutive complete cycles, and as present again on the first cycle that
reaches it. A cycle that did not reach the probes SHALL neither add to the count
nor reset it, and a runtime that has not yet decided anything SHALL treat the
link as present rather than absent, so that its first successful probe is not a
return.

The threshold is not decoration. Measured on this machine over seven days, the
outer probe failed on 25 of 3,117 cycles and never twice in a row; without a
threshold every one of those was an absence and every following cycle was a
return. Six of them fell in one half hour on 2026-09-12, each asking for a
rebuild of a tunnel that was working, while the runtime that owns that tunnel
did nothing at all in the same window.

These are the six the production supervisor acts on, measured from its own log
rather than derived from its source. Two of them have not occurred in
sixty-one days and are kept: the process exiting is the only thing between this
machine and no network at all, and connectivity returning is how a laptop comes
back from a dead link.

#### Scenario: The process is gone

- **WHEN** the sing-box process is not running
- **THEN** the decision is to rebuild, naming the process

#### Scenario: A wake gap

- **WHEN** the interval since the previous cycle exceeds the configured wake threshold
- **THEN** the decision is to rebuild, naming the gap

#### Scenario: The carrier changed

- **WHEN** the interfaces carrying the configured destinations differ from the previous cycle's
- **THEN** the decision is to rebuild, naming the carrier

#### Scenario: Connectivity returned

- **WHEN** the outer path has been unreachable for the configured number of consecutive cycles and is then reached
- **THEN** the decision is to rebuild, naming the return

#### Scenario: A single probe fails and the next succeeds

- **WHEN** the outer path is unreachable on one cycle and reachable on the next, below the threshold
- **THEN** no cause is named and no rebuild is decided

#### Scenario: A cycle stops before the probes while the link is absent

- **WHEN** a cycle does not complete
- **THEN** it neither adds to the count of consecutive failures nor resets it

#### Scenario: The payload path failed

- **WHEN** the payload probe has failed for the configured number of consecutive cycles
- **THEN** the decision is to rebuild, naming the payload path

#### Scenario: More than one cause is present

- **WHEN** several causes hold in the same cycle
- **THEN** one decision is reached and it names every cause that held, because a reader comparing it against another runtime needs to know which of them that runtime saw

### Requirement: A probe that proves traffic rather than reach

The runtime SHALL probe the payload path in a way that fails when traffic does
not traverse the tunnel, and SHALL NOT treat a completed connection as proof of
traversal.

A socket that answers proves that something answered. This repository has
already written down that a reachable socket is not qualification, and the
switch this prepares for cannot be completed on evidence weaker than the thing
it claims.

#### Scenario: The tunnel carries traffic

- **WHEN** the payload path is exercised and the response is the expected one
- **THEN** the probe succeeds

#### Scenario: The tunnel is up and carries nothing

- **WHEN** the connection completes but the payload does not traverse
- **THEN** the probe fails, and it is this failure that the payload cause counts

### Requirement: The decision is recorded beside what the owner did

The runtime SHALL record each decision durably, and the record SHALL be
comparable against what the owning runtime actually did.

A decision that agreed and a decision that was never reached look the same in
an empty log. The comparison is the whole purpose of deciding without acting,
so the record SHALL exist on every cycle, including the cycles that decided to
do nothing.

#### Scenario: A cycle decides

- **WHEN** any cycle completes
- **THEN** its decision is recorded, whether or not it found a cause

#### Scenario: The owner acted

- **WHEN** the owning runtime rebuilt the tunnel
- **THEN** the record can be read to say whether this runtime would have, and for which cause
