# Design

## What eviction actually needs

`chooseEvictions` uses three fields of a stored record: `Sequence` (to name the
file it removes), `Size` (to know when enough has been reclaimed) and `Priority`
(to take diagnostics before operational, and critical only under duress). It
touches neither the metadata nor the event.

Two of the three come from the directory and are already in `stableRecord`. The
third does not: priority is written inside the record. That single missing fact
is the whole reason `Append` reaches for `scanStable`, and `scanStable` pays for
it by decoding the event, canonicalising it and re-marshalling the entry to
verify its size — per record, per append.

## Decision: keep the priority beside the listing

`stableRecord` gains `Priority`. It is empty until something needs to evict,
because a spool below its bound never classifies anything, and the listing is
the one thing appending is already allowed to keep.

Where it comes from:

- A record the spool itself published: from the staged `Entry`, free, in
  `noteCommitted`.
- A record already on disk when the listing was taken: read once, by
  `readPriority`, which decodes the wire envelope's `schema`, `sequence` and
  `priority` and stops. It does not canonicalise, does not re-marshal, and does
  not look at the event.

Because the listing is kept across appends and amended as records are published
and evicted, the reading happens once per record rather than once per append. A
spool at its bound reads each record once ever and then evicts from memory.

### Why not the filename

Encoding the priority into the stable name would remove the read entirely. It
would also rename eighty-four thousand live records, and a spool half migrated
is a spool whose listing cannot be parsed. The read is paid once; the migration
would be paid on a machine that currently has no working observer.

### Why not a ledger outside the listing

A separate `map[sequence]priority` outliving the listing would survive
`forgetIndex` and save re-reading after a quarantine or an acknowledgement. It
would also need pruning, because `Acknowledge` removes records without telling
it, and an unpruned ledger grows for the life of the process. Tying the fact to
the listing means it is dropped exactly when the listing is dropped, and nothing
has to reason about when a remembered priority stopped being about a record that
exists.

## Decision: one eviction path, over what the directory reports

`chooseEvictions`, `commit` and `noteCommitted` take `stableRecord` rather than
`Entry` for the records being evicted. `commit` only ever used `Sequence`; the
`Entry` values it received were the residue of having decoded them.

`recover` also chose evictions from a full decode. It now uses the same path,
which is why the pending-record branch of recovery stops costing a decode of the
whole spool.

`Entries`, `EntriesBySequenceRanges` and `Acknowledge` still decode, and must:
they hand records out, and the requirement to prove a record before handing it
out is unchanged. None of them is on a path the observing daemons take.

## Decision: an unclassifiable record is quarantined, and eviction retries

`scanStable` today quarantines a record it cannot prove and carries on with the
rest. The classifying path must behave the same way, or a single damaged record
makes a full spool unappendable.

Quarantine renames the record out of the directory, which invalidates the
listing, so the classifying loop quarantines everything it could not read,
forgets the listing and starts over. It terminates because every iteration
removes at least one record from the directory and adds none.

## What this does not do

The spool stays full. Nothing in the observing runtime calls `Acknowledge` —
only the telemetry uploader does, and the daemons do not run it — so every
append from here on evicts. That is now cheap, and it is still a spool that
retains nothing longer than its bound allows. Whether a queue nobody drains
should exist is a real question and it is not this change's.
