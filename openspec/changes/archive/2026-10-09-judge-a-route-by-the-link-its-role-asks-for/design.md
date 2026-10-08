# Design: judge a route by the link its role asks for

## Context

See proposal.md — Why. Four things were read before this was written.

- `MapScopedRoutes` takes `expectedInterface` and is given the managed tunnel's
  name. Every route not on that interface counts as `Conflicting`.
- `desiredPath` in `internal/routeplan` already decides where a route belongs:
  the tunnel for corporate, `gitlab_https` and inherited; the physical interface
  or the upstream VPN for ingress and never the tunnel; and for
  `codex_fallback`, no route at all while normal Codex is reachable.
- The planner already distinguishes the states this needs:
  `ReasonMissingRoute`, `ReasonWrongPath`, `ReasonFallbackRequired` and
  `ReasonFallbackRestored`, across `OperationEnsureHostRoute` and
  `OperationRemoveOwnedHostRoute`.
- `plannerIntents` already carries the plan into the read model, but only as two
  booleans for the whole component, so nothing per-route survives the trip.

## Goals / Non-Goals

Goals: the fact judges each route against the link its role asks for; a route
nobody asked for is not counted; absent and misplaced are separate quantities;
`ready` is reachable; the rule has one statement.

Non-Goals: moving a route; changing `desiredPath`; changing what the planner
proposes; changing the other mappers, which compare nothing against a single
interface; explaining `open_gaps`.

## Decisions

### The rule is exported, and the read model applies it to the same evidence

This was decided twice. The first decision was to derive the fact from the
plan's operations by reason, so the collector would count the planner's output
and never hold the rule.

`Evidence` forbids it, in its own words: *"Nothing here is derived. The daemon's
own conclusions stay with the daemon; this is what it saw before drawing them,
so the read model reaches its own conclusions from the same evidence rather than
from a second look at the host."* A plan verdict is the daemon's conclusion, and
carrying it to the read model is the one thing that type exists to prevent.

The invariant states the same worry inverted, and resolves it better: the read
model should reach the judgement itself, from the same readings. Then the rule
has one statement not because its output is passed around, but because both
callers **call the same function**. So `desiredPath` is exported, the planner and
the collector both use it, and `Evidence` gains the configuration the judgement
needs — the targets with their roles and preferred links — which is
configuration and not a conclusion. `ConfiguredRoutes` is already there on
exactly that footing.

Alternatives rejected: a second statement of the role-to-link rule beside the
collector, which is the defect this repository has paid for twice, once in the
configuration it did not record and once in the health a label did not match;
and passing the plan or a count of it, which the type forbids for a reason that
survives this change.

### `configured` keeps its meaning and `ready` stops depending on it

`configured` stays the number of routes the configuration declares — 21 — so a
reader comparing the quantity across time is not handed a silent redefinition.

What changes is the ready condition. It is `conflicting == 0 && missing == 0`,
not `installed == configured`. The routes nobody asked for are then neither
counted nor required, and the remainder is `configured - installed -
conflicting - missing` for a reader who wants it, without a field of its own.

### A fourth quantity, because one number answered two questions

`missing` is the routes the configuration asks for and the host lacks;
`conflicting` becomes only the routes the host has on the wrong link. Today both
are summed under `conflicting`, which is the same fault as the health that
counted workload: a quantity that cannot distinguish two states with two
different remedies.

### A route pending withdrawal counts as conflicting

`OperationRemoveOwnedHostRoute` with `ReasonFallbackRestored` is a route the
configuration no longer asks for and the host still has. That is present and
unwanted, which is a divergence on the same axis as present-and-misplaced, and
is answered by the same kind of act, so it is counted as conflicting rather than
given a fifth name.

It does not occur on this machine today: the live plan holds two operations and
both are `ensure_host_route` with `wrong_path`, and withdrawal needs a route
this runtime owns, which in `observe-only` it does not. So this decision is
reasoned rather than measured, and the task says so.

## Risks / Trade-offs

- **The component will read `ready` while two ingress routes are still on the
  wrong links.** → It will not: those two produce `wrong_path` operations, so
  `conflicting` becomes 2 and the component stays degraded — correctly, and for
  two routes rather than fourteen. That is the first time this fact will have
  reported a number that means what it says.
- **A reader's dashboard compares `conflicting` across the change.** → The
  meaning narrows from 14 to 2 with no code on the reader's side changing. The
  documents must say when it changed and what it meant before.
- **The fact now depends on the plan having been computed.** → A cycle that
  fails before planning has no plan, and the fact must then be `unknown` rather
  than a count of zero, which would read as "nothing diverges".

## Migration Plan

Nothing is installed by this change and no runtime restarts; the next install
carries it. No persisted schema changes: the payload gains a field, and the read
model's checkpoints are rebuilt rather than migrated.

That rebuild has a surface this plan did not name, and it is named here because
it looks like corruption and is not. `Checkpoint.Validate` recomputes
`Snapshot.Digest()` and compares it with the stored `SnapshotDigest`. The digest
attests to the serialization, so a new payload field changes it for **every**
stored record: the decoded snapshot re-serializes with the new key and no longer
matches. `Store.Load` fails, and the resume reports
`recovered_ancestor` / `record_invalid` — not `digest_mismatch`, which is
reserved for the index disagreeing with the record.

Measured on the install: 5650 stored checkpoints, 5648 of the old shape, and the
watcher moved from `resume: latest, reason: none` to `recovered_ancestor,
record_invalid`. The read model walked back, proved nothing within its depth,
recorded a `lineage_break` and began a new lineage. Only the derived checkpoints
are affected; the spool and the archive are the evidence and are untouched.

`omitempty` would avoid it for a zero value and is not used: it would make
absent and zero the same claim — the thing the previous change was spent
separating — and would only postpone the break to the first non-zero.

Rollback is **not** clean, and the design said this had to be verified rather
than assumed. Verified 2026-10-09: it does not hold. `connectivity.Decode`
refuses an unknown field by design — two encodings of one fact would carry two
digests, and the digest is the identity the aggregate deduplicates on — so the
previous release's decoder, given a fact written by this one, answers
`connectivity fact encoding is invalid: json: unknown field "missing"`. On the
archive's read path such a record becomes `ErrInvalidField`.

So after this change is installed, records carrying the fourth quantity are
unreadable to the previous binary. They are not lost: they stay on disk and are
readable again on rolling forward, and the cross-domain publication path is
untouched because `scoped_routes` is root-owned and the user daemon never emits
it.

The alternative was a fact schema version — `hexroute.connectivity-fact.v2` —
so the refusal would name a version rather than a field. It is not taken, for
two reasons. It would touch every reader keyed on the schema, for an
incompatibility that exists either way. And it would make the refusal *less*
informative: `unknown field "missing"` says what and why, where `unknown
schema` says only that the two disagree.

## Open Questions

- Whether `connectivity-watch.json`'s aggregate needs anything beyond this. It
  reports `degraded` on this component alone, so it should follow; if it does
  not, that is its own reading and not a reason to change this fact.
- Whether exporting the rule should carry the counting with it —
  `routeplan.Placement(targets, input, observed)` returning the three
  quantities — or only the per-target decision, leaving the counting to the
  collector. The first keeps the arithmetic beside the rule; the second keeps
  `routeplan` free of a shape only the read model wants. Answered when the
  function is written, and it changes no requirement either way.
