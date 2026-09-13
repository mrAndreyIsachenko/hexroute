# Read the tail, not the journal

## Why

The root daemon runs for two and a half minutes after launchd starts it before
it logs `daemon_started`, and observes nothing in that window. It has been
measured three times — 169.9, 165.9 and 152.1 seconds — recorded twice as
standing, and never diagnosed.

Diagnosed now, by sampling the process through the window: every sample is
`encoding/json` and `jsoncanonicalizer.Transform`. Startup replays the facts
accepted after the checkpoint's watermark, and the way it finds them is to
decode every record both journals retain and keep the ones whose fold position
is above it.

Measured on the machine the same hour: the checkpoint the pointer names is
generation 794, written a minute earlier, at fold position 144,670. The newest
records stand at 144,670 and 144,674. So the daemon decodes 136,397 records to
find four.

## What Changes

- The records after a watermark are read from the newest backwards, stopping at
  the first one at or below it, rather than by reading everything and filtering.
- The watermark a broken lineage rebuilds from is taken from the newest record
  of each journal rather than from all of them.
- The spool says which sequences it holds without opening any record, and hands
  back one record by sequence.
- Opening the event archive takes its highest sequence from the listing rather
  than by decoding every record. Found by measuring what the rest of this change
  left standing: seventeen of the thirty-three remaining seconds.

## Impact

- Affected specs: `observable-connectivity-state-machine`, `bounded-spool-operation-cost`, `local-event-archive`
- Affected code: `internal/connectivityjournal`, `internal/connectivityruntime`, `internal/spool`, `internal/eventarchive`
- The offline verifier keeps reading everything. Proving a whole lineage is what
  it is for, and it does not run on a daemon's startup path.

## What this rests on

That a journal's fold positions rise with its spool sequences, so the records
after a watermark are a suffix. Measured rather than assumed: over the newest
three hundred records of each journal, the fold position never goes backwards,
not once in six hundred.

The assumption is also guarded rather than trusted. `RecordsAfter` already
reports whether the range it returns is continuous from the watermark, and its
caller publishes uncertainty rather than folding a broken one — so a suffix that
turned out not to be one is refused by the check that already exists.
