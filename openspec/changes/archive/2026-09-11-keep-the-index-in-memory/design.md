## Context

```
Append → index()      lists 69,000 entries, sorts them, stats each
       → expiredByAge reads the oldest record
       → stage/commit writes one record
       → index()      lists them all again when anything was evicted
```

Eleven times a cycle. The listing is O(records stored) and the cycle multiplies
it by records appended, so the cost is O(stored × appended) — which no bound in
this system caps, because the size bound caps only the first factor.

## Decisions

**The listing is kept on the archive, under the lock that already guards it.**
The archive is long-lived and single-writer; the alternatives — a sidecar file
holding the count, or a sequence counter without sizes — either add a thing that
can disagree with the directory or cannot answer the size question the byte
bound needs.

**Any failure drops it rather than amending it.** A rename that succeeded and a
sync that did not leaves a directory this archive cannot describe. Paying for a
fresh listing there is the cheap half of the trade; answering from a stale one is
the expensive half.

**The published record's size comes from the filesystem, not from the caller.**
One `Lstat` on the file just renamed, rather than the length of the bytes handed
in. They are the same today and the byte bound is about what the directory
holds, so the directory is what should be asked.

**Noticing a foreign writer is given up deliberately.** Re-listing per append
would notice a record added or removed by something else. Nothing does that: the
runtime opens the archive, the review tool opens it read-only, and the directory
is not writable by anyone else. The test helpers that plant aged records are the
one thing that makes it untrue, and they now say so.

## Risks

The saving is real and partial. One cycle's eleven appends against 20,000 stored
records cost 767ms listing each time and 109ms listing once; at the 69,000 the
live archive holds, that is roughly 2.7 seconds returned per cycle out of a pause
of six to thirteen. What accounts for the rest is not established, and this
change does not claim it.
