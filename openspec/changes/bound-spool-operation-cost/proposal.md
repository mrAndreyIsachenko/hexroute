# Stop re-proving the whole spool on every operation

## Why

The root daemon burns most of a core and cannot answer its socket. A spindump
of the live process puts the time in JSON decoding, canonical marshalling and
the garbage they create, beside `open` and `read`. The cause is one line:
`Spool.Append` calls `scanStable`, which reads and fully decodes every stable
entry — every time. Six operations call it, so asking the spool its own size
re-proves all sixty thousand records.

Nothing has overflowed. Both spools sit inside their hundred-megabyte bound at
fifty and twenty-six. The spool is doing exactly what is written down, and the
machine is unusable — so what is missing is a statement about cost, not about
size.

The consequence travels. The user daemon publishes each cycle with no explicit
timeout, so it inherits the fifteen-second default and stalls for a whole cycle
behind a slow peer. Today that made a healthy user daemon look dead while the
fault was in root, and the misdirection cost hours.

## What Changes

- Prove an entry when it is read for use, not when it is counted. An operation
  that needs sequences and sizes reads the directory and the file metadata; the
  payload is decoded where an entry is actually handed out. `Append` currently
  re-marshals every entry to compute a size it then compares against the size
  the filesystem already reported.
- A damaged entry stops its own use and nothing else. Today one unreadable file
  refuses every append, which converts losing an old record into losing all
  future observation. The entry is set aside, still on disk, and reported.
- Give the connectivity publish an explicit timeout derived from the observe
  interval, so a slow peer costs one publication rather than the cycle. The
  graceful path already exists and is deliberate: a fact held back and sent
  later would describe a moment that has passed.
- **BREAKING** for one behaviour: a corrupt entry no longer fails `Append`. That
  is the point — recording today's observation must not depend on the integrity
  of a record from three weeks ago.

## Capabilities

**New Capabilities**

- `bounded-spool-operation-cost` — what a spool operation may cost, and when the
  integrity of a stored record is proved.

**Modified Capabilities**

- `local-control-plane-foundation` — the bounded journal gains a bound on the
  cost of using it, and states what a damaged record stops.

## Impact

`internal/spool` changes how `Append`, `Size`, `Open` and `Acknowledge` obtain
what they need; `Entries` and `EntriesBySequenceRanges` keep decoding, because
they hand entries out. `internal/userdaemon` gives its publish an explicit
deadline. The spools on this machine are left as they are: after the change
their size stops mattering, which is the point.

## Non-Goals

- **Bounding the entry count.** The byte bound is deliberate and holding. A
  count bound would silently drop observations the design promises to keep until
  acknowledged, and that is a decision about what the system remembers — not one
  to be taken under pressure from a performance fault.
- **What the spool retains and for how long.** Its drain is the cloud uploader,
  which does not run here, so it fills to its bound and stays there. Whether
  undeliverable telemetry should age out is a real question and a separate one.
- **Trimming the existing store.** Sixty thousand records stop being a problem
  rather than becoming one to clean up.
- **An in-memory index.** Disk is the truth in this design, which is why every
  operation re-reads. A cache reintroduces the divergence the re-reading avoids,
  and it diverges quietly.
- **The abandoned state directories.** `connectivity.pre-fold-position` (10,000
  files), `connectivity.desynced` (2,000) and the `*.superseded` copies are
  leftovers of earlier migrations, outside every hot path. They are recorded
  here so that whoever decides their fate does it by reading rather than by
  tripping over them.

## Rollout

Nothing to install and no state to migrate. The change is code; the daemons pick
it up at their next restart, and the existing spools are read the same way they
were written.

## Rollback

Revert. Nothing is written in a new format, no record is moved and no pointer
changes, so a reverted binary reads exactly the same directory it did before.
The one behaviour that does not revert cleanly is the setting-aside of a damaged
entry: a record quarantined under this change stays quarantined, which is the
safe direction — it is still on disk and nothing has deleted it.

## Ownership boundary

Public Hexroute owns the spool, the journal and the publish deadline. The
private repository owns the measurements taken from the operator's machine and
the evidence for the live acceptance. Twilight is untouched.
