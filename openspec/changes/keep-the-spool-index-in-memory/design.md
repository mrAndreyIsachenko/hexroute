## Context

The same shape as the archive, in the store beside it:

```
Append → scanIndex()   lists the spool directory, sorts it, stats each entry
       → stage/commit  writes one record
```

Eleven times a cycle, into a directory inside the 107,600-file connectivity
state. The archive's fix took its half; this takes the other.

## Decisions

**Only committing amends the listing.** The archive amends on publish and on
eviction because both happen on the append path. The spool has two more ways to
move a stable record — acknowledging an upload and quarantining a damaged
record — and both are rare beside appending. They drop the listing rather than
amend it: paying one listing there is cheaper than reasoning about whether the
amendment is right, and being wrong there would be a spool that thinks it holds
a record it has uploaded away.

**`Open` and `recover` still read the disk.** They are the two operations whose
whole purpose is to find out what is actually there, and neither is on a cycle's
path.

**The published record's size comes from the filesystem.** One `Lstat` per
published record, as in the archive, rather than the length of the bytes handed
in — the byte bound is about what the directory holds.

## Risks

Both stores now rely on being the only writer of their directory. That was
already true and is now depended upon; the requirement says so in both
capabilities rather than leaving it as something a reader must infer.
