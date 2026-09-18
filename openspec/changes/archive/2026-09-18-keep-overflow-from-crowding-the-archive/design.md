# Design

## The overflow record is written when the eviction happens

Three ways to stop overflow records crowding the archive were weighed.

- **Coalesce in memory** and write one record per span of time. Rejected: an
  eviction whose record is still in memory when the process dies is a loss of
  records with no record of it, which is the one thing the requirement exists to
  prevent.
- **Rewrite an open overflow record** to extend its range. Rejected: records are
  published by rename and never changed, and the crash-safety argument for the
  archive rests on that.
- **Evict more at once.** Chosen. One eviction frees a share of the bound beyond
  what the append needs, and is named in one record per class dropped, written
  as it is today. Nothing new has to survive a crash.

## A sixty-fourth

The share is the same for both bounds, so there is one number to reason about.

For size it is 16 MiB of the default gigabyte — about four thousand records at
one four-kilobyte block each. By arithmetic from the measured day of 2026-09-14,
about seventeen thousand records a day, that is a size eviction every hour and a
half: tens of overflow records a day where thousands were written. That is a
prediction, and the closing measurement is what settles it.

For age it is two and a half hours of the default seven days. Nothing is evicted
for age until the oldest record is that far outside the window; then everything
outside the window goes. A record may therefore be kept up to two and a half
hours beyond seven days. The size bound is the promise about the disk, and it is
unchanged; the window an operator reads is a week, and a week and two hours is
still one.

The retained window for size shrinks by at most the share: just after an
eviction the archive holds a sixty-fourth less than its bound.

## The bound is raised so the window binds

Stopping the overflow records was half of it. The other half is that the archive
answered for 3.6 days while stating seven: at 19,383 records a day, measured
2026-09-15, one block each, 256 megabytes holds that long with no critical record
at all. The bound is a gigabyte, so a week — about 530 megabytes — fits and the
age window is what removes records.

Measured on a generated archive of 136,000 records, the size a week of that rate
reaches: opening it took 0.5 to 2.2 seconds, an append 10.9 milliseconds, an age
eviction batch of 3,806 records 407 milliseconds. One path is expensive: an
append that evicts for size took 6.1 seconds, because choosing by priority reads
every stored record. At this bound that path does not run in a steady state —
the window binds first — and it is recorded as its own issue rather than fixed
here.

## Counting in blocks

`chooseEvictions` counted a record's contents against an excess counted in
blocks, so it chose as many records as fit in one block. It now counts what each
record occupies — from the index, which already holds it, and by the same
arithmetic as a write for a record the index does not know.

"Covered" keeps its meaning: the append can go ahead if what can be evicted
covers what it needs, even when it falls short of the share. Refusal still means
only critical records are left.

## What it costs

The age walk still reads the oldest record and then only expired ones. Size
eviction still reads records only when the bound is reached, and now reaches it
about a thousand times less often at the default bound.

## Not changed

A refused append still writes an overflow record per refusal. A refusal means
the archive holds nothing but critical records, and that is the condition this
change removes the main cause of; bounding refusals is a separate question.
