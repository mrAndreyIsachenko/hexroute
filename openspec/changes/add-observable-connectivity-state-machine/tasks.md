## 1. Fact And Ownership Contracts

- [x] 1.1 Add bounded typed connectivity fact models, component tagged payloads, lifecycle states, source identity, boot identity, sequence, freshness and reason enums.
- [x] 1.2 Add strict canonical encoding and decoding that rejects unknown fields, trailing data, invalid bounds and non-canonical persisted facts.
- [x] 1.3 Add static root/user component ownership and authoritative/corroborating source declarations inside the compiled safety envelope.
- [x] 1.4 Add validation tests proving cross-domain facts, second authoritative owners, commands, arbitrary paths and protected credential classes are rejected.
- [x] 1.5 Add synthetic public fixtures for every component class without live endpoints, selectors, routes, session identities or credentials.

## 2. Ordered Idempotent Acceptance

- [x] 2.1 Implement canonical fact digests and source identity checks for `(source_id, boot_id, source_sequence)`.
- [x] 2.2 Implement durable monotonic host acceptance sequencing with exact-retry idempotency and conflicting-reuse rejection.
- [x] 2.3 Implement explicit source-gap tracking and clearing only through a later validated complete baseline fact.
- [x] 2.4 Add duplicate, reordered, delayed, conflicting and sequence-gap unit and race tests across independent sources.
- [x] 2.5 Prove corroborating facts remain visible as evidence but cannot replace authoritative component state.

## 3. Normalized Snapshot And Pure Reducer

- [x] 3.1 Add the versioned `ConnectivitySnapshot` model with source watermarks, policy generations, component records, freshness, gaps, conflicts and aggregate summary.
- [x] 3.2 Implement a pure reducer from validated prior snapshot, ordered accepted facts and exact policy-generation descriptor with no I/O or environment access.
- [x] 3.3 Implement deterministic policy-aware summary derivation while retaining every configured component record in local status.
- [x] 3.4 Implement semantic no-op detection and exactly-once snapshot-generation advancement for effective changes.
- [x] 3.5 Mark desired state and proposals unauthorized when active policy is absent, invalid, suspended or generation-mismatched while preserving observations.
- [x] 3.6 Add canonical determinism, permutation, no-op, generation-change and partial-component-failure tests.

## 4. Desired State, Diff And Proposals

- [x] 4.1 Add a read-only adapter from the revalidated active effective policy to bounded per-domain desired connectivity state.
- [x] 4.2 Implement `converged`, `missing`, `unexpected`, `divergent`, `stale`, `unknown`, `conflict` and `grandfathered_noncompliant` diff classification.
- [x] 4.3 Add immutable digest-addressed `ReconciliationProposal` values bound to snapshot, bundle and owning-domain policy generations and canonical diff digest.
- [x] 4.4 Reject proposal construction containing commands, arguments, paths, endpoints, selectors, process details or credential references.
- [x] 4.5 Add tests proving established newly unauthorized state is reported but never disconnected and stale proposals cannot be resumed after state or policy changes.
- [x] 4.6 Add an architectural test proving no IPC operation, callback or package dependency can execute a reconciliation proposal or mint an action lease.

## 5. Journal, Checkpoint And Replay

- [x] 5.1 Extend separate root and user crash-safe priority journals with validated connectivity-fact records and accepted host-sequence metadata.
- [x] 5.2 Add an atomic generation-guarded aggregate checkpoint and bounded append-only index containing checkpoint/parent identity, prior snapshot digest, consumed host sequence range and source watermarks, exact policy and reducer identity, and canonical snapshot/diff/proposal output digests.
- [x] 5.3 Implement startup lineage validation, bounded search for the newest fully valid retained read-model ancestor and deterministic replay of a continuous accepted-fact journal under the current active policy; never move the policy active pointer backward.
- [x] 5.4 Extend retention so the latest complete baseline for every configured component and critical transitions survive before diagnostic eviction.
- [x] 5.5 Add crash-point tests around journal/index append, checkpoint file sync, rename and directory sync plus parent-link tamper, output-digest tamper, missing ancestor, bounded-depth exhaustion, corrupted checkpoint and truncated-journal recovery.
- [x] 5.6 Add bounded-overflow tests proving an overflow condition is visible and no guessed healthy state is loaded.

## 6. Time, Sleep And Boot Semantics

- [x] 6.1 Add wall-clock, source-monotonic and boot-ID validation with no cross-boot reuse of monotonic freshness deadlines.
- [x] 6.2 Add a full-wake event that marks network, DNS, tunnel, relay and session components stale until complete owner baselines arrive.
- [x] 6.3 Add deterministic sleep/wake, wall-clock jump, reboot, delayed-baseline and stale-session tests.
- [x] 6.4 Prove pre-sleep or pre-reboot proposals cannot become current without fresh reduction under current boot, snapshot and policy generations.

## 7. Privilege-Separated Daemon Integration

- [x] 7.1 Gate daemon integration on completed atomic-policy startup, domain-mismatch, suspension and redacted-status contracts; document the exact prerequisite task evidence.
- [x] 7.2 Adapt existing root network, DNS, scoped-route, transport and relay observations into complete root-owned facts without adding new mutation calls.
- [x] 7.3 Adapt existing user Pritunl and session observations into complete user-owned facts without reading or serializing PIN, TOTP, OTP or Keychain references.
- [x] 7.4 Extend authenticated bounded IPC with user-fact publication and exact peer-UID/domain/size validation, without adding an action request.
- [x] 7.5 Integrate host acceptance sequencing, pure reduction, checkpointing and normalized local status into `hexrouted` behind an observe-only feature gate.
- [x] 7.6 Mark absent or stale user IPC as unknown/stale without impersonation, credential access or reconnect attempts.
- [x] 7.7 Add integration tests proving root/user ownership, daemon restart convergence, Twilight namespace coexistence and unchanged AdGuard and Codex paths.

## 8. Redacted Status And Cloud Projection

- [ ] 8.1 Add bounded local operator rendering that shows aggregate state beside component states, freshness, gaps, conflicts, generations and proposal classes.
- [ ] 8.2 Add a versioned allowlisted cloud projection schema and signed idempotent ingestion support for connectivity snapshots.
- [ ] 8.3 Add projection and telemetry secret-canary tests rejecting topology, selector, endpoint, path, process, event, session, proposal-digest and credential fields.
- [ ] 8.4 Add cloud persistence and dashboard read-model support for the redacted projection without any control endpoint or local callback.
- [ ] 8.5 Prove API, PostgreSQL, worker and dashboard loss or stale cloud data cannot influence local reduction, policy generation or proposals.

## 9. Replay And Shadow Qualification

- [ ] 9.1 Add an offline replay harness that verifies checkpoint lineage and canonical snapshot, diff and proposal digests from a valid ancestor, current policy descriptor and synthetic accepted-fact trace.
- [ ] 9.2 Add synthetic fault traces for duplicate, reorder, gap, collector loss, conflict, parent/output tamper, missing ancestor, bounded recovery-depth exhaustion, checkpoint corruption, policy change, sleep/wake and reboot.
- [ ] 9.3 Add a shadow comparison recorder that correlates normalized proposals with existing component planner output without executing either proposal.
- [ ] 9.4 Record the qualification gate as a canonical append-only hash-linked evidence chain for 72 eligible hours, two sleep/wake cycles, one reboot and every mandatory injected failure with no unexplained divergence; bind each result to source checkpoint/snapshot/diff/proposal/fault-trace digests and reject gaps, tamper and cross-session evidence.
- [ ] 9.5 Capture rollback evidence showing the reducer, collectors and status integration can be disabled while existing observation, Twilight, AdGuard and both Codex paths remain unchanged.
- [ ] 9.6 Block completion and any follow-up executor proposal when retained facts cannot reproduce a published snapshot or proposal.

## 10. Documentation And Verification

- [ ] 10.1 Document fact ownership, snapshot fields, reducer invariants, desired-state diff semantics, proposal non-executability and operator status interpretation.
- [ ] 10.2 Document the pinned Firezone, NetBird, Agent Framework and Chain Indexer architectural references, adopted mechanisms and Hexroute-specific safety differences.
- [ ] 10.3 Document startup replay, sleep/wake re-baselining, cloud-loss behavior, observe-only rollout and independently executable rollback.
- [ ] 10.4 Run focused unit, race, replay, crash-recovery, IPC, cloud projection, secret-canary and cross-platform build tests for affected packages.
- [ ] 10.5 Run `make check` and resolve every static, race, formatting, repository-boundary and secret-leak failure.
- [ ] 10.6 Run `openspec validate add-observable-connectivity-state-machine --strict` and keep proposal, design, specs and tasks synchronized with implementation evidence.
- [ ] 10.7 Sync validated delta requirements into baseline specs only after implementation and shadow qualification are complete.
