## ADDED Requirements

### Requirement: Qualified synthetic-only first rollout

The reconciliation engine SHALL remain disabled in daemon runtime until the
atomic-policy and observable-connectivity prerequisite changes have complete
replay-derived qualification and synchronized baseline requirements. Its first
enabled capability registry SHALL contain only synthetic adapters and SHALL NOT
contain production route, DNS, firewall, process, tunnel, Pritunl, Keychain or
credential mutation capabilities.

#### Scenario: A prerequisite qualification is incomplete

- **WHEN** either prerequisite gate is collecting, invalid, generation-mismatched or incomplete
- **THEN** daemon startup exposes no proposal-translation or action-execution operation
- **AND** existing observe-only state and Twilight production ownership remain unchanged

#### Scenario: Synthetic qualification is enabled

- **WHEN** both prerequisites are complete and the reconciler feature gate is enabled
- **THEN** only declared synthetic capability identifiers can create action attempts
- **AND** no production mutation package or adapter is reachable

### Requirement: Generation-bound action readiness

The reconciler SHALL derive action readiness only from a fresh canonical
connectivity snapshot, exact active bundle/root/user policy generations,
current boot identity, source watermarks and policy-defined thresholds, budgets,
backoff and cooldown. Readiness SHALL be `ready`, `temporarily_blocked` or
`denied`; raw component state SHALL remain separately observable.

#### Scenario: One probe fails below the stability threshold

- **WHEN** a raw component fact reports failure but the configured consecutive or duration threshold is not satisfied
- **THEN** the failure remains visible in the snapshot
- **AND** readiness is `temporarily_blocked` and no action lease is minted

#### Scenario: Relevant source evidence has a gap

- **WHEN** the proposal depends on a source with an uncleared sequence gap, conflict or stale baseline
- **THEN** readiness is not `ready`
- **AND** no guessed healthy or actionable state is produced

#### Scenario: Stable evidence and policy gates pass

- **WHEN** authoritative facts are fresh and stable and policy, budget, backoff and cooldown gates all permit the exact capability
- **THEN** readiness is `ready` for the referenced snapshot and policy generations
- **AND** any later generation or relevant source-watermark change invalidates that readiness

### Requirement: Pure exact proposal-to-plan translation

The translator SHALL accept only an immutable fresh reconciliation proposal,
its referenced snapshot and diff, exact active effective policy, static
capability descriptor and canonical adapter metadata. It SHALL perform no I/O,
and equal canonical inputs SHALL produce the same typed action-plan digest. The
plan SHALL bind target, snapshot, policy/control generations, proposal and diff
digests, capability and adapter version, readiness digest, ordered steps,
verification expectations and compensation digests.

#### Scenario: The same proposal is translated repeatedly

- **WHEN** identical validated inputs are translated more than once
- **THEN** the canonical plan and digest are identical
- **AND** translation performs no process, network, filesystem, environment, clock or credential access

#### Scenario: Proposal binding is stale

- **WHEN** snapshot, bundle, domain, control, target, capability or adapter binding differs from the proposal
- **THEN** translation is denied with a stable bounded reason
- **AND** no action plan or lease is created

#### Scenario: Desired and current state are semantically equal

- **WHEN** fresh typed comparison proves the proposal is a semantic no-op
- **THEN** the reconciler returns an accepted no-action result
- **AND** it does not mint a lease or create an execution attempt

### Requirement: Typed acknowledgement and retry contract

Every proposal submission SHALL return exactly one acknowledgement classified
as `accepted`, `temporarily_rejected` or `denied` with an allowlisted reason.
Only `temporarily_rejected` MAY include a bounded retry-after hint. Raw runtime,
adapter, process, network and credential errors SHALL NOT appear in the
acknowledgement.

#### Scenario: A pending action is durably created

- **WHEN** readiness passes and the exact plan and one-time lease are persisted successfully
- **THEN** the acknowledgement is `accepted`
- **AND** it identifies the action without exposing plan internals or protected values

#### Scenario: Cooldown temporarily blocks an allowed action

- **WHEN** policy authorizes the capability but its cooldown has not elapsed
- **THEN** the acknowledgement is `temporarily_rejected` with a stable cooldown reason
- **AND** the same request is not silently queued for later execution

#### Scenario: Capability is not statically declared

- **WHEN** a proposal names a capability outside the installed static allowlist
- **THEN** the acknowledgement is `denied`
- **AND** replaying the same request identity cannot turn it into an accepted action

### Requirement: Durable explicit attempt lifecycle

Every executable action SHALL use the existing valid one-time action lease and
an append-only attempt lifecycle with `pending`, `claimed`, `running`,
`verifying` and `committed` states plus terminal `expired`, `denied`,
`cancelled`, `rolled_back`, `failed` and `safe_mode` outcomes. Each transition
SHALL be generation-guarded and durably appended before the next side effect.
Execution outcome and telemetry delivery state SHALL be independent.

#### Scenario: An action completes successfully

- **WHEN** one attempt claims a valid pending lease, applies every verified owned step and verifies the final state
- **THEN** its lifecycle advances monotonically to `committed`
- **AND** the immutable action, nonce, boot, attempt and plan identities remain unchanged

#### Scenario: Telemetry reporting fails after commit

- **WHEN** a committed outcome cannot be uploaded or acknowledged
- **THEN** the local outcome remains `committed` and its report remains pending
- **AND** reporting failure does not repeat, roll back or otherwise affect the action

#### Scenario: Lease expires before claim

- **WHEN** the continuous monotonic lease deadline passes before an execution claim
- **THEN** the attempt becomes `expired`
- **AND** no adapter step is invoked

### Requirement: Fail-closed crash and claim recovery

An executor SHALL durably claim a pending lease with one attempt ID before any
mutation. A different process, boot or attempt SHALL NOT resume or repeat a
claimed action lacking a durable terminal outcome. Startup recovery SHALL
observe target state and require an explicit verified recovery decision.

#### Scenario: Executor crashes after claim and before apply

- **WHEN** startup finds a claimed attempt without an applied-step record or terminal outcome
- **THEN** the action is not automatically rerun
- **AND** recovery must prove untouched state before resolving the attempt

#### Scenario: Executor crashes after an apply before outcome persistence

- **WHEN** startup cannot prove whether the claimed transaction still owns the observed state
- **THEN** only that target enters `safe_mode`
- **AND** no new action is admitted for the target until explicit verified recovery

### Requirement: Diff-based reconnect-safe rehydration

Every capability adapter SHALL compare fresh typed current state with exact
desired state before proposing a step. Tunnel/interface lifecycle, scoped route,
DNS, firewall, process and user-access changes SHALL remain separate typed
operation classes and ownership domains. No generic restart operation SHALL be
available.

#### Scenario: An authorized managed interface is missing

- **WHEN** fresh authoritative observation reports a missing Hexroute-owned interface and current desired state requires it
- **THEN** the adapter may produce only the exact typed reconstruction steps for that interface
- **AND** unchanged route, DNS, firewall and process classes are omitted

#### Scenario: Observed state is foreign or ambiguous

- **WHEN** an interface, route, process or session cannot be proven as owned by the exact transaction or protected namespace
- **THEN** the diff is a conflict and no purge, adoption, restart or compensation step is produced

#### Scenario: Current and desired capability state match

- **WHEN** the adapter's canonical semantic comparison reports equality
- **THEN** rehydration is a no-op
- **AND** no apply call is made

### Requirement: Durable cancellation and verified cleanup

Cancellation SHALL be a durable intent that prevents the next unstarted action
step. If transaction-owned state was applied, cancellation SHALL use a bounded
cancellation-independent compensation context and reverse only the verified
owned applied prefix. Temporary helpers, private files and capability-local
credential leases SHALL be registered to the attempt and closed on every
terminal path.

#### Scenario: Cancellation arrives before the first apply

- **WHEN** a pending or claimed attempt records cancellation before any step is applied
- **THEN** the attempt becomes `cancelled`
- **AND** no adapter apply or compensation operation runs

#### Scenario: Cancellation arrives after one owned step

- **WHEN** one step is durably verified as transaction-owned before cancellation
- **THEN** the exact verified inverse is attempted for only that owned step
- **AND** successful compensation ends as `rolled_back`

#### Scenario: Cleanup cannot prove ownership

- **WHEN** applied or temporary state is missing, foreign, ambiguous or changed during cleanup
- **THEN** Hexroute does not mutate that state
- **AND** the target enters `safe_mode` with a bounded critical incident

### Requirement: Typed end-to-end action provenance

Proposal, readiness, lease, attempt, step, compensation, outcome and incident
records SHALL share a minimal canonical provenance header containing schema and
record identity, strict record kind, parent and root action identity,
producer/domain, boot identity, exact policy/control/snapshot generations,
source/input/output digests and applicable wall and monotonic time. Each record
kind SHALL retain a separate bounded payload and canonical digest.

#### Scenario: Action history is replayed

- **WHEN** a complete valid record chain is replayed from proposal through outcome
- **THEN** Hexroute reconstructs the same attempt state, plan binding and terminal result
- **AND** no relationship is inferred only from timestamps or display text

#### Scenario: Parent or output evidence is altered

- **WHEN** a record is missing, reordered, rewritten or has an invalid parent or output digest
- **THEN** replay fails closed with a lineage reason
- **AND** the chain cannot authorize or qualify an action

#### Scenario: Record payload types are substituted

- **WHEN** an acknowledgement, readiness, outcome or incident payload is decoded under another record kind
- **THEN** strict decoding rejects the record
- **AND** a generic metadata map cannot bypass the type boundary

### Requirement: Domain and cloud authority isolation

Root and user action plans, leases, attempts and journals SHALL remain separate.
Credential values and protected credential references SHALL NOT cross domain
IPC, logs, telemetry or diagnostics. Cloud availability, acknowledgements and
stored state SHALL NOT participate in readiness, translation, lease issuance,
execution, verification, compensation or local recovery.

#### Scenario: Root proposal requires a user credential

- **WHEN** root translation encounters an operation requiring PIN, TOTP, OTP, Keychain or another user-owned credential class
- **THEN** translation is denied as a domain violation
- **AND** no credential reference or value is serialized

#### Scenario: Cloud is unavailable during local synthetic execution

- **WHEN** telemetry API, worker, PostgreSQL or object storage is unavailable
- **THEN** local execution and outcome persistence follow only local policy and state
- **AND** bounded eligible reports remain queued without blocking the attempt

### Requirement: Synthetic fault qualification and capability leak gate

Before this change can complete, deterministic synthetic qualification SHALL
cover no-op, every acknowledgement class, expiry, cancellation before/after
apply, compensation, crash after claim/apply, verification mismatch, policy and
state generation change, missing-state rehydration, foreign-state conflict,
telemetry gap repair and unrecoverable gap. Repository checks SHALL reject
production mutation dependencies and undeclared capability paths.

#### Scenario: Synthetic qualification passes

- **WHEN** every mandatory trace replays to the same canonical plan and terminal outcome with valid provenance
- **THEN** the engine may be marked qualified for synthetic operation
- **AND** the result grants no production capability or ownership

#### Scenario: Production mutation dependency is introduced

- **WHEN** the engine or synthetic adapter imports or reaches an undeclared route, DNS, process, tunnel, Pritunl, Keychain, credential or network mutation path
- **THEN** repository and release checks fail
- **AND** the change cannot be enabled or marked complete

#### Scenario: Coexistence evidence regresses

- **WHEN** a synthetic test or shadow run changes Twilight, AdGuard, Pritunl, sing-box, routes, DNS or either Codex path
- **THEN** qualification fails
- **AND** rollback disables the engine without attempting a network inverse
