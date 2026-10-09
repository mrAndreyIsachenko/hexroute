# Explain every quantity a component reports

## Why

The connectivity reference explains one of the eight component payloads. The
documentation gate does not notice, and cannot be made to notice by checking
harder in the same way.

Measured 2026-10-09. Eight payload types, 25 fields between them, and exactly
one type — `ScopedRoutesPayload`, documented last week by the change that added
a field to it — has every field in a table row:

| Payload | Fields | Explained |
|---|---|---|
| `PhysicalNetworkPayload` | 3 | none |
| `DefaultPathPayload` | 2 | none |
| `DNSPayload` | 4 | none |
| `ScopedRoutesPayload` | 4 | all |
| `TransportsPayload` | 3 | none |
| `RelaysPayload` | 4 | one |
| `UserAccessPayload` | 3 | none |
| `SessionExpiryPayload` | 2 | none |

`tests/connectivity_documentation_test.sh` holds the component names, the
component states, the authorization values, the diff classifications and
reasons, the proposal classes, the declared sources and the installed
arguments. It holds no payload field, so twenty quantities a reader meets in a
status answer are explained nowhere.

**The obvious extension would be a gate that lies.** The check is
`grep -qF` for a backtick-quoted word anywhere in the document, and these
vocabularies collide on the same words:

- `configured` appears in three payload types — routes, transports, relays —
  and means a different subject in each. One row cannot say all three.
- `ready` and `degraded` are both transport counts and component states.
- `missing` is both a scoped-routes quantity and a diff reason.

Measured, not supposed: a flat check reported `TransportsPayload` fully
explained because `configured` matched the scoped-routes row and `ready` and
`degraded` matched the component-state rows. It reported the same of the field
`missing` before that row existed. Three false passes, each found only by
reading the document for the specific word.

A gate that passes for the wrong reason is worse than the missing gate it
replaces: it converts an absent guarantee into a false one.

## What Changes

- Every component payload's fields SHALL be explained **under that component**,
  so a word that appears in several payloads is explained once per payload with
  the subject it counts there.
- The gate SHALL check per payload type rather than per word, so a row
  belonging to another vocabulary cannot satisfy it.
- The seven undocumented payloads gain their sections — twenty quantities.
- **No new vocabulary.** This explains what the code already reports; no field
  is added, renamed or removed, and no runtime behaviour changes.

## Capabilities

### Modified Capabilities

- `observable-connectivity-state-machine` — it owns the normalized snapshot and
  its separately inspectable component state. The requirement that a component
  is inspectable gains the condition that what it reports is explained where a
  reader can tell which component it belongs to.

## Impact

- `docs/connectivity-read-model-reference.md` — seven sections added, one per
  payload, in the shape the scoped-routes section already has.
- `tests/connectivity_documentation_test.sh` — the payload check, keyed on the
  payload type and its component rather than on a bare word.
- `internal/connectivity/payload.go` — read by the gate, not changed by it.
- Nothing is installed, no runtime restarts, and no live value enters Git.

## Not in this change

The same flat-match weakness may affect the gate's other checks — a component
name or a diff reason could also be satisfied by a row from another vocabulary.
This change fixes the payload check and does not audit the rest, which would be
a reading of its own.
