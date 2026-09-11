## Why

The stores on this machine occupy 778 megabytes and contain 238. Records average
1.25 kilobytes and take a four-kilobyte block each, so the disk charges more than
three times what the bounds count.

The archive is bounded at 256 megabytes. It held 96.7 and occupied 315 — over a
bound it had not reached. At its bound it would occupy about 840, and the two
journals beside it another 660: a gigabyte and a half where the configuration
says 456 megabytes.

The age bound never applied either. Thirty days was configured; at twelve
megabytes a day the byte bound arrives in about twenty-one, so the window an
operator would read off the configuration was a number nothing ever used.

## What Changes

The archive's size bound counts what the filesystem charges, and what it reports
as its size is the same number. The age window becomes seven days, which is
short enough to be the one that bites.

## Capabilities

### Modified Capabilities

- `local-event-archive`: the size bound counts occupied space, and the age
  window is one an operator can state.

## Impact

- `internal/diskusage` (new) — what a record takes and what one will take.
- `internal/eventarchive` — the bound, the reported size, the default window.
- On installation the archive trims to 256 megabytes occupied, from 315. About
  fifteen thousand of the oldest records are dropped, and the window narrows
  from eight days to seven. That is the change doing what it was asked to.

Not in scope: the spool and the journals carry the same defect. Their bound is
the one that was left alone deliberately, and correcting their accounting means
restating every size assertion in their tests — mechanical, and not safe to do
quickly.
