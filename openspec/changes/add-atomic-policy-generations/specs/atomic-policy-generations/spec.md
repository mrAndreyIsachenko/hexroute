## ADDED Requirements

### Requirement: Closed policy source composition

The policy compiler SHALL compose a non-overridable compiled safety baseline,
disjoint root and user policy namespaces, and bounded operator authorization
leases into one complete effective snapshot. Operator leases SHALL only narrow
predeclared capabilities by intersection, compiled denies SHALL win, and no
source SHALL use last-writer-wins behavior.

#### Scenario: Operator source attempts to expand the safety envelope

- **WHEN** an operator source authorizes a capability or selector outside the compiled safety baseline
- **THEN** the compiler rejects the complete candidate
- **AND** no domain payload is eligible for prepare or activation

#### Scenario: Root and user sources declare disjoint valid policy

- **WHEN** root and user sources stay inside their owned namespaces and the compiled envelope
- **THEN** the compiler includes both in one effective snapshot
- **AND** neither daemon receives policy owned by the other privilege domain

#### Scenario: Compiled deny intersects an operator lease

- **WHEN** an operator lease intersects both an allowed selector and a compiled deny
- **THEN** the denied portion remains unauthorized
- **AND** the lease cannot broaden the effective selector set

### Requirement: Strict deterministic policy compilation

`hexroute-policy` SHALL reject duplicate YAML keys, anchors, aliases and unknown
fields, SHALL decode into typed Go models, and SHALL emit RFC 8785 canonical JSON
for a manifest plus separate root and user payloads. Daemons SHALL NOT parse the
operator YAML or compose policy sources.

#### Scenario: Source contains ambiguous YAML features

- **WHEN** a policy source contains a duplicate key, anchor, alias or unknown field
- **THEN** compilation fails before an effective snapshot or signature is produced

#### Scenario: Equivalent source is reordered

- **WHEN** comments or source ordering change without changing effective authorization
- **THEN** canonical payload and manifest digests remain unchanged
- **AND** a semantic no-op generation cannot be committed

### Requirement: Signed compatible policy manifests

Every candidate SHALL have an Ed25519-signed canonical manifest containing the
policy schema, compiler version and digest, bundle and parent generations,
domain generations and payload hashes, static digest, signer identity and UTC
validity bounds. Each daemon SHALL independently verify the signature against a
statically pinned fingerprint and SHALL reject unknown fields, unsupported
schema versions and default schema downgrades.

#### Scenario: Manifest signature or payload digest is invalid

- **WHEN** either daemon detects a signature, signer fingerprint or payload hash mismatch
- **THEN** that daemon rejects prepare without changing its active pointer
- **AND** the rejection exposes only a bounded reason and candidate digest

#### Scenario: Candidate requires a different static configuration

- **WHEN** the signed manifest's static digest differs from the installed daemon configuration
- **THEN** prepare returns `restart_required`
- **AND** the candidate cannot be activated live

#### Scenario: Schema is unsupported by one domain

- **WHEN** either daemon does not support the candidate policy schema
- **THEN** the complete bundle is ineligible for activation
- **AND** binary or static rollout remains a separate operation

### Requirement: Static and dynamic authority separation

Static configuration SHALL own role and domain identity, UID/GID, sockets,
launchd labels, executable identities, fixed storage roots, credential
ownership, action allowlists, protected namespaces, signer fingerprint and
schema range. Dynamic policy MAY define only typed selectors, readiness rules,
thresholds, budgets, timing and predeclared capability leases inside that static
envelope.

#### Scenario: Dynamic policy attempts to change a protected identity

- **WHEN** a candidate attempts to change a daemon UID, executable identity, storage root or credential owner
- **THEN** the candidate is rejected or reported as `restart_required`
- **AND** no live activation changes the protected identity

#### Scenario: Dynamic threshold changes inside the envelope

- **WHEN** a candidate changes a bounded threshold allowed by the installed static policy
- **THEN** the value can participate in normal prepare and commit validation
- **AND** no process sandbox or privilege boundary changes

### Requirement: Selector ambiguity rejection

The compiler SHALL reject overlapping host, port, path, route/CIDR, action or
credential-reference selectors with different role, route, TLS, protocol,
signing or action semantics. Wildcard and concrete selectors SHALL NOT imply a
specificity override, while exact selectors with identical semantics SHALL be
deduplicated.

#### Scenario: Route selectors overlap with different paths

- **WHEN** two route or CIDR selectors overlap but assign different ownership or paths
- **THEN** the complete candidate is rejected before signing

#### Scenario: Wildcard overlaps a concrete conflicting selector

- **WHEN** wildcard and concrete selectors overlap with different semantics
- **THEN** the compiler reports a bounded conflict instead of selecting the more specific rule

#### Scenario: Duplicate selectors are semantically identical

- **WHEN** two exact selectors have identical authorization semantics
- **THEN** canonical compilation deduplicates them
- **AND** their source order does not affect the digest

### Requirement: Two-level monotonic generations

The effective snapshot SHALL carry one monotonic `bundle_generation` and
independent monotonic `root_policy_generation` and `user_policy_generation`
values. A domain action SHALL bind the bundle and its owning domain generation,
and a cross-domain action SHALL require both daemons to report the same bundle.

#### Scenario: Only user policy changes

- **WHEN** a semantic policy change affects only the user payload
- **THEN** the bundle and user policy generations advance
- **AND** the root policy generation and root payload digest remain unchanged

#### Scenario: Domains report different active bundles

- **WHEN** root and user daemons report different active bundle generations
- **THEN** new local mutations are blocked
- **AND** the existing data plane remains running

### Requirement: Mandatory semantic diff and replay

The compiler SHALL produce an offline semantic diff and SHALL replay the
candidate with a deterministic Go evaluator against synthetic safety fixtures
and recent redacted local observation traces before signing. The report SHALL
identify newly allowed, newly denied and changed plans, highlight expansions and
block signing on any safety-invariant violation.

#### Scenario: Replay finds a newly allowed unsafe plan

- **WHEN** a candidate permits a plan that violates a synthetic safety invariant
- **THEN** signing is blocked
- **AND** no candidate files are installed in either daemon store

#### Scenario: Candidate passes deterministic replay

- **WHEN** repeated offline replay uses the same candidate and trace inputs
- **THEN** it produces the same normalized plan classifications and report digest

### Requirement: User-presence policy approval

The operator Ed25519 key SHALL remain in the user Keychain and SHALL require
user presence for signing. The signature SHALL approve only the exact canonical
manifest and domain hashes within a bounded activation window. Daemons and cloud
components SHALL NOT possess the private key or mint replacement generations.

#### Scenario: Operator approves a reviewed candidate

- **WHEN** semantic diff and replay pass and Keychain user presence succeeds
- **THEN** the compiler signs the exact candidate digest once
- **AND** prepare and commit cannot substitute a different payload or digest

#### Scenario: Automatic recovery evaluates an active policy

- **WHEN** an already active policy permits an automatic action in a future capability
- **THEN** runtime evaluation does not require another Touch ID prompt
- **AND** execution still requires a valid one-time action lease

### Requirement: Immutable bounded policy storage

Root and user domains SHALL use separate mode-private stores containing
immutable generation-addressed regular files and signed active-pointer records
updated by atomic rename. Stores SHALL reject symlinks and caller-supplied paths,
retain the newest 16 valid generations and unresolved prepares, remove full
rejected payloads, and bound retained audit metadata to 90 days.

#### Scenario: Candidate path is a symlink or has invalid ownership

- **WHEN** a daemon resolves its fixed candidate path and finds a symlink, wrong owner or wrong mode
- **THEN** prepare fails without parsing or activating the candidate

#### Scenario: Retention limit is exceeded

- **WHEN** more than 16 resolved valid generations are retained
- **THEN** the oldest eligible generation files are removed
- **AND** unresolved prepared state and bounded audit metadata remain available

### Requirement: Typed atomic policy activation

Policy activation SHALL use versioned `PreparePolicy`, `CommitPolicy` and
`AbortPolicy` IPC requests containing only bounded transaction identity,
generation and digest fields. Each daemon SHALL derive fixed local paths and
independently validate ownership, mode, schema, signature, hashes, validity,
static binding and domain semantics before persisting a prepare receipt.

#### Scenario: Both domains prepare and commit the same bundle

- **WHEN** root and user daemons persist valid prepare receipts for the same signed bundle and domain hashes
- **THEN** commit atomically replaces each domain's signed active pointer
- **AND** both daemons publish the same active bundle generation

#### Scenario: One domain rejects prepare

- **WHEN** either daemon rejects prepare
- **THEN** the activation transaction aborts without committing either candidate
- **AND** both daemons continue using their last valid active generation

#### Scenario: IPC attempts to supply a path or policy payload

- **WHEN** a caller sends an unversioned operation, arbitrary filesystem path or policy payload
- **THEN** the daemon rejects the request as unsupported
- **AND** no file watcher or signal-based reload activates policy

### Requirement: Crash-safe cross-domain convergence

Prepared receipts and the signed commit intent SHALL be durable. If one daemon
commits and the other is unavailable, the system SHALL expose `domain_mismatch`,
block new mutations and converge the lagging daemon forward to the same signed
bundle after restart and complete revalidation. It SHALL NOT automatically roll
the committed daemon back.

#### Scenario: User daemon crashes between domain commits

- **WHEN** root commits the signed bundle and user crashes before updating its active pointer
- **THEN** root reports `domain_mismatch` and authorizes no new mutation
- **AND** existing data plane connections are not stopped

#### Scenario: Lagging daemon restarts with a valid commit intent

- **WHEN** the user daemon restarts and revalidates the same durable signed commit intent
- **THEN** it commits the matching user payload and clears the mismatch after both statuses agree

### Requirement: Fail-closed invalid candidate handling

An invalid candidate SHALL be rejected atomically with no partial activation.
The last valid generation SHALL continue when available; without one, the
daemon SHALL enter observe-only `SAFE_MODE`. Candidate rejection SHALL NOT stop
AdGuard, Twilight, Pritunl, sing-box or established connectivity.

#### Scenario: Candidate fails conflict validation after a valid generation exists

- **WHEN** a new candidate is invalid and a valid generation is active
- **THEN** the active generation remains unchanged
- **AND** a bounded rejection event is recorded without selector or secret data

#### Scenario: Daemon starts without any valid generation

- **WHEN** no installed generation passes signature, digest and compatibility validation
- **THEN** the daemon runs observe-only in `SAFE_MODE`
- **AND** it grants no local mutation authority

### Requirement: Local authorization suspension

Each daemon SHALL maintain an `authorization_suspended` overlay independent of
policy generations. Active-bundle corruption, signature or digest mismatch,
domain mismatch, clock anomaly or IPC ownership violation SHALL suspend new
mutations, preserve the existing data plane and emit a bounded incident. The
overlay SHALL clear only after local revalidation and SHALL never expand policy.

#### Scenario: Active payload is corrupted on disk

- **WHEN** periodic or startup validation detects an active payload digest mismatch
- **THEN** the daemon suspends new authorization immediately
- **AND** it does not manufacture or activate a replacement generation

#### Scenario: Valid deny generation is activated

- **WHEN** an emergency policy is needed to revoke authorization
- **THEN** the operator compiles and signs a normal deny generation
- **AND** load failure or suspension is not treated as a substitute policy

### Requirement: Monotonic policy rollback

Rollback SHALL compile previously valid effective content into a newly signed
generation whose bundle and affected domain counters are greater than the
current counters. Rollback SHALL NOT decrement counters, reuse an old active
pointer, revive expired authorization leases or restore revoked credential
references.

#### Scenario: Operator rolls back to prior effective content

- **WHEN** generation 12 is rolled back to the effective content previously used by generation 9
- **THEN** the compiler creates generation 13 with a new parent and validity metadata
- **AND** normal diff, replay, signing, prepare and commit gates still apply

### Requirement: One-time generation-bound action leases

Every authorized mutation SHALL require a short-lived one-time lease containing
an action ID, bundle generation, owning domain generation, control-state
generation, exact target and plan digest, issue and expiry times, boot ID and
nonce. The executor SHALL reject replay, expiry, boot mismatch or any policy or
state-generation change and SHALL persist `committed`, `aborted` or `expired`.

#### Scenario: Control state changes before execution

- **WHEN** a lease's control-state generation no longer matches immediately before mutation
- **THEN** the action is rejected as stale
- **AND** the nonce cannot be reused for a later attempt

#### Scenario: Mac sleeps beyond the lease TTL

- **WHEN** a lease expires while the Mac is asleep
- **THEN** the action is not executed after wake
- **AND** the lease is durably classified as expired

#### Scenario: Mac reboots with an unfinished lease

- **WHEN** the current boot ID differs from the lease boot ID
- **THEN** the unfinished lease is invalid even if its wall-clock expiry has not passed

### Requirement: Exact transactional action plans

A multi-step action SHALL bind an immutable ordered plan digest, step-level
verification and an inverse plan. Policy and state generations SHALL be checked
before each mutation step and commit. Staleness or failure SHALL roll back only
the Hexroute-owned subset applied by that transaction; foreign or ambiguous
state SHALL remain unchanged.

#### Scenario: Policy changes after the first action step

- **WHEN** an action applies one owned step and then detects a policy-generation change
- **THEN** it aborts and applies the verified inverse for only that owned step
- **AND** it does not continue the stale plan

#### Scenario: Verified rollback fails

- **WHEN** an applied Hexroute-owned step cannot be restored by its inverse plan
- **THEN** the target enters `SAFE_MODE`
- **AND** further actions are blocked and a critical bounded incident is emitted

### Requirement: Policy and lease time semantics

Signed policy SHALL use UTC `issued_at`, `not_before` and `expires_at` bounds,
while action leases SHALL use a continuous monotonic clock plus boot ID. Clock
rollback or excessive skew SHALL block new activation, sleep SHALL count toward
lease TTL, and reboot SHALL require active-policy signature, digest, static and
validity revalidation.

#### Scenario: Wall clock moves backward before activation

- **WHEN** a candidate cannot satisfy the configured clock-skew checks
- **THEN** new activation is blocked and the last valid active policy continues
- **AND** the daemon reports a bounded clock anomaly

#### Scenario: Active policy is loaded after reboot

- **WHEN** the daemon restarts and the active pointer, signature, digest, static binding and validity all pass
- **THEN** the active policy can be restored
- **AND** no unfinished pre-reboot action lease is restored

### Requirement: Grandfathered existing data plane

Existing data-plane state that is no longer authorized by a newly active policy
SHALL be marked `grandfathered_noncompliant` rather than disconnected. It SHALL
receive no new recovery action under the old authorization and SHALL require an
exact reconcile or drain plan under a new lease. Passing `reconcile_by` SHALL
raise an incident but SHALL NOT create an implicit stop action.

#### Scenario: New policy denies an established VPN session

- **WHEN** a newly active policy no longer authorizes an already established session
- **THEN** Hexroute marks the session `grandfathered_noncompliant`
- **AND** it does not disconnect the session or stop AdGuard, Twilight or the VPN process

#### Scenario: Reconcile deadline passes

- **WHEN** `reconcile_by` passes without an authorized reconcile plan
- **THEN** a bounded incident is emitted
- **AND** no hidden drain or disconnect is executed

### Requirement: Redacted policy observability

Local status, journals and cloud telemetry SHALL expose only policy generations
and digests, lifecycle states including `prepared`, `active`, `rejected`,
`restart_required`, `domain_mismatch` and `authorization_suspended`, bounded
allowlisted reasons and activation timestamps. They SHALL NOT expose selectors,
endpoints, source paths, leases, credential references or credential values.

#### Scenario: Candidate rejection is uploaded

- **WHEN** a rejection event is eligible for cloud ingestion
- **THEN** it contains only allowlisted generation, digest, state, time and bounded reason fields
- **AND** secret-canary validation fails the event before persistence or upload if protected data appears

#### Scenario: Cloud service is unavailable during activation

- **WHEN** local prepare or commit runs while cloud API, PostgreSQL or worker is unavailable
- **THEN** local activation proceeds from local evidence only
- **AND** bounded eligible telemetry is queued without granting cloud mutation authority

### Requirement: Advisory policy proposals have no activation authority

Runtime observations and Codex MAY produce a redacted draft source proposal,
but SHALL NOT automatically allow, merge, sign, install or activate it. Every
draft SHALL pass normal compilation, conflict validation, replay, user-presence
signing and activation gates.

#### Scenario: Runtime observes a repeated denied action

- **WHEN** an advisor proposes a policy adjustment from redacted observations
- **THEN** the proposal remains an unsigned operator source draft
- **AND** daemon authorization remains unchanged

### Requirement: Shadow-gated bounded first activation

Policy evaluation SHALL remain shadow-only until it completes 72 continuous
eligible hours, at least two sleep/wake cycles, one reboot, and injected invalid
signature, selector conflict, stale generation and crash-between-domain-commit
tests with no unexplained safety mismatch. The first enforced capability SHALL
be only `operator_resume`.

#### Scenario: Shadow gate is incomplete

- **WHEN** any duration, lifecycle or fault-injection criterion has not passed
- **THEN** policy results remain observational
- **AND** no production route, process, Pritunl, sing-box or credential mutation authority is enabled

#### Scenario: First active capability is enabled after the gate

- **WHEN** all shadow criteria pass and `operator_resume` enforcement is explicitly enabled
- **THEN** policy may authorize only the generation-guarded resume state transition
- **AND** Twilight remains the production owner of the data plane
