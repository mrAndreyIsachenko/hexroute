# Design

## What the silence cost

The reading that prompted this is one line long: `daemon_started` at
2026-09-26T09:04:03Z, and nothing before it. The cycle at 09:04:05 then reported
`running=false, complete=false, tick_gap=0` — a first cycle, with no tunnel and
no previous cycle to compare against — and the tunnel stayed down until 09:05:04.

Everything in that paragraph was read from the archive and from launchd. Nothing
in it says what ended the previous process, and nothing can: the loop returns an
error and `Run` turns it into an exit code.

## Where the record belongs

Not in the loop. The loop has one logger, the one writing to stdout, and three
of its eleven exits are that logger failing — a record emitted there would be
attempted through the thing that just broke.

`Run` has both. It builds `infoLog` over stdout and `errorLog` over stderr before
anything else, and it is where the exit code is decided. So the loop returns what
ended it alongside the error, and `Run` records it.

That also keeps the loop's signature honest about what it knows. A stop reason is
not a detail of a cycle; it is the loop's answer to a question only its caller
asks.

## A name, not a message

The log record carries a fixed field set and a closed vocabulary, because these
logs are collected and this repository is public. So what failed has to be a
name, and the exits collapse into five kinds:

| What failed | Reason |
|---|---|
| the runtime was built wrong | `invalid_runtime` |
| a log record could not be written | `journal_unwritable` |
| the connectivity publication failed | `publication_failed` |
| the control state could not be written | `control_state_unwritable` |
| the operator socket ended | `operator_socket_ended` |

Five rather than nine, because a reader looking for the cause wants the part that
failed rather than the line number. Five rather than one, because one would be
`stopped_on_error`, which is what the exit code already says.

Two names that looked obvious are not there, and reading the code is what
removed them. The read model reports every failure it has of its own —
observing, sampling, recording — and carries on, so the only error its fold
returns is the log refusing a record: an exit naming the read model would claim a
distinction the code does not make. The event archive is the same. So the fold's
exit is the journal, and there is no `read_model_unwritable` or
`archive_unwritable` to name.

One name covers two routes to the same thing. The user runtime writes its
snapshot to disk and updates its controller with the same state, so both exits
answer `control_state_unwritable`: a runtime whose control state cannot be
written is answering the operator with something it did not write, whichever
half failed.

And one call fails for two different reasons. `emitSummary` returns the log's
refusal and also its own `ErrInvalidConfig`, for a cycle state or a route role
this runtime does not know — the first is the journal and the second is the
runtime, and naming the journal for both would send the reader to the wrong
subsystem.

## The result is degraded, and the event is the one that exists

`daemon_stopped` with `degraded` rather than a new event name. The ending is the
same ending; what differs is whether it was asked for. A reader filtering for
`daemon_stopped` should see every stop, and one filtering for `result != ok`
should see the ones nobody asked for — and neither works if the unplanned stop
has a name of its own.

## Failing to say it does not change what happened

The record is attempted and its own failure is dropped. A runtime that could
write neither log still stops, and the exit code is the same; allowing the record
to fail the stop would mean a broken stderr turning a legible exit into an
illegible one.

## Both runtimes

The user daemon is asked the same question rather than assumed to be different.
Its loop is its own, and a defect that ends it is as invisible as this one was —
this repository has already been caught writing a check for one side and
reporting the other side ready.
