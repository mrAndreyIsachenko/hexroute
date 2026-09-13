# Design

## Why a suffix is a suffix

A journal appends a record when a fact is folded, and the fold position rises
with every fold. Its spool names records by an increasing sequence and appends
in the same order. So within one journal the fold positions rise with the
sequences, and the records after a watermark are the end of the list.

Measured before it was relied on: over the newest three hundred records of each
journal on the live machine, the fold position never went backwards — not once
in six hundred. That is evidence about this data rather than a proof about the
code, which is why the guard below matters.

## Decision: walk backwards and stop

`RecordsAfter` reads sequences newest first, decoding one record at a time, and
stops at the first connectivity fact whose fold position is at or below the
watermark. It collects what it passed on the way, reverses it, and hands it back
in the order it was folded.

A record that is not a connectivity fact is stepped over rather than stopped at:
the spool is shared with other event classes and one of theirs at the end of the
list says nothing about where the facts are.

A record that cannot be decoded is still an error rather than a skip. Dropping
one silently would turn a corrupt journal into a shorter healthy-looking one,
which is the reason the whole-journal reader gave for refusing, and walking
backwards does not change it.

## The guard was already there

`RecordsAfter` reports whether the range it returns is continuous from the
watermark, and `resumeUnfolded` refuses a broken one rather than folding it. So
a journal whose order did not hold is caught by the check that already exists,
and the failure is visible uncertainty rather than a silently short replay.

That is why this change does not add an assertion of its own about ordering.
Restating a guarantee beside a guard that already enforces it makes two places
to keep true.

## Decision: the newest record answers the watermark question

`highestIssued` wants the largest host sequence and fold position a journal ever
handed out, and it read every record to take two maxima. With the order above,
the newest connectivity fact carries both.

It runs only when the lineage cannot be proven — which is exactly when a host
has already lost its read model and wants to be observing again quickly.

## Decision: the spool hands out sequences and one record

Both walks need the same two things the spool would not give: which sequences
exist, and one record by sequence.

`Sequences` comes from the listing the spool already keeps, so it opens nothing.
`Entry` reads and proves one record, and reports absence as an ordinary answer —
a caller holding a listing from a moment ago is asking a reasonable question
about a record eviction has since taken.

`EntriesBySequenceRanges` was not reused for this. It refuses rather than
answers when a range runs past its limit, and it takes ranges where the caller
here has one sequence at a time and does not know how many it will want.

## What this does not do

The offline verifier keeps reading whole journals. Proving a lineage end to end
is what it is for, and it does not run on a daemon's startup path.

`LatestBaselines` also reads everything and is left alone: nothing outside the
journal calls it, and changing a reader with no caller would be building on
speculation about how it will be used.
