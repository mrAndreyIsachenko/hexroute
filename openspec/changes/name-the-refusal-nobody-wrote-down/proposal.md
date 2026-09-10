## Why

On 2026-09-10 the Pritunl rescue was refused on a live machine and neither
runtime recorded why. Root wrote nothing at all; the asking runtime wrote
`recovery_refused`, which is also what it writes when root fails internally.
The refusal took an afternoon of measurement to characterise and could not be
attributed even then.

The capability already requires that a refusal name the check that made it. Two
paths do not honour it, and both are silent by construction rather than by
accident:

- the dispatcher's mutation gate answers `precondition_failed` above the
  rescuer, before anything that writes a reason exists in the call;
- the broker answers `internal_error` when no reader takes the envelope, and
  nothing in the broker can write.

The asking runtime then erases the distinction that survives the boundary:
`refusalOutcome` maps `precondition_failed` and every unnamed code — including
`internal_error` — to one reason. So a reader cannot tell "root looked and
disagreed" from "root did not get to it", which are the two answers that send
them to different places.

## What Changes

Root records a refusal on every path that produces one, including the two that
answer above the rescuer. The asking runtime keeps apart a refusal from an
internal failure of the runtime it asked.

## Capabilities

### Modified Capabilities

- `pritunl-recovery-ownership`: a refusal is recorded by the refusing runtime on
  every path that can produce one, and the asking runtime does not report a
  refusal and an internal failure as the same thing.

## Impact

- `internal/operator/dispatcher.go` — the mutation gate gains somewhere to write.
- `internal/operator/broker.go` — an envelope nobody took is recorded.
- `internal/userdaemon/recovery.go` — the unnamed-code arm gets its own outcome.
- `internal/userdaemon/run.go`, `internal/logging` — one reason added.

Not in scope: root's observation cycle blocking its own operator loop for
seconds at a time. That is a separate fault, measured but not diagnosed.
