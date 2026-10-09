# Hold the ten unheld vocabularies

## Why

Sixteen string vocabularies are published by the connectivity packages and the
gate holds six. Ten are held by nothing — 47 values.

Measured 2026-10-09, after keying the six existing checks to their sections.
The ten are not alike, and the difference is the whole shape of this change:

| Vocabulary | Values | Where its values are explained today |
|---|---|---|
| `AuthorizationReason` | 5 | `## Authorization`, as its own rows |
| `LinkClass` | 5 | inside the `physical_network` field's own rows |
| `ExpiryClass` | 4 | inside the `session_expiry` field's own rows |
| `ResolverClass` | 4 | inside the `dns` field's own rows |
| `PathClass` | 3 | inside the `default_path` field's own rows |
| `SelectedClass` | 3 | inside the `relay_ingress` field's own rows |
| `ProfileClass` | 2 | inside the `user_access` field's own rows |
| `Lifecycle` | 5 | **nowhere of its own** |
| `AggregateState` | 4 | **nowhere of its own** |
| `Reason` | 12 | **ten of the twelve are not in the document at all** |

Seven need a check and nothing else: their values are already explained in the
place that owns them, about the thing they are.

**Three do not, and two of those would pass a section-keyed check for the wrong
reason.** `Lifecycle` and `AggregateState` are strict subsets of
`ComponentState`, and the rows in `### Component states` are exactly
`ComponentState`'s seven values. A check keyed to that section would find every
lifecycle value and every aggregate value and be satisfied by rows about a third
thing — the derived state rather than what a collector asserted or what the
summary says of the host.

The reference already knows these are different: `### A component row` says that
reading `state` for `observed` is "the most likely mistake here". It explains the
distinction and then tabulates one of the three vocabularies.

And `Reason` — what a collector gives as its account of an assertion — has no
section and ten values that appear nowhere: `baseline`, `boot_rebaseline`,
`expiry_approaching`, `link_changed`, `not_configured`, `owner_unavailable`,
`policy_applied`, `probe_failed`, `probe_succeeded`, `wake_rebaseline`. An
operator reads `probe_failed` in every degraded fact this runtime produces, and
the reference does not say what it means.

## What Changes

- The seven vocabularies already explained where they are owned SHALL gain a
  check keyed to that place.
- `Lifecycle` and `AggregateState` SHALL be explained in their own right, beside
  `ComponentState` rather than inside it, so three vocabularies that share words
  stop being one table. What each word means in each role is the thing a reader
  gets wrong.
- `Reason` SHALL gain a place and an explanation for all twelve values.
- **No vocabulary changes.** No value is added, renamed or removed, and no
  runtime behaviour is touched.

## Capabilities

### Modified Capabilities

- `observable-connectivity-state-machine` — the requirement that what the read
  model publishes is explained where it is owned gains the case this change
  found: a vocabulary whose values are a subset of another's is not explained by
  that other's rows, and a gate that accepts them is keyed on the word again.

## Impact

- `docs/connectivity-read-model-reference.md` — two vocabularies gain their own
  rows, one gains a section, and seven are read where they already are.
- `tests/connectivity_documentation_test.sh` — ten more `vocabulary` calls.
- `tests/reference_documentation.py` — read; it already does this work and needs
  nothing new unless the subset case does.
- Nothing is installed, no runtime restarts, no live value enters Git.

## Not in this change

A vocabulary is still not held to having a producer. `link_class` declares three
values nothing emits, `expiry_class` two, and `managed_transports.degraded` is
never set. This change explains them; holding a value to being reachable is a
different question and is the same shape as the unwired-package list the
repository already keeps.

`### A component row`'s fields are also still unheld.
