## Why

On 2026-09-26 the root daemon ended at about 09:04:00 and left no account of it.
There is a `daemon_started` at 09:04:03, no `daemon_stopped` before it, nothing
above `info` in either log, and `runs = 23` under launchd. The tunnel its last
act had started was gone with it, and because this runtime held the tunnel claim
the previous owner was standing down: the machine went about a minute with no
tunnel at all.

The daemon cannot say why it stopped. Eleven places in the observation loop
return an error, `Run` turns any of them into `return 1`, and none of them
records anything. `daemon_stopped` is written on exactly one path — a context
that was cancelled — so the record exists for the one ending that needs no
explanation and for none of the ten that do.

This is the instrument, not the fault. A defect that ends the daemon cannot be
diagnosed from the outside: an operator reading the logs sees a runtime that
started, and has to infer from a gap in the cycles that one had ended. The seven
days of tunnel ownership this repository is working towards would collect those
gaps and explain none of them.

## What Changes

- A runtime that stops on a failure SHALL record that it stopped and what ended
  it, naming the part of its own work that failed, from a closed vocabulary.
- The record goes to the error log rather than the journal it may have failed to
  write, and failing to write it SHALL NOT change the exit.
- The ordinary ending keeps the record it has: a cancelled context is still
  `daemon_stopped` with `ok`.

## Capabilities

### Modified Capabilities

- `local-control-plane-foundation`: what a runtime records about its own
  ending. The capability already requires a bounded redacted journal and that a
  runtime can report quantities about itself; it does not require it to say that
  it stopped, and the implementation says so for one ending in eleven.

## Impact

- `internal/rootdaemon`: the observation loop's error returns, and `Run`, which
  has both logs in scope and is where the record belongs.
- `internal/logging`: the reasons a stop can name.
- `internal/userdaemon`: the same question, asked of the other runtime rather
  than assumed to be different.

## Non-goals

- No change to what ends a runtime. Every failure that is fatal today stays
  fatal; this is about the record it leaves.
- No diagnosis of the stop of 2026-09-26. It cannot be established from what was
  recorded, which is the point; what this change buys is that the next one can.
- No new transport. The record is a log line in the vocabulary already allowed.

## Rollout

Install the binaries and restart in the ordinary way. A runtime that never fails
logs exactly what it logs today.

## Rollback

Reinstall the previous binaries. Nothing on disk carries the new state.

## Production ownership boundary

The local control plane on the operator's own machine. Twilight owns the tunnel
throughout and is untouched; the cloud is not involved.
