# Local Control Plane Foundation Specification Delta

## ADDED Requirements

### Requirement: A runtime says why it stopped

A runtime that ends on a failure SHALL record that it stopped and what ended it,
naming the part of its own work that failed from a closed vocabulary. An ending
nobody asked for and one that was requested SHALL be distinguishable by the
result of that record rather than by its presence.

Measured 2026-09-26, the root daemon ended and left no account: a
`daemon_started` with no `daemon_stopped` before it, nothing above `info` in
either log, and a count of restarts under launchd as the only trace. Eleven exits
of its observation loop became one exit code, and the one ending that recorded
anything was the cancelled context, which needs no explanation.

The record SHALL be written where the failure it reports cannot have broken it:
a runtime whose journal could not be written SHALL NOT report that through the
journal.

Failing to write the record SHALL NOT change the ending. A runtime that can write
neither log still stops, with the status it would have had.

Both local runtimes SHALL answer this the same way. A defect ending one of them
is as invisible as a defect ending the other.

#### Scenario: A runtime stops on a failure

- **WHEN** a runtime's loop ends on a failure of its own work
- **THEN** it records that it stopped, with a result saying nobody asked for it, naming the part that failed
- **AND** the exit status is what that failure calls for

#### Scenario: A runtime is asked to stop

- **WHEN** a runtime's context is cancelled
- **THEN** it records that it stopped with a result saying so, and names no failure

#### Scenario: The journal is what failed

- **WHEN** a runtime stops because a log record could not be written
- **THEN** the stop is reported through the other log rather than the one that failed

#### Scenario: Nothing can be written at all

- **WHEN** neither log can be written as the runtime stops
- **THEN** it stops anyway, with the status the failure calls for
