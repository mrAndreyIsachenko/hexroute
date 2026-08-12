## Context

The atomic-policy change defines signed policy generations, one-time action
leases, exact transactional plans and a state-only `operator_resume` executor.
The observable-connectivity change defines complete source-owned facts, a
normalized snapshot, deterministic desired-state diffs and immutable
non-executable reconciliation proposals. Neither change defines a general
network reconciler, and Twilight remains the production owner.

The missing boundary is the conversion of one fresh proposal into one
capability-scoped execution attempt that remains explainable across retry,
crash, cancellation and reporting. A direct event-to-command loop would bypass
the policy generation, flatten transient observations into actions and invite
blind restarts. A generic remote command runner would also violate Hexroute's
typed capability and privilege boundaries.

This design adapts mechanisms reviewed in `architecture-radar`: durable command
state and independent reporting from `wenet-ec/edge-core`, typed command
acknowledgements from PX4, diff-based interface rehydration from
`DefGuard/gateway`, cancel-safe cleanup from QGroundControl, range-gap recovery
from the MAVLink companion log service, provisional/finalized separation from
reorg-safe materializers, and provenance envelopes from LangGraph/OpenLineage.
They are design inputs only; Hexroute copies no dependency or product control
plane.

## Goals / Non-Goals

**Goals:**

- Define a deterministic, generation-bound proposal-to-plan boundary.
- Keep action readiness separate from raw or provisional connectivity state.
- Persist action claim, execution, verification, cancellation, compensation and
  reporting as explicit typed state.
- Rehydrate missing managed state only from a fresh authorized desired-state
  diff, never from an opaque restart decision.
- Preserve root network/process and user credential/action ownership.
- Make every action outcome replayable from bounded typed provenance.
- Repair bounded telemetry sequence gaps without making cloud state an input to
  local policy, reduction or execution.
- Prove the engine with synthetic adapters before any production capability is
  proposed.

**Non-Goals:**

- Adding a production route, DNS, firewall, process, tunnel, Pritunl, Keychain
  or OTP mutation adapter.
- Cutting ownership away from Twilight or changing Twilight or AdGuard.
- Automatically disconnecting grandfathered sessions or selecting failover.
- Running arbitrary commands, accepting arbitrary paths or adding a remote
  shell surface.
- Allowing cloud, provider events or dashboard input to request local actions.
- Treating a telemetry report as authorization or as proof that an action ran.
- Replacing atomic policy, observable connectivity or existing lifecycle state
  machines.

## Decisions

### Prerequisites are structural, not advisory

Daemon integration remains disabled until both `add-atomic-policy-generations`
and `add-observable-connectivity-state-machine` have complete replay-derived
qualification evidence and their validated requirements are synchronized. The
engine packages and synthetic tests may be built earlier, but no runtime feature
flag may expose proposal translation or execution while either prerequisite is
incomplete, invalid or generation-mismatched.

The first rollout contains only an in-memory and a crash-fixture synthetic
adapter. Production capability identifiers are absent from the compiled static
allowlist. This was chosen over a disabled real adapter because dead code can
still create an undeclared import, secret or fallback path.

### Privilege and ownership remain capability-local

The root daemon owns host snapshot sequencing and may coordinate only root-owned
synthetic capabilities. The user daemon owns user-domain action state and is the
only process that may ever use future credential-backed adapters. A proposal for
one domain cannot mint a lease in the other domain. Cross-domain work requires
matching bundle generations and separate domain leases and receipts; it is not
implemented in this change.

Collectors own failure detection. The pure reducer owns snapshot, desired-state
and diff derivation. The reconciler owns readiness classification and exact plan
translation. A capability adapter owns only its typed observe/apply/verify and
compensate operations. The local action journal owns durable lifecycle state.
The cloud owns only redacted storage, correlation and acknowledgement of
telemetry uploads.

### Raw state and action readiness are different projections

Every proposal is re-evaluated against the current canonical snapshot, exact
bundle/root/user policy generations, current boot, source watermarks and the
policy's threshold, budget, backoff and cooldown state. Raw component state
remains immediately visible, but readiness is one of `ready`,
`temporarily_blocked` or `denied`.

`ready` requires fresh authoritative facts, no relevant source gap or conflict,
the configured consecutive/duration threshold, available action budget and a
compatible non-suspended policy. `temporarily_blocked` preserves a bounded
retry-after hint for transient freshness, threshold, backoff, cooldown or budget
conditions. `denied` covers policy, capability, ownership, schema, generation or
target violations. A single failed probe therefore cannot become a network
action merely because its latest raw state is failed.

This adapts the provisional/finalized distinction without hiding failures: the
operator sees raw component state and readiness side by side. There is no
probabilistic confidence score and no caller-supplied readiness flag.

### Proposal translation is pure and exact

The translator accepts only a fresh immutable proposal, its referenced snapshot
and diff, the exact active effective policy, a static capability descriptor and
canonical adapter metadata. Equal inputs produce the same typed action plan and
digest. Translation performs no I/O and cannot read environment state,
credentials or the clock.

The plan binds proposal, target, snapshot, bundle/domain/control generations,
capability and adapter version, readiness evidence digest, ordered step digests,
verification expectations and compensation digests. It contains no arbitrary
command, argument, path, endpoint or credential value. A semantic no-op returns
an accepted no-action result and cannot mint a lease.

### A durable attempt machine wraps the existing lease contract

The atomic-policy one-time lease remains the authorization primitive. The
reconciler adds a separate append-only attempt state machine:

`pending -> claimed -> running -> verifying -> committed`

Terminal alternatives are `expired`, `denied`, `cancelled`, `rolled_back`,
`failed` and `safe_mode`. Transitions use generation compare-and-swap and append
an immutable record before the next side effect. The execution attempt ID,
action ID, nonce, boot ID and plan digest never change.

A process may claim only a pending valid lease. After a durable claim, another
process cannot repeat or resume the action. Startup observes current target
state and sends uncertain claimed work to explicit recovery; it does not copy
edge-core's automatic rerun of a `running` command. This preserves the stronger
atomic-policy fail-closed crash contract.

Reporting has an orthogonal delivery state (`pending`, `acknowledged`,
`terminally_rejected`) and never changes the execution outcome. Cloud loss or a
reporting retry cannot block, repeat, commit or roll back a local action.

### Acknowledgements classify acceptance and retryability

Every proposal submission returns one bounded acknowledgement:

- `accepted`: a canonical no-op was proven or a pending action/lease was durably
  created for the exact request.
- `temporarily_rejected`: current policy permits the capability, but freshness,
  stability, budget, backoff, cooldown or a recoverable local dependency blocks
  it now. The response may include a bounded monotonic retry-after hint.
- `denied`: schema, ownership, policy, generation, target, capability or
  provenance validation failed. The same request identity cannot be replayed as
  an accepted action.

The acknowledgement contains a stable allowlisted reason and no raw adapter,
process, network or credential error. Retryability is a typed contract, not
inferred from strings or HTTP status.

### Rehydration is desired-state reconciliation, not restart

An adapter first produces a fresh typed current-state observation. The planner
compares it with exact desired state and emits independent capability steps.
Tunnel/interface lifecycle, scoped routes, DNS, firewall, process and user
access remain distinct operation classes and ownership domains. A missing
interface is reconstructed only when fresh desired state authorizes it; an
unexpected or foreign interface is never silently adopted or purged.

Each step verifies the before state, applies one typed operation and observes the
exact applied-state digest before advancing. Unchanged state is a no-op. A later
production adapter must define its own protected namespaces, semantic equality,
ownership proof, verification and inverse operations in a separately approved
change. There is no universal `restart` action.

### Cancellation is a durable intent with bounded cleanup

Cancellation appends an intent and prevents the next not-yet-started step. If no
step was applied, the attempt becomes `cancelled`. If owned state was applied,
the runner uses a cancellation-independent bounded compensation context and the
atomic-policy verified reverse-prefix rules. Foreign, ambiguous or changed
state is not touched; uncertainty moves only the target to `safe_mode`.

Temporary helpers, private files and capability-local credential leases are
registered as typed resources under the attempt and closed on success, cancel,
generation change or failure. Cleanup failure is an explicit outcome and
incident. Cancellation never deletes immutable action or forensic records.

### Provenance uses a minimal shared header and strict payloads

Proposal, readiness, lease, attempt, step, compensation, outcome and incident
records share a minimal `ActionProvenance` header containing schema/record ID,
record kind, parent and root action IDs, producer/domain, boot ID, exact policy,
control and snapshot generations, source/input/output digests, UTC observation
time and source monotonic time where meaningful. Every record has its own strict
bounded payload and canonical digest.

This allows end-to-end causality without a generic evidence map. A readiness
record cannot be decoded as an execution result, and an uploaded report cannot
be substituted for a local outcome. Raw source evidence, topology, endpoints,
paths, process output and credential references remain local or are excluded
entirely.

### Telemetry gap repair is bounded and upload-only

Signed ingestion acknowledgements bind node, request ID and durable accepted
high-watermark and may return a bounded sorted set of missing node-sequence
ranges. The local uploader may replay only exact retained immutable records for
those ranges. It never synthesizes missing records, rewrites sequence numbers or
uses the acknowledgement as reducer/action input.

Range count, width, response size, retry rate and local scan work are bounded.
If retention no longer contains a requested range, the uploader emits one
redacted `telemetry_gap_unrecoverable` record after the gap and leaves the
server-side gap visible. Uploading newer events remains possible. This was
chosen over server-initiated callbacks so the cloud remains telemetry-only.

### Qualification proves the engine, not production mutation

Synthetic adapters cover no-op, accepted, temporary reject, deny, expiry,
cancel-before-apply, cancel-after-apply, compensation, crash-after-claim,
crash-after-apply, verification mismatch, generation change, missing-interface
rehydration, foreign-state conflict, telemetry gap fill and unrecoverable gap.
Replay must derive identical plans and terminal outcomes from the immutable
records.

Capability-leak checks reject production route, DNS, process, tunnel, Pritunl,
Keychain, credential and network imports from this change's executors. A later
production adapter must add its own safety envelope, fault matrix, live shadow
qualification, guarded cutover and independently executable rollback.

## Risks / Trade-offs

- **[A generic engine accidentally becomes a command runner]** -> Plans and
  adapters are closed typed unions, arbitrary command/path fields are rejected,
  and production capability imports fail repository checks.
- **[Stability gating delays recovery]** -> Raw failures remain immediately
  visible; only mutation readiness waits for policy-defined evidence.
- **[Cancellation races with completion]** -> Durable compare-and-swap
  transitions and before/after ownership verification select one terminal path.
- **[Crash leaves uncertain applied state]** -> Claimed attempts are never
  automatically rerun; explicit observation and recovery either prove state or
  isolate the target in `safe_mode`.
- **[Gap repair grows into cloud control]** -> Acknowledgements can reference
  telemetry sequence ranges only and are not visible to local policy or action
  packages.
- **[Provenance metadata leaks topology]** -> Cloud projection is a separate
  allowlisted schema with secret-canary tests; rich local records are bounded.
- **[The framework creates premature confidence]** -> Qualification proves only
  synthetic engine semantics. Every real adapter and ownership cutover remains
  a separate blocked change.

## Migration Plan

1. Complete and archive atomic-policy and observable-connectivity qualification.
2. Add strict action, acknowledgement, provenance and telemetry-range schemas
   plus canonical synthetic fixtures.
3. Implement the pure readiness and proposal-to-plan packages without daemon
   integration.
4. Implement the durable attempt coordinator, synthetic adapters, cancellation,
   compensation and replay under a disabled build/runtime gate.
5. Add bounded telemetry gap acknowledgements and local replay independently of
   action execution.
6. Wire only synthetic shadow comparison into disjoint Hexroute daemons; keep
   all production adapter capability IDs absent.
7. Run crash, race, replay, secret-canary and capability-leak qualification.
8. Roll back by disabling the engine and gap-repair uploader. Existing
   observe-only snapshots, legacy telemetry retry, Twilight, AdGuard and both
   Codex paths remain unchanged.

Failed prerequisite activation, engine startup, local replay or cloud delivery
leaves the existing observe-only control plane and Twilight production runtime
available. No failure in this change can authorize a production mutation.

## Open Questions

Production adapter order, per-capability safety envelopes, two-domain action
coordination and root/user ownership cutovers remain intentionally unresolved.
Each requires its own grill session and OpenSpec change after this engine and
its prerequisites qualify.
