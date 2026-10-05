# Local Control Plane Foundation Specification Delta

## ADDED Requirements

### Requirement: An authorization compares the runtime's own control state

The generations an action authorization is judged against SHALL be the ones the
runtime holds for itself. The control-state generation SHALL be read by the
runtime that answers the request, from the store that holds it, and SHALL NOT be
taken from the request being judged.

It was taken from the request. The evaluator compared the caller's generation
with a copy of the caller's generation, so the only requirement left was that the
value was not zero, on every action path there is — Pritunl rescue, operator
resume and the tunnel question. A request naming a control state from an hour ago
was authorized as readily as one naming the current state, which is the whole
protection that generation exists to give.

A runtime that cannot read its own control-state generation SHALL refuse the
action. An authorization that proceeds when the state it must compare against is
unreadable is the failure it was built to prevent, arriving as an error path
instead.

#### Scenario: A stale control-state generation

- **WHEN** a request carries a control-state generation older than the one the runtime reads for itself
- **THEN** the action is refused for a generation mismatch, whatever else about the request holds

#### Scenario: A current control-state generation

- **WHEN** a request carries the generation the runtime reads for itself, and the policy authorizes the action
- **THEN** the action is authorized

#### Scenario: The control state cannot be read

- **WHEN** the runtime cannot read its own control-state generation
- **THEN** the action is refused, and the refusal says the state could not be read rather than naming the policy

## MODIFIED Requirements

### Requirement: A runtime says why it stopped

A runtime that ends on a failure SHALL record that it stopped and what ended it,
naming the part of its own work that failed from a closed vocabulary. An ending
nobody asked for and one that was requested SHALL be distinguishable by the
result of that record rather than by its presence.

Which of the two it is SHALL be decided by whether the stop was asked for, and
not by which of several ready answers a runtime happens to take first. The
operator socket's server runs on the loop's own context, so cancelling it ends
the server too and both answers are ready at once: measured 2026-09-27, the same
signal and the same act were recorded twice as a socket failure on the error
stream and twice as an ordinary ending in the journal. A socket that ended
because the runtime was asked to stop SHALL take the ordinary ending.

Measured 2026-09-26, the root daemon ended and left no account: a
`daemon_started` with no `daemon_stopped` before it, nothing above `info` in
either log, and a count of restarts under launchd as the only trace. Eleven
exits of its observation loop became one exit code, and the one ending that
recorded anything was the cancelled context, which needs no explanation.

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

#### Scenario: The socket ends because the stop was asked for

- **WHEN** the operator socket's server returns because the runtime's context was cancelled
- **THEN** the ending is recorded as the one that was asked for, whether or not the server said why it returned

#### Scenario: The journal is what failed

- **WHEN** a runtime stops because a log record could not be written
- **THEN** the stop is reported through the other log rather than the one that failed

#### Scenario: Nothing can be written at all

- **WHEN** neither log can be written as the runtime stops
- **THEN** it stops anyway, with the status the failure calls for
