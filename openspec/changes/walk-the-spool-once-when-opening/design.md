# Design

## The three walks

`recover` reads the directory to find what a half-finished write left behind. It
then lists it again, with a stat for every file, to check the total against the
bound. `Open` lists it a third time to learn the highest sequence.

The second and third ask the same question. The spool already keeps what it
learns about its own directory, and recovery is exactly the moment that
knowledge becomes true — so recovery keeps it and opening reads what it kept.

## Why the first walk stays

Recovery must know about pending records before a listing means anything:
committing one renames it into a stable name, and a listing taken before that
would describe a directory that no longer exists. Folding the two would mean
either listing before recovery and discarding it when recovery changed anything,
or making recovery's listing conditional — both trade a plain sequence of steps
for a saving that is a `ReadDir` rather than a stat sweep.

The stat sweep is the cost. Measured on this repository's live spools, `ReadDir`
over 84,000 names takes 199 milliseconds and a stat for each of them 160, so one
of the two remaining walks is nearly free and the other is not.

## What this does not change

Nothing about when the spool forgets. It still drops what it knows whenever a
write may have half happened or anything other than an append moves a record,
and it still learns it again when asked. Opening is not an exception to that
rule; it is the first time the rule applies.
