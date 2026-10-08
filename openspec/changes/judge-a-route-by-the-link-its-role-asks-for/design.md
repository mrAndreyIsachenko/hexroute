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
model's checkpoints are rebuilt from the spool rather than migrated.

Rollback is the previous binary, which reads the same facts and ignores a field
it does not know — to be verified, not assumed, since the payload has a
validator.

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
