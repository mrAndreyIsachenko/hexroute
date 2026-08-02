## Context

Hexroute already has component lifecycle machines, root and user observe-only
daemons, typed peer-authenticated IPC, bounded journals and generation-guarded
operator state. Those components do not yet produce one causal view of the
host's connectivity. Route, DNS, tunnel, relay and Pritunl observations can be
read independently, but their ordering, freshness, ownership and relationship
to the active policy generation are not represented by one deterministic model.

That gap is dangerous before active reconciliation: a conventional event loop
can turn each authorization, relay or probe event directly into an imperative
network update, leaving behavior dependent on arrival order and partial state.
Hexroute instead needs a read model that can explain both what is observed and
what would be reconciled without having authority to perform that reconciliation.

The design adapts two reviewed mechanisms: Firezone's policy-driven gateway
[event loop](https://github.com/firezone/firezone/blob/2b4ffb54ec248ca26cb327af2717f7f8801e3b2f/rust/gateway/src/eventloop.rs)
and NetBird's normalized client
[status model](https://github.com/netbirdio/netbird/blob/f2318a8fef230219110c9eeb58ca7f60e247ad98/client/status/status.go).
Hexroute does not copy either product architecture. Direct event-to-mutation is
replaced with deterministic reduction, policy-generation binding and a
non-executable proposal boundary.

Twilight remains the production owner. The atomic-policy change supplies the
signed generation contract used by this design, but this change does not wait
for or enable its future action executor.

## Goals / Non-Goals

**Goals:**

- Represent physical connectivity, default path, DNS, scoped routes, managed
  transports, relays/ingress, Pritunl/user access and session expiry in one
  typed snapshot without hiding their individual states.
- Make every derived state reproducible from ordered source facts, a prior
  checkpoint and an exact active policy generation.
- Distinguish observed state, desired state, typed divergence and a proposed
  reconciliation plan.
- Preserve root/user ownership, credential isolation and cloud telemetry-only
  authority.
- Survive duplicate, missing, reordered and stale observations plus daemon
  restart, sleep/wake and reboot without silently inventing healthy state.
- Qualify the model in shadow mode before any later change can add an executor.

**Non-Goals:**

- Applying routes, DNS, tunnel, process, Pritunl, AdGuard or credential changes.
- Replacing the existing component lifecycle machines or atomic policy compiler.
- Capturing packet payloads, session payloads, credentials or Keychain values.
- Using cloud state, provider events or operator dashboard input as reducer input.
- Cutting production ownership away from Twilight.

## Decisions

### Complete component facts are the input contract

Collectors emit bounded typed facts that describe the complete current state of
one component, not imperative commands or order-dependent patches. Each fact
contains a schema version, event UUID, domain, component and source identity,
source boot ID, monotonically increasing source sequence, wall-clock observation
time, source monotonic tick, lifecycle state, freshness deadline and an
allowlisted reason. Component-specific detail is a strict tagged payload.

Complete facts make convergence possible after an event gap: a later accepted
fact supersedes the prior state for that owned component without replaying every
intermediate patch. The gap remains visible until the source emits an explicit
baseline fact. The alternative, arbitrary delta events, was rejected because a
lost delta can make all following state ambiguous.

Static ownership maps each component to exactly one authoritative root or user
source. Additional probes are corroborating evidence and cannot overwrite the
owner. A second owner, a reused sequence with different content or a fact outside
the sender's domain is a conflict, not a last-writer-wins update.

### The root control plane sequences the aggregate read model

`hexrouted` owns the host-level accepted-event sequence, aggregate checkpoint and
normalized snapshot because it already owns root network/process observations.
`hexroute-userd` persists user facts in its own journal and publishes a bounded
credential-free projection over authenticated typed IPC. Root cannot request a
user action and never receives PIN, TOTP, Keychain references, generated OTPs or
session secrets. If user IPC is absent, user components become stale or unknown;
root does not infer their state.

This avoids adding a third privileged daemon while preserving action ownership:
future root and user executors, if separately approved, must independently
revalidate their domain policy, current observations and action lease. Possessing
the aggregate snapshot grants no credential or mutation authority.

### Reduction is pure, deterministic and generation-bound

The reducer accepts only a validated prior `ConnectivitySnapshot`, an ordered
batch of accepted facts and an exact active policy-generation descriptor. It
returns a new snapshot, desired state, typed diff and zero or more
`ReconciliationProposal` values. It performs no I/O, reads no clock or
environment state and calls no process, route, DNS, VPN or Keychain API.

Facts are ordered by the durable host acceptance sequence. Within a source,
`(source_id, boot_id, source_sequence)` is idempotent; equal identity with a
different canonical digest is a conflict. A semantically unchanged reduction
does not advance the snapshot generation. A changed snapshot advances exactly
once and binds the active bundle plus root/user policy generations used for the
decision.

No compatible active policy means observations remain available while desired
state, diff and proposals are marked unauthorized. A policy change requires a
fresh reduction; output from an older policy generation cannot be resumed.

### Raw component state and aggregate summary coexist

`ConnectivitySnapshot` contains source watermarks, freshness and gap metadata,
all normalized component states, ownership conflicts, active policy generations
and a derived summary. Component states use explicit `unknown`, `ready`,
`degraded`, `failed`, `stale`, `conflict` and `not_applicable` values. The
summary is a deterministic policy-aware projection and never replaces or
removes the component records from local status.

This accepts the operator convenience of NetBird's single overview without
allowing it to flatten DNS, relay or session-expiry failure modes.

### Desired state, diff and proposals are separate types

Desired state is compiled only from the installed safety envelope and active
effective policy. The typed diff classifies each component as `converged`,
`missing`, `unexpected`, `divergent`, `stale`, `unknown`, `conflict` or
`grandfathered_noncompliant` and records bounded reason codes.

A `ReconciliationProposal` binds snapshot generation, bundle and owning-domain
policy generation, target ID, proposal class and canonical diff digest. It is
immutable and digest-addressed but deliberately contains no command, argument,
path, endpoint, credential reference or executable step. Existing IPC has no
operation that can execute it. A future mutation change must translate a fresh
proposal into an exact transactional action plan and obtain a one-time lease;
that authority is not part of this change.

### Journal plus checkpoint is reconstructible and bounded

Root and user facts use the existing crash-safe priority journals. The aggregate
checkpoint is written with generation compare-and-swap, atomic rename and
directory synchronization, and records the last consumed host and source
watermarks. Startup validates the checkpoint and replays accepted facts after
those watermarks. Replaying identical inputs produces canonical identical
output.

Retention always preserves the latest complete baseline for every configured
component before evicting lower-priority diagnostics. Corrupt or unreconstructible
state yields an `unknown`/`conflict` observe-only snapshot and an incident; it
never falls back to a guessed healthy checkpoint or triggers a mutation.

### Boot, wall time and monotonic time have distinct roles

Wall-clock UTC is retained for operator display and portable telemetry.
Freshness within one boot uses source monotonic ticks. A boot-ID change
invalidates prior monotonic deadlines and requires baseline facts. A full wake
marks time-sensitive network, DNS, tunnel and session components stale until
their owners publish new baseline facts. Sleep never counts as proof of health.

### Cloud receives a one-way redacted projection

The cloud projection contains schema and snapshot generation, policy
generations, aggregate and component-class states, freshness buckets, bounded
reason codes, gap/conflict counts and proposal classes/counts. It excludes IP
addresses, hostnames, route prefixes, selectors, source paths, process details,
event UUIDs, session identifiers, endpoints, credential references and values.
It uses existing signed idempotent ingestion and is never read back by the
reducer. Cloud loss only delays upload from bounded local storage.

### Shadow qualification precedes any executor design

The rollout first proves canonical reduction offline with synthetic traces,
then records local shadow snapshots and diffs beside Twilight. Qualification
requires 72 eligible hours, two sleep/wake cycles, one reboot and injected
duplicate, reorder, sequence-gap, collector-loss, checkpoint-corruption and
policy-generation-change cases. Any divergence that cannot be explained from
the retained facts blocks completion.

## Risks / Trade-offs

- **Aggregate state can create false confidence** -> Local status always retains
  component records, freshness, gaps and conflicts beside the summary.
- **A root-owned aggregate increases root code surface** -> Reduction is a pure
  package; IPC accepts strict facts only; no proposal is executable; user
  credentials and actions remain in the user domain.
- **Event gaps can hide transient failures** -> Gaps remain explicit and clear
  only after a complete baseline, even when the latest state can be reduced.
- **Journal bounds can remove forensic detail** -> Latest component baselines and
  critical transitions outrank diagnostics, while overflow is itself observable.
- **Wall-clock jumps can corrupt freshness** -> Same-boot freshness uses
  monotonic ticks; reboot and wake force re-baselining.
- **Policy and observation changes can race** -> Every output binds exact policy
  and snapshot generations; stale proposals cannot be resumed.
- **Cloud schemas may leak topology** -> Projection is allowlisted, secret-canary
  tested and separate from the richer local model.

## Migration Plan

1. Add models, canonical encodings, pure reducer and synthetic replay without
   wiring either daemon.
2. Add root/user collectors and domain journals behind observe-only feature
   flags; compare generated facts with existing component observations.
3. Add authenticated user-to-root fact publication, aggregate checkpointing and
   local status while keeping all proposal execution paths absent.
4. Enable the redacted cloud projection only after local secret-canary and
   canonical replay gates pass.
5. Run the shadow qualification gate beside unchanged Twilight and AdGuard.
6. Roll back by disabling collectors/reducer/status integration and retaining
   existing component observation paths. No network inverse plan is needed
   because this change never mutates production state.

Failed policy activation, missing policy, reducer failure or cloud loss leaves
the existing local recovery path and Twilight production state available. None
of those failures can manufacture a proposal with authority or stop an
established connection.

## Open Questions

None for this observe-only change. Mutation planning, executor placement and
cutover criteria require a separate grill session and OpenSpec change.
