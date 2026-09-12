# Pay for eviction, not for being full

## Why

A spool that has reached its byte bound decodes every record it holds on every
append. The live root runtime stopped observing because of it: the daemon came
up at 17:32:58, wrote its last heartbeat at 17:35:54, and four hours later was
still running, burning a core in `jsoncanonicalizer.Transform` and
`encoding/json` under `spool.scanStable`, reached from `spool.Append`. Nothing
recorded the stall, because the thing that would have recorded it was the append
that never returned.

The baseline already forbids this. `bounded-spool-operation-cost` says the cost
of appending "SHALL NOT include opening or decoding any stored record". The
implementation honours that only below the bound; above it, `Append` calls
`scanStable`, which reads, canonicalises and re-marshals every stored record to
learn three numbers per record — sequence, size and priority — two of which the
directory already reports.

The requirement was not wrong. Its scenario was: it qualified the full-spool
case as "inside its byte bound", and that qualifier is exactly the case the
implementation does not meet. A scenario that excludes the failing case is how a
green gate coexists with a wedged daemon.

Why now rather than when this was first deferred: at that time the spool was not
full, so the path was not taken. Nothing drains this spool — `Acknowledge` is
called only from the telemetry uploader, which the observing daemons do not run
— so reaching the bound was not a risk but a schedule, and the schedule came due.

## What Changes

- Eviction chooses from what the directory reports plus one fact per record that
  the directory cannot report — its priority class — and learns that fact by
  reading each record once, not once per append.
- `Append` no longer decodes stored records in any case. Above the bound it
  works from the kept listing, exactly as it already does below it.
- A record whose priority cannot be read is set aside, as it is today on the
  path that decodes it, and the remaining records are still evictable.
- The full-spool scenario is stated without the qualifier that excused it, and a
  regression test drives an append against a spool at its bound and fails if any
  stored record is opened.

## Impact

- Affected specs: `bounded-spool-operation-cost`
- Affected code: `internal/spool`
- Not in scope: draining the spool. Nothing in the observing runtime
  acknowledges uploads, so the spool stays full and every append evicts. That is
  a separate question about what a spool nobody reads is for, and it is not
  answered by making eviction cheap.
