# Key every check to its section

## Why

The documentation gate's seven remaining checks cannot notice an explanation
being removed.

Each requires its vocabulary's values to appear as a backtick-quoted word
**anywhere** in the reference, and the values appear in several sections because
the vocabularies overlap. So the row that explains a value can be deleted while
the gate keeps passing on a row about something else.

Demonstrated 2026-10-09, not argued. The row explaining the component state
`degraded` — "the owner asserted partial function" — was deleted from
`### Component states`, and the gate exited **0**. The word survives in the
`managed_transports` payload section, where it counts degraded transports and
says nothing about what a degraded component is.

The same removal would go unnoticed for at least six of the seven
vocabularies. Audited, value by value, against the section each appears in:

| Check | Values | Also appear outside their own section |
|---|---|---|
| component | 8 | all 8, in their payload sections |
| component state | 7 | `conflict`, `degraded`, `ready`, `stale`, `unknown` |
| authorization | 2 | `unauthorized`, in two other sections |
| classification | 8 | `conflict`, `missing`, `stale`, `unknown` |
| diff reason | 12 | `none`, in Authorization and six payload sections |
| proposal class | 4 | none — the only check not exposed |

Nothing is currently undocumented: every value is explained in its own section
today, and the gate passes for the right reason by luck rather than by
construction. The defect is that it cannot tell the difference, which is the
same defect the payload check was fixed for last change — and the fix there is
the pattern.

## What Changes

- Each check SHALL look for its vocabulary only in the section that owns it, so
  a row elsewhere cannot stand in for a missing explanation.
- A check SHALL refuse when the section it is keyed to is absent, rather than
  treating the whole document as the section.
- A check SHALL refuse a value explained in its section that the code no longer
  declares, so the reference cannot describe a vocabulary that has been
  narrowed.
- **No vocabulary changes and no value is added, renamed or removed.** This
  changes what the gate reads, not what the system reports.

## Capabilities

### Modified Capabilities

- `observable-connectivity-state-machine` — the requirement added last change
  says what a component reports is explained under that component. It gains the
  general form: every vocabulary the read model publishes is explained where
  that vocabulary is owned, and a gate holding it is keyed to that place.

## Impact

- `tests/connectivity_documentation_test.sh` — the seven `require` calls become
  section-keyed, and the helper that reads a vocabulary from a typed constant
  block stays as it is.
- `tests/payload_documentation.py` — the pattern already written for payloads;
  the seven checks join it rather than each growing their own parser.
- `docs/connectivity-read-model-reference.md` — read, and changed only if a
  value turns out to be explained outside its own section.
- Nothing is installed, no runtime restarts, no value enters Git.

## Not in this change

**Ten published vocabularies have no check at all**, which the audit for this
change turned up and which is the larger half of the hole. Sixteen string
vocabularies are published by these packages and the gate holds six:
`Reason` (12 values), `Lifecycle` (5), `LinkClass` (5), `AuthorizationReason`
(5), `AggregateState` (4), `ExpiryClass` (4), `ResolverClass` (4), `PathClass`
(3), `SelectedClass` (3) and `ProfileClass` (2) are unheld — 47 values. Six of
them are mentioned in prose in the payload sections written last change, which
is a mention and not an explanation held by anything.

They are left for their own change deliberately: section keying is the pattern
those ten checks will need, and building it here on the four vocabularies whose
sections are already row-aligned gives them a shape to adopt rather than one to
invent.

The vocabularies are also wider than what any mapper produces — `link_class`
declares three values nothing emits, `expiry_class` two — and this change does
not hold a value to having a producer either.
