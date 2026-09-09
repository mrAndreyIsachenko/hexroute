## Why

The event archive reads and decodes every record it holds on every append.
`Archive.scan()` does an `os.ReadFile` and a `json.Unmarshal` per stored record,
and `Append` calls it unconditionally — twice when anything is evicted. The
connectivity journal mirrors every fact into the archive synchronously, so each
fact the root daemon folds pays that cost.

This is the same defect `bounded-spool-operation-cost` fixed in `internal/spool`
and left in `internal/eventarchive`.

It stopped being theoretical. With 41,492 records in the live root archive, one
scan costs about 900 ms; a publication carries two facts, so a publication pays
at least 1.8 seconds and the daemon pays it again for its own events. The root
daemon burned 23.3 CPU-seconds in every 30 seconds of wall time, and a CPU
profile attributes the burn to this package — 388 samples in `runtime.syscall`,
with `encoding/json` and garbage collection behind it.

The visible consequence was a user daemon whose every connectivity publication
was refused for about a day: the publisher bounds a publication at a third of
its cycle, five seconds, and root could not answer inside it. Nothing about the
network was wrong, and nothing about the exchange was wrong. The archive was.

## What Changes

- An append no longer reads or decodes stored records on the path that does not
  evict. Sequence and size come from the directory and from file metadata, as
  they already do in the spool.
- Age eviction stops requiring every record's timestamp. Records are named by a
  monotonic sequence and appended in order, so the oldest retained record is the
  lowest retained sequence. The append reads that one record to learn whether
  the age bound could be reached, and reads further records only while they are
  actually expired.
- Size eviction keeps choosing by priority and therefore still reads records —
  but only when the byte bound is actually approached, which is the path that is
  about to rewrite the directory anyway.

Not breaking. No stored record changes shape, no filename changes, and nothing
already written needs migrating.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `local-event-archive`: gains a requirement stating what an archive operation
  may cost, and what may be read to satisfy a bound. The bounds themselves are
  unchanged.

## Non-goals

- **Retention is not changed.** The archive keeps the same maximum age and
  maximum total size, evicts on the same conditions, and answers for the same
  window. Nothing that is retained today is dropped after this change. An
  entry-count bound is a separate question about what the archive should keep,
  not about what an append should cost, and it is not asked here.
- **The mirror stays synchronous.** A fact is written to the journal and to the
  archive together or not at all. Making the mirror asynchronous would buy
  nothing once the append is cheap, and would trade that property for records
  lost on a crash.
- **The publication deadline is not revisited.** The five-second bound is what
  `bounded-spool-operation-cost` chose deliberately, so that a slow peer costs
  one publication rather than the cycle. It exposed this defect; it did not
  cause it.
- **`internal/spool` is not touched.** Its entry-count bound is separate work.

## Impact

- `internal/eventarchive`: `Append`, `scan` and the age partition.
- `internal/connectivityjournal`: no change, but it is the caller that pays the
  cost twice per record through its mirror, so its behaviour is what the
  regression is measured against.
- The root daemon's CPU load and its ability to answer IPC inside a caller's
  deadline.

## Ownership boundary

This change belongs in public Hexroute. It is generic daemon code with no
provider identity, no live endpoint and no deployment evidence. Nothing here is
owned by hexroute-infra or by Twilight, and the production ownership boundary is
unaffected: Twilight remains the active production owner.

## Rollout

Ordinary build and install of the root daemon through
`scripts/macos/observe-root-launchd.sh`. No store migration, no format change,
no coordinated restart with the user daemon.

## Rollback

Reinstall the previous root daemon binary with the same script. The archive on
disk is untouched by this change — same filenames, same record shape — so a
binary from before it reads everything written after it. Rollback is one
command and does not depend on anything this change installs.
