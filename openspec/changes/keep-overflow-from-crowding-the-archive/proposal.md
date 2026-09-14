# Keep overflow from crowding the archive

Linear: HEX-18

## Why

The archive writes a critical record naming what it evicted, and a critical record
is never evicted for size. It writes one such record for every append that evicts
anything, so past its bound it writes them at about the rate records arrive, and
they push out the operational records they report on.

Measured on the live root archive on 2026-09-14 at 17:49Z: 65,536 records, of
which 14,832 were overflow records and 1,929 connectivity baselines. The oldest
operational record was three days and five hours old in an archive meant to
answer for seven. In the last day it wrote 7,954 overflow records against 16,272
operational ones.

The soak of the tunnel rule reads operational records. A projection from those
numbers, not a measurement: operational retention falls below a day around
2026-09-19, and appends start being refused around 2026-09-21, when nothing but
critical records is left to evict. The soak ends on 2026-09-21.

The age bound has the same shape and has not bitten yet. A record expires about
as often as one is appended, so an overflow record per expiring append is one per
append — and each of those expires a week later and writes another.

A third defect was found measuring the first. Choosing what to evict for size
counts a record's contents, and the bound it frees room under counts disk blocks.
In this repository's tests a record of about half a kilobyte on a four-kilobyte
block was evicted eight at a time to free one block's excess; by the same
arithmetic, not measured, the live archive's records of about 1.25 kilobytes go
about three at a time. That is a batch by accident, of a size the filesystem sets.

## What

- An eviction for size frees a sixty-fourth of the bound beyond what the append
  needs, counted in what the filesystem charges, and is named in one overflow
  record per class dropped. At the default bound that is about a thousand records
  per overflow record rather than one to eight.
- An eviction for age waits until the oldest record is a sixty-fourth of the
  window past it, and then evicts everything outside the window, named the same
  way. At the default window a record is kept up to about two and a half hours
  beyond seven days.
- The overflow record stays durable and written when the eviction happens. A
  summary kept in memory and written later was rejected: a crash would lose the
  only record that records were lost.

## Impact

- Affected specs: `local-event-archive`
- Affected code: `internal/eventarchive`
- The root daemon has to be installed from this branch, with the reinstall
  shorter than three cycles so the soak sees no hole.
- The records already written stay. Size eviction does not remove them; the age
  bound removes them over the week after installation.

## Closing measurement

On the live archive, a day after installation: overflow records written in the
last day fall from thousands to tens, and the oldest operational record is older
than it was at installation rather than younger.
