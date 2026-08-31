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

### Component states

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
