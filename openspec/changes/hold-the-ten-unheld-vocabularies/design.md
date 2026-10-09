# Design: hold the ten unheld vocabularies

## Context

See proposal.md — Why. The audit that sized this is in the proposal's table, and
one line of it decides the design: `Lifecycle` and `AggregateState` are strict
subsets of `ComponentState`, whose seven values are exactly the rows of
`### Component states`.

`tests/reference_documentation.py` already holds a vocabulary to a section. The
question is what "explained there" has to mean when three vocabularies share a
section and their words.

## Goals / Non-Goals

Goals: all sixteen published vocabularies held; three vocabularies that share
words each explained in their own right; `Reason`'s twelve values explained
where a reader will look.

Non-Goals: holding a value to having a producer; holding
`### A component row`'s fields; changing any vocabulary; restructuring the three
sections whose tables belong to another vocabulary, which the previous change
recorded as its own tightening.

## Decisions

### Each of the three gets its own rows under one heading

`### Component states` becomes three tables under one heading rather than three
vocabularies sharing one table: what a collector asserted, what the read model
derived, and what the summary says of the host. The words repeat across them,
which is the point — a reader comparing the three sees that `degraded` is three
claims and which one is in play.

Alternative: three sections. Rejected, because the distinction is the reason to
read any of them and separating them invites reading one and not the others.

That leaves the section owning three vocabularies, so the check is keyed to the
section **and** to a subheading within it, which the reader already supports:
the heading is a string, and `#### What a collector asserted` is as good a key as
`### Component states`.

### `Reason` gets a section after the component row

The field is already named there — `reason`, `payload` — so the values follow
where the field was introduced. Twelve rows, read from the mappers rather than
paraphrased from the names, which is how the payload sections were written and
which turned up six quantities that say something other than their names.

### The subset case is a gate, not a convention

A vocabulary that is a subset of another is the case this change found by
measurement. The gate holds it: given two vocabularies and a place, it refuses
when one's values are satisfied only by rows belonging to the other. That is a
small addition to the reader, and it is what stops this recurring the next time
a vocabulary is added inside another's range.

Alternative: rely on the subheading keying alone. Rejected — it works only while
someone remembers to key the subheading, and the whole point of the last two
changes is that the gate should not depend on remembering.

### The seven already-explained vocabularies get a check and nothing else

Their values are explained in the place that owns them, about the thing they
are. Six are inside their payload field's own rows, which is where a reader of
that field will be. Adding separate tables for them would repeat the payload
sections for the gate's benefit.

## Risks / Trade-offs

- **Three tables under one heading read as repetition.** → They are repetition,
  deliberately: the same words with three meanings is the thing being
  documented. The heading says so in a sentence before the tables.
- **The subheading becomes the key and someone renames it.** → A renamed
  subheading makes its check refuse, as a renamed heading already does.
- **`Reason`'s twelve explanations are written by someone who did not design
  the collectors.** → Each is read from the mapper that emits it, and where the
  mapper's intent is not evident the row says what it is emitted for rather than
  guessing why.

## Migration Plan

Documentation and one gate. Nothing is installed, no runtime restarts, rollback
is the commit.

## Open Questions

- Whether `ComponentState`'s two extra values — `stale` and `conflict` — belong
  in the collector's table as "never asserted by a collector". It would make the
  three tables comparable at a glance, and it would also put a word in a table
  about a vocabulary that does not contain it, which is what this change is
  against. Answered when the tables are written.
