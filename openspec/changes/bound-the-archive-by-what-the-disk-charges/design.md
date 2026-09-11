## Context

| store | contains | occupies | files | per record |
|---|---|---|---|---|
| event archive | 96.7 MB | 315 MB | 80,624 | 1,258 B |
| journal, root domain | 50.5 MB | 165 MB | 42,220 | 1,254 B |
| journal, user domain | 90.6 MB | 298 MB | 76,170 | 1,247 B |

## Decisions

**Stored records are measured, not estimated.** `st_blocks` is what the
filesystem charged and it is exact. Rounding the record's length to a block
would be a guess about the filesystem, and a compressed or inlined record would
make the guess wrong in the direction that matters.

**A record not yet written is rounded up.** It is the one number the filesystem
cannot be asked for, and counting it at its length would admit one record more
than the bound meant to, on every append.

**Both places the archive learns a size are covered.** It learns one when it
publishes a record and one when it reads its directory, and they are different
code. If they disagreed, what the bound counts would depend on whether the
archive had been restarted — which is why the regression reads a record's size
both ways and requires them equal.

**What the archive reports is what it counts.** Two numbers differing by a
factor of three, one called "size" and one enforcing the bound, is how this was
invisible for as long as it was.

**Seven days rather than a larger byte bound.** The window an operator reads off
the configuration should be the one that applies. At the current rate seven days
is about 88 megabytes of records and 256 megabytes of disk, so the two bounds
now arrive together instead of one hiding the other.

## Risks

Installing this evicts. The archive holds eight days and 315 megabytes; it will
trim to seven days and 256 megabytes occupied, dropping about fifteen thousand of
the oldest records and writing overflow records that say so. That is the change
doing what it was asked to, and it is stated here so it is not discovered.
