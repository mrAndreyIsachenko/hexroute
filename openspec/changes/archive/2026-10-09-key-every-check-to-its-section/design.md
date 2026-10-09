# Design: key every check to its section

## Context

See proposal.md — Why. Two things were established before this was written.

- The audit, value by value: every value of all seven vocabularies is explained
  in its own section today, and six of the seven have values that also appear
  elsewhere. Only `proposal class` is confined.
- The demonstration: the row explaining the component state `degraded` was
  deleted from `### Component states` and the gate exited 0, because the word
  survives in the `managed_transports` payload section.

`tests/payload_documentation.py` already does this correctly for the payloads,
and the seven checks join it rather than each growing a parser.

## Goals / Non-Goals

Goals: each check reads only the section that owns its vocabulary; a missing
section is a refusal; a value explained there and no longer declared is a
refusal; the gate says what it held.

Non-Goals: changing any vocabulary; holding a value to having a producer, which
is the other half of the reading and stays open; auditing documents other than
the connectivity reference.

## Decisions

### The vocabulary comes from the code, the place comes from a declaration

The values are read from the typed constant blocks, as the gate already reads
them. The section that owns a vocabulary cannot be derived from the code — it is
the document's structure — so the gate declares seven headings.

That is a list, and this repository has paid twice for lists beside code. The
difference is what the list holds: not the values, which is the part that
changes and which the code still provides, but the place to look. A heading that
is renamed or removed makes its check refuse rather than widen, so the list
cannot rot quietly — which is exactly what the old flat check did.

Alternative: a marker in the document, so each section declares the vocabulary
it owns. Rejected: it writes the document for the gate, and the heading is
already the structure a reader navigates by.

### A missing section refuses rather than widens

A check whose section is absent refuses. The flat behaviour — treating the
whole document as the section — is the defect being removed, and keeping it as a
fallback would reintroduce it the first time a heading changed.

### Forward for all seven, reverse only where the rows align

A value its section does not explain fails. That is the fix for the
demonstrated defect, and it applies to all seven checks.

The reverse direction — a row in the section for a value the code no longer
declares — applies to four of them. Measured: `component`, `component state`,
`classification` and `proposal class` have exactly as many rows as values and
nothing else. `authorization` and `diff reason` do not: their sections' tables
are occupied by **another vocabulary** — `## Authorization` tabulates the
authorization *reasons* and explains `authorized` and `unauthorized` in prose;
`## The diff` tabulates the classifications and explains the twelve reasons in
prose. The `source` check's values sit in the second column of the component
table rather than the first.

So the reverse check is applied where it is sound and not where it would demand
restructuring the document. Requiring rows everywhere is a second tightening
with its own justification, and conflating it with this one would mean rewriting
three sections to satisfy a gate rather than a reader.

### One helper, not seven

`tests/payload_documentation.py` becomes the shape for all of it: a reader given
a vocabulary, a section and a document. The payload check keeps its own entry
point because its places come from the `Payload` struct rather than from a
declared list.

## Risks / Trade-offs

- **A section legitimately explains a value of another vocabulary.** → The
  reverse check names what it did not expect instead of failing silently, and a
  row that belongs there is recorded as expected rather than removed from the
  document.
- **The seven headings drift from the document.** → Each drift is a refusal, not
  a pass. That is the whole change.
- **Seven checks become seven section reads, and the gate gets slower.** → It
  reads one file seven times; the document is 300 lines.

## Migration Plan

One gate and possibly a few rows in one document. Nothing is installed, no
runtime restarts, rollback is the commit.

## Open Questions

- Whether `### A component row`'s fields should be held the same way. They are a
  vocabulary in the same sense — the fields of a published record — and the
  payload check already holds the payloads' own. Answering it is a reading of
  the record type, and it changes no requirement here.
