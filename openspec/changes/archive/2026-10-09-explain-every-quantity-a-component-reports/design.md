# Design: explain every quantity a component reports

## Context

See proposal.md — Why. Three things were measured first.

- Eight payload types in `internal/connectivity/payload.go`, 25 fields, and one
  type with every field in a table row.
- `tests/connectivity_documentation_test.sh` checks eight vocabularies and no
  payload field. Its `require` helper is `grep -qF` for a backtick-quoted word
  anywhere in the document.
- Three words are shared across vocabularies, and a flat check passed on all
  three for the wrong reason.

## Goals / Non-Goals

Goals: every payload field explained under its own component; a gate that cannot
be satisfied by another vocabulary's row; a gate that refuses a stale
explanation as well as a missing one.

Non-Goals: changing any payload; auditing the gate's other checks for the same
weakness; explaining anything outside the component payloads.

## Decisions

### The section heading is the key

Each payload gets a section whose heading names the component as the code
spells it — `### The scoped_routes payload` — and the gate looks for each field
only **between that heading and the next**. A row elsewhere cannot satisfy it,
which is the whole point.

The scoped-routes section added last week is titled "The scoped routes payload",
with a space. It is renamed to the code's spelling so the gate can derive the
heading from the component constant rather than from a second list of titles.

Alternative: a machine-readable block — a table with a component column, or a
JSON file beside the document. Rejected: a reader meets these quantities in
prose and a column would be written for the gate rather than for them. The
heading is already how the document is organised.

### The gate derives the payloads from the code

The payload types and their fields come from `internal/connectivity/payload.go`
by reading the struct declarations and their `json` tags, the same way the
existing checks read a typed constant block. No list of payloads in the gate: a
list beside the code is the thing that does not get updated, which this
repository has now paid for in a configuration nobody recorded and a
documentation gate that held nothing.

### Both directions

A field the reference does not explain fails. So does an explained field no
payload carries. The second is what keeps the document from describing a
quantity that has been removed, and it costs one comparison.

### The gate says what it checked

It prints the number of payloads and fields it held, so a future reader can see
the gate is looking at something. A gate that silently checks nothing is how
`tests/route_coverage_roles_test.sh` nearly passed on an empty read last week —
it refused instead, and that refusal is the model.

## Risks / Trade-offs

- **The component spelling and the heading drift.** → The heading is derived
  from the component constant, so a renamed component fails the gate until the
  heading follows.
- **A field explained in prose rather than a table row.** → The gate looks for a
  row, `| \`field\` |`, which is the shape the document already uses. Prose
  about a field is welcome beside the row and is not a substitute.
- **Twenty explanations written by someone who did not design the payloads.** →
  Each is read from the mapper that fills it, and where the mapper's meaning is
  not obvious from its code the explanation says what it counts rather than
  guessing why.

## Migration Plan

Documentation and one gate. Nothing is installed, no runtime restarts, no
persisted shape changes, and rollback is the commit.

## Open Questions

- Whether the gate's other checks share the flat-match weakness. The proposal
  records this as out of scope; answering it is a reading, and this change's gate
  is the pattern a fix would follow.
