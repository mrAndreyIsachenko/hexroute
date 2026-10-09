# Reading the connectivity read model

[`connectivity-read-model.md`](connectivity-read-model.md) says why the read
model is safe to run. This says what it actually reports, field by field, and
how to read an answer without guessing.

Everything here is a statement about the code, not about intent. Where a value
is absent the reason is given, because in this model an absent answer and a
healthy answer must never look alike.

## Who owns what

Exactly one source is authoritative for each component, and the split follows
the privilege boundary: root owns what only root can see, the user domain owns
access and session state so root never needs a credential to describe them.

| Component | Authoritative source | Domain | Corroborated by |
| --- | --- | --- | --- |
| `physical_network` | `root.network` | root | — |
| `default_path` | `root.network` | root | `root.probe` |
| `dns` | `root.dns` | root | `root.probe` |
| `scoped_routes` | `root.routes` | root | — |
| `managed_transports` | `root.transports` | root | — |
| `relay_ingress` | `root.relays` | root | `root.probe` |
| `user_access` | `user.access` | user | `user.probe` |
| `session_expiry` | `user.session` | user | — |

A corroborating source cannot update a component. It runs on its own schedule
and may disagree, and that disagreement is recorded as evidence rather than
folded in as an update — an owner and a probe that differ is a fact about the
host, not a question of which to believe.

Two of these owners do not exist yet. `root.dns` has no collector, so `dns` is
permanently `unknown`; nothing observes session lifetime, so `session_expiry`
never claims `expiring` or `expired`. Both are described in the safety
envelope so the gap is visible in the snapshot rather than being an absent row.

## The snapshot

One reduction produces one snapshot. Its fields divide into four groups.

**Identity and provenance.** `schema`, `version`, `generation`, `reducer_id`,
`reducer_version`. The generation moves by one per reduction and is what the
checkpoint lineage is ordered by.

**The time context the reduction ran in.** `boot_id` and `evaluation_tick`.
They are supplied to the reducer, never read by it — that is what makes a
replay able to prove it reached the same conclusion from the same inputs
rather than from the same wall clock.

**Where the fold has reached.** `consumed_host_sequence` and
`consumed_fold_position` are two different distances, and the difference is
load-bearing. The accepted order skips every duplicate, conflict and late
arrival; the folded order counts them. A replay that read only the accepted
order would arrive somewhere the snapshot never was.

**What was concluded.** `policy`, `authorization`, `authorization_reason`,
`components`, `sources`, `conflicts` and `summary`.

### A component row

| Field | What it says |
| --- | --- |
| `state` | the derived state — the row's answer |
| `observed` | what the owner last asserted, kept even when the derived state is `stale`, so what went quiet is still visible |
| `reason`, `payload` | the owner's own account of that assertion |
| `boot_id`, `monotonic_tick` | when it was asserted, in the only clock that survives comparison |
| `freshness_deadline` | the tick past which the assertion stops counting |
| `host_sequence` | where that assertion sits in the accepted order |
| `has_baseline` | whether the owner has ever restated this component in full |
| `rebaseline_required` | set by a wake or reboot, cleared only by a full restatement |
| `conflicts` | how many arrivals contradicted the owner |
| `corroborations` | what the probes said |

`state` and `observed` answer different questions, and reading one for the
other is the most likely mistake here. `observed: ready` with `state: stale`
does not mean the component is ready; it means it was, and nothing has said so
recently enough to still count.

### The `scoped_routes` payload

| Field | What it says |
| --- | --- |
| `configured` | every route the configuration declares, whether or not it asks for one under the present conditions |
| `installed` | asked for, and on the link its role asks for |
| `conflicting` | asked for and somewhere else, or present and asked for nowhere |
| `missing` | asked for and not there |

The three judged quantities count only the routes the configuration asks for,
so `configured` minus the three is the number it asks for nowhere — twelve of
twenty-one on the host this was written from, because it asks for no fallback
route while normal Codex is reachable.

`ready` is `conflicting == 0 && missing == 0`. It does not require every
declared route to be installed, because a configuration that asks for nothing
of a role could never satisfy that.

**This changed on 2026-10-09, and a reader comparing across that date is
comparing two different measurements.** Before it, `conflicting` meant "not on
the managed tunnel" and there was no `missing`. The configuration assigns routes
to three links by role and two of its roles must never be on the tunnel, so
every route correctly on another link was counted as a conflict: read
2026-10-08, `conflicting` was 14 of 21 and all 14 were exactly where their role
asks them to be. `ready` required all 21 on one interface and could not be
reached, so the component had reported `degraded` in every cycle it ever ran.

A residue of 2 is expected and is not this runtime's to clear: the host's two
ingress routes stand on each other's links, which is the arrangement the
runtime that owns the tunnel enforces, read on 2026-09-25 and unchanged since.

### The `physical_network` payload

| Field | What it says |
| --- | --- |
| `link_class` | `wired` whenever the interface reports up, `none` otherwise. The observer cannot tell wired from wireless, so `wired` here means *up* and not *not wireless* — the vocabulary also holds `wireless`, `cellular` and `virtual`, and nothing emits them |
| `link_up` | the interface reported up |
| `has_carrier` | **a default gateway was observed on the interface.** Not a link-layer carrier, despite the name: it is set from the gateway being valid |

A link that is up without a gateway reads `degraded`, which is the only reason
`has_carrier` is separate from `link_up`.

### The `default_path` payload

| Field | What it says |
| --- | --- |
| `path_class` | `tunneled` when the default route leaves through the managed tunnel, `direct` when it leaves without one, `none` when neither could be established |
| `gateway_present` | under `tunneled`, that a gateway was observed; under `direct`, nothing — it is set to true with the class |

The two classes do not make the same claim with this field, which is why the
class is read first.

### The `dns` payload

| Field | What it says |
| --- | --- |
| `resolver_class` | which kind of resolver answers: `system`, `scoped`, `encrypted`, or `none` |
| `responding` | the resolver answered |
| `scoped_domains` | how many domains are configured to resolve through the scoped resolver |
| `failing_domains` | how many of those did not resolve |

**Nothing produces this component.** No mapper can emit a DNS observation, and
a test asserts that none does, so the fields above describe what the payload is
shaped to carry rather than anything a reader will meet today. The component is
in the snapshot's vocabulary so the shape is fixed before a collector exists.

### The `managed_transports` payload

| Field | What it says |
| --- | --- |
| `configured` | how many managed transports the caller declared. The root cycle passes **one**, literally, so this is not a count of anything observed |
| `ready` | one when the tunnel process is running, zero when it is not |
| `degraded` | **never set.** No mapper writes it; it is zero in every fact this runtime has produced |

`configured`, `ready` and `degraded` are also words in other vocabularies here —
`configured` counts routes in the `scoped_routes` payload and relays in
`relay_ingress`, and `ready` and `degraded` are component states. They are
different subjects with the same names.

### The `relay_ingress` payload

| Field | What it says |
| --- | --- |
| `configured` | how many ingress endpoints were probed this cycle |
| `reachable` | how many of them answered ready |
| `reserve` | how many are held in reserve. The root cycle passes **zero**, so this reports nothing it measured |
| `selected_class` | which class is in use: `primary`, `reserve`, or `none` when nothing is configured. The root cycle passes `primary`, and a `reserve` selection with no reserve is corrected to `primary` |

Two of the four are the call site's constants rather than observations. They
exist for a fleet that selects a reserve, and nothing selects one yet.

### The `user_access` payload

| Field | What it says |
| --- | --- |
| `profile_class` | `configured` when a profile was found, `none` when it was not |
| `authenticated` | a session was accepted. It is **inferred**, not checked: carrying traffic implies it, and so does a profile that is connecting or active |
| `connected` | the profile is carrying traffic |

### The `session_expiry` payload

| Field | What it says |
| --- | --- |
| `expiry_class` | `valid` while a session is active, `none` otherwise. The vocabulary also holds `expiring` and `expired`, and no mapper emits either |
| `sessions` | one while a session is active, zero otherwise. It is a count that has never been anything else |

### The reasons a collector gives

The `reason` on a fact, named in the component row above. It is the collector's
own account of why it asserted what it did, from a closed vocabulary of twelve.

Each row says what emits it, read from the mapper rather than from the name —
which matters, because half of this vocabulary is emitted by nothing.

| Reason | What emits it |
| --- | --- |
| `probe_succeeded` | a collector that observed its component working |
| `probe_failed` | a probe that ran and failed, or an observation that returned an error. It is the reason on every `unknown` lifecycle, so it covers both "it answered badly" and "it could not be asked" |
| `link_changed` | the physical and default-path collectors: an interface that is absent, one that is down, one that is up with no gateway, and a default route that leaves without a tunnel |
| `not_configured` | nothing is configured for the collector to observe — no routes, no profile, no session |
| `owner_unavailable` | the user-access collector, when the service beneath the session is not running |
| `none` | the reducer, clearing the reason on a record it is no longer attributing |
| `baseline` | **nothing in the running system.** Only `internal/connectivity/fixture.go` emits it, which is synthetic |
| `policy_applied` | nothing |
| `wake_rebaseline` | nothing |
| `boot_rebaseline` | nothing |
| `expiry_approaching` | nothing |
| `expired` | nothing |

**Five of the twelve are emitted by nothing, and a sixth only by a fixture.**
The vocabulary was fixed before the collectors that would use it: a wake or a
boot does set `rebaseline_required` on a component row, but no fact carries
`wake_rebaseline` or `boot_rebaseline` as its reason, and
`session_expiry` reports `expiry_class` rather than an `expiry_approaching`
reason. Reading one of those five in a fact would mean something new started
emitting it.

Nothing holds that true. A value here could become reachable, or stay
unreachable after the collector that was meant to use it arrives, and no gate
would notice.

### Component states

Three vocabularies share these words and mean different things: what a
collector asserted about its own component, what the read model derived from
that, and what the summary says of the whole host. The word a reader is most
likely to get wrong is one of the shared ones — `degraded` is three claims, and
which one is in play depends on which of the three you are reading.

#### What a collector asserted

The lifecycle on a fact. It is the owner's own account of its component, before
the model has done anything with it, and `observed` in a component row is where
a reader meets it.

| Lifecycle | What the collector is saying |
| --- | --- |
| `ready` | it observed its component working |
| `degraded` | it observed partial function — a link up with no gateway, routes some of which are misplaced, relays some of which answer |
| `failed` | it observed the component not working at all |
| `unknown` | it could not observe. The probe did not run, or the observation returned an error, and the payload carries no count it can stand behind |
| `not_applicable` | nothing is configured for it to observe |

A collector never asserts `stale` or `conflict`. Those two belong to the model,
which is why they are in the next table and not this one.

#### What the model derived

The state on a component row. It is the lifecycle judged against freshness,
baselines and ownership, so it can say things no collector can.

| State | What it means |
| --- | --- |
| `unknown` | nothing has been established — including a component whose owner does not exist |
| `ready` | the owner asserted health, inside its freshness deadline, on a baselined stream |
| `degraded` | the owner asserted partial function |
| `failed` | the owner asserted failure |
| `stale` | something was established and the evaluation tick is past its freshness deadline |
| `conflict` | arrivals contradict each other and the model does not pick a winner |
| `not_applicable` | policy does not manage this component on this host |

`unknown`, `stale` and `conflict` are the three ways of saying *no current
answer*, and they are kept apart because they call for different actions:
nothing was ever said, what was said has expired, and two things were said.

#### What the summary says of the host

The aggregate. It is deliberately pessimistic — it cannot report better than its
worst component — and it is **also degraded by the integrity of the streams
themselves**, which is the part a reader is most likely to miss.

| Aggregate | When it is reported |
| --- | --- |
| `failed` | any component failed |
| `degraded` | any component is degraded, stale or conflicted, **or** the evidence has a hole in it: an open gap, an evicted gap, a source still owing a baseline, or a source with conflicts |
| `unknown` | nothing is worse than unknown, and at least one component is unknown |
| `ready` | every component is ready and no stream has a hole in it |

The integrity clause is why the host can read `degraded` with every component
`ready`. Every surviving component can be fresh and ready precisely because the
facts that would have said otherwise are the ones that went missing, so a hole
degrades the summary on its own rather than only through the components that
happen to have been observed. Measured 2026-10-08: the watcher reported
`aggregate: degraded` with `open_gaps: 2`, and a note written at the time
attributed it to one degraded component alone — the integrity clause was
degrading it too.

### The summary

Counts per state, plus the integrity numbers: `open_gaps`, `gap_overflow`,
`source_conflicts`, `conflict_overflow` and `awaiting_baseline`. The two
overflow flags mean retained evidence was evicted to stay inside a bound —
reported rather than left to be inferred from a number that stopped moving.

`awaiting_baseline` is the one to read first after a wake, a reboot or a
restart. It counts sources that still owe a full restatement, and while it is
above zero the ready rows rest on a stream with a hole in it.

## What the reducer guarantees

- **It is pure.** Everything it may see is in one input struct — prior
  snapshot, events, policy, boot id, evaluation tick. It reads no clock, no
  file and no network, which is what makes a replay a proof rather than a
  second opinion.
- **It does not judge stream integrity.** Gaps, overflow, baseline debt and
  conflict counts are decided once, in the acceptor, and adopted. There is no
  second opinion available to diverge from the first.
- **It fails closed.** Absent, invalid, suspended or generation-mismatched
  policy yields `unauthorized` with the reason named, and no proposal asks for
  a change under it.
- **Staleness is arithmetic, not judgement.** A record is stale exactly when
  the evaluation tick is past its freshness deadline.
- **Sleep is not evidence of health.** A wake or reboot sets
  `rebaseline_required`, and only a full restatement clears it. A fresh
  non-baseline fact is not enough: it describes now without accounting for
  the gap.

## Authorization

`authorization` is `authorized` or `unauthorized`, and the reason says which
contract decided it.

| Reason | What is wrong |
| --- | --- |
| `none` | nothing — this accompanies `authorized` |
| `policy_absent` | no active policy to read |
| `policy_invalid` | the active policy did not validate |
| `policy_suspended` | authorization is suspended |
| `policy_generation_mismatch` | the policy generation is not the one this reduction was bound to |

`unauthorized` does not stop observation. Facts keep arriving, the snapshot
keeps being produced and the state of every component is still reported — what
stops is any proposal asking for a change. The read model's job is to describe
the host, and being unable to authorize a change is not a reason to stop
knowing what the host looks like.

## The diff

The diff compares the snapshot against what policy asks for. It is per
component, and it carries a classification and the reason for it.

| Classification | What it means |
| --- | --- |
| `converged` | what policy asks for is what is observed |
| `missing` | policy asks for it and it is not there |
| `unexpected` | it is there and policy does not ask for it |
| `divergent` | it is there and differs from what policy asks |
| `stale` | the observation is too old to compare against |
| `unknown` | there is nothing to compare |
| `conflict` | the observations contradict each other |
| `grandfathered_noncompliant` | it predates the policy that would not ask for it |

`grandfathered_noncompliant` exists so that a policy change cannot turn
something already running into something to withdraw. The reasons — `none` for
a converged row, and `not_observed`, `stale_observation`, `owner_conflict`, `nothing_present`,
`below_expected_count`, `class_mismatch`, `observed_failed`,
`observed_degraded`, `not_managed_by_policy`, `excluded_by_new_policy`,
`policy_unauthorized` — say which of several paths produced the
classification, because `missing` for four different causes is four different
situations.

## The proposals

A proposal is the model's account of what would close a diff. **Nothing in
this change can execute one**, and that is enforced by the import boundary
rather than by there being no caller: the read model cannot reach `os/exec`,
`net`, or any command, plan, lease or credential package.

| Class | Covers |
| --- | --- |
| `establish` | policy wants something that is not there |
| `reconcile` | something exists and differs |
| `withdraw` | something is present that policy does not ask for and that no earlier policy established |
| `observe` | the answer is uncertainty, so what is proposed is a fresh observation |

`observe` is why the other three stay honest. Uncertainty has somewhere to go
that is not a network change, so `unknown`, `stale` and `conflict` never have
to be resolved into an action in order to be reported.

## Reading a status answer

```
hexroute-connectivity-replay --state --store <root>
```

Read it in this order:

1. **Did it answer at all?** No summary means nothing could be proven, and the
   resume verdict and its reason are printed instead. This is not a healthy
   host with nothing wrong; it is no answer.
2. **`authorization`.** `unauthorized` makes every proposal moot, and the
   reason names which policy contract is missing.
3. **`awaiting_baseline` and the overflow flags.** Above zero, or set, and the
   rows below rest on an incomplete stream.
4. **The component rows.** `state` first, then `observed` to see what the row
   used to say.

An empty answer and a clean answer are deliberately different shapes. A zeroed
summary would print as no component failing and nothing stale, which reads
exactly like a healthy host.
