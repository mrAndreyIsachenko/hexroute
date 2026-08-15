## 1. Prerequisite And Capability Boundaries

- [x] 1.1 Record the exact completed atomic-policy and observable-connectivity qualification artifacts and synchronized baseline revisions required before daemon integration.
- [x] 1.2 Add a prerequisite gate that fails closed when either change is incomplete, invalid, generation-mismatched or not synchronized.
- [x] 1.3 Define the static reconciliation capability registry with synthetic capability identifiers only and no production adapter identifiers.
- [x] 1.4 Add repository dependency/import checks rejecting route, DNS, firewall, process, tunnel, Pritunl, Keychain, credential and undeclared network mutation paths.
- [x] 1.5 Add build and startup tests proving the engine packages may exist while proposal translation and execution IPC remain absent until the prerequisite gate passes.
- [x] 1.6 Add synthetic fixtures with no live endpoint, route, selector, process, session or credential values.

## 2. Action, Acknowledgement And Provenance Contracts

- [x] 2.1 Add bounded versioned models for readiness, typed acknowledgements, action plans, operation sessions, checkpoints, attempts, steps, resources, outcomes and report-delivery state.
- [x] 2.2 Add the minimal `ActionProvenance` header with strict record-kind payloads, parent/root identity, domain/boot/generation bindings and source/input/output digests for operation-session and action records.
- [x] 2.3 Implement canonical encoding, hashing and strict decoding that rejects unknown fields, trailing data, non-canonical forms and payload-kind substitution.
- [x] 2.4 Define allowlisted reason, retry and terminal-outcome enums without raw runtime or adapter error strings.
- [x] 2.5 Add size, count and string bounds for every action and provenance record plus rejection fixtures at each boundary.
- [x] 2.6 Add secret-canary tests proving credentials, references, topology, endpoints, selectors, paths, process output and session identities cannot enter persisted or projected records.

## 3. Generation-Bound Readiness

- [x] 3.1 Implement a pure readiness evaluator over a fresh canonical snapshot, exact policy/control generations, boot/source watermarks and bounded lifecycle state.
- [x] 3.2 Implement policy-defined consecutive/duration thresholds, action budgets, backoff and cooldown without caller-supplied readiness flags.
- [x] 3.3 Classify readiness as `ready`, `temporarily_blocked` or `denied` with bounded retry-after only for temporary conditions.
- [x] 3.4 Invalidate readiness on relevant source gap/conflict/staleness, boot change, policy/control generation change, suspension or target ownership change.
- [x] 3.5 Add deterministic tests for single transient failure, stable failure, cooldown, exhausted budget, missing baseline, source gap/conflict and policy change.
- [x] 3.6 Add status rendering tests proving raw component state and action readiness remain separately visible and cannot be flattened into one healthy/actionable flag.

## 4. Proposal Translation And Acknowledgements

- [x] 4.1 Implement the pure proposal translator from exact proposal, snapshot, diff, active policy, static capability descriptor and canonical adapter metadata.
- [x] 4.2 Bind generated plans to target, proposal/diff/readiness digests, snapshot, bundle/domain/control generations, capability/adapter version and ordered verification/compensation steps.
- [x] 4.3 Reject arbitrary commands, arguments, paths, endpoints, credential fields and undeclared operation classes during plan construction.
- [x] 4.4 Implement canonical semantic no-op handling that returns accepted no-action without minting a lease or attempt.
- [x] 4.5 Implement `accepted`, `temporarily_rejected` and `denied` acknowledgements with exact durable-acceptance semantics and replay-safe request identity.
- [x] 4.6 Add determinism, stale-binding, wrong-owner, undeclared-capability, no-op and acknowledgement retryability tests.
- [x] 4.7 Add an architectural test proving translation performs no I/O and cannot access environment, clock, filesystem, process, network or credential packages.

## 5. Durable Attempt Lifecycle And Crash Recovery

- [x] 5.1 Add a domain-local crash-safe append-only action journal and generation compare-and-swap transition API.
- [x] 5.2 Implement `pending`, `claimed`, `running`, `verifying`, `committed`, `expired`, `denied`, `cancelled`, `rolled_back`, `failed` and `safe_mode` transition validation.
- [x] 5.3 Integrate the existing one-time lease and immutable nonce/execution-claim contract without creating a second authorization mechanism.
- [x] 5.4 Persist each transition before its following side effect and keep action, nonce, boot, attempt and plan identities immutable.
- [x] 5.5 Implement startup classification of unfinished attempts without automatic rerun by a different process, boot or attempt.
- [x] 5.6 Add explicit verified recovery for untouched, exactly-owned applied and uncertain target states; route uncertainty only to target-local `safe_mode`.
- [x] 5.7 Add crash-point, duplicate-worker, competing-claim, expiry-during-sleep, reboot and journal-tamper tests.
- [x] 5.8 Add a domain-local operation-session checkpoint store with manifest digest, contract/runtime versions, checkpoint sequence, parent checkpoint digest, policy/control/snapshot bindings and child action references.
- [x] 5.9 Implement operation-session lifecycle transitions for `running`, `suspended`, `cancelled`, `failed` and `completed` without granting authorization or minting leases.
- [x] 5.10 Implement explicit resume validation that rejects manifest drift, generation drift, missing ancestry, sequence gaps, child action ambiguity and owner-attempt mismatch before any proposal or adapter step.
- [x] 5.11 Add replay-gated continuation records for future human approval flows, including rejection, timeout and changed-plan outcomes that leave action state unchanged.
- [x] 5.12 Add checkpoint-store failure, manifest-mismatch, competing-resume, suspended-resume, approval-rejection and approval-timeout tests.

## 6. Synthetic Diff And Rehydration Adapters

- [x] 6.1 Define a closed typed synthetic adapter interface for observe, semantic compare, apply, verify, compensate and cleanup.
- [x] 6.2 Implement in-memory and crash-fixture synthetic adapters with canonical current/desired state and deterministic fault injection.
- [x] 6.3 Keep interface/tunnel, scoped route, DNS, firewall, process and user-access operation classes separate in the synthetic plan model.
- [x] 6.4 Implement missing-state reconstruction only from fresh authorized desired state and exact ownership metadata, with unchanged classes omitted.
- [x] 6.5 Reject foreign, ambiguous, protected or unexpectedly changed state without purge, adoption, restart or compensation.
- [x] 6.6 Add no-op, missing-state rehydration, partial divergence, foreign conflict and verification-mismatch tests proving no generic restart path exists.

## 7. Cancellation, Cleanup And Compensation

- [x] 7.1 Add durable cancellation intent and compare-and-swap handling for pending, claimed, running and verifying attempts.
- [x] 7.2 Prevent the next unstarted step after cancellation and finish cancel-before-apply without invoking apply or compensation.
- [x] 7.3 Reuse the atomic-policy verified reverse-prefix rules to compensate only exact transaction-owned applied state under a bounded cancellation-independent context.
- [x] 7.4 Add typed registration and terminal cleanup for synthetic helpers, private temporary files and capability-local lease fixtures.
- [x] 7.5 Preserve immutable forensic records while reporting cleanup uncertainty or failure as a bounded outcome and target-local incident.
- [x] 7.6 Add race tests for cancel-versus-claim, cancel-versus-apply, cancel-versus-commit, generation change during compensation and cleanup failure.

## 8. Telemetry Gap Repair And Redacted Outcomes

- [x] 8.1 Extend signed ingestion acknowledgements with node/request binding, durable high-watermark and bounded sorted missing-sequence ranges.
- [x] 8.2 Validate range count, width, ordering, node/request identity, response size, retry rate and local scan-work bounds.
- [x] 8.3 Implement local exact-record replay for retained requested ranges without synthesis, renumbering or reducer/action package dependencies.
- [x] 8.4 Persist report delivery independently as `pending`, `acknowledged` or `terminally_rejected` without changing local action outcome.
- [x] 8.5 Emit one redacted `telemetry_gap_unrecoverable` record when requested evidence has expired locally while allowing newer uploads.
- [x] 8.6 Add a strict redacted action-evidence projection and reject action-like fields in ingestion responses.
- [x] 8.7 Add API, PostgreSQL, worker-loss, duplicate, reordered range, oversized range, retained-gap, expired-gap and secret-canary tests.

## 9. Privilege-Separated Shadow Integration

- [x] 9.1 Add root and user domain-local synthetic action stores with disjoint paths, ownership and peer-authenticated typed IPC.
- [x] 9.2 Reject cross-domain proposals and keep root unable to forward or execute user actions or credential-backed operations.
- [x] 9.3 Wire synthetic proposal comparison and replay behind a disabled feature gate without adding production capability IDs or adapters.
- [x] 9.4 Prove missing root/user peer state disables cross-domain reconciliation while preserving observe-only status and the available domain.
- [x] 9.5 Add integration tests proving cloud loss and engine failure leave observable state, Twilight, AdGuard, Pritunl, sing-box, routes, DNS and both Codex paths unchanged.

## 10. Qualification, Documentation And Rollback

- [x] 10.1 Build canonical synthetic traces for no-op, all acknowledgement classes, operation-session resume/reject paths, expiry, cancellation before/after apply, compensation and generation changes.
- [x] 10.2 Add crash-after-claim/apply, verification mismatch, missing-state rehydration, foreign conflict, retained telemetry gap and unrecoverable-gap traces.
- [x] 10.3 Add a replay harness proving identical canonical sessions, checkpoints, plans, transitions, outcomes and provenance from every mandatory trace.
- [x] 10.4 Run unit, race, fuzz, crash-recovery, replay, IPC, schema-boundary, secret-canary and capability-leak test suites before enabling the synthetic runtime gate.
- [x] 10.5 Document readiness interpretation, acknowledgement semantics, crash/cancel recovery, telemetry gap repair and the absence of production adapters.
- [x] 10.6 Document that every real capability adapter, live shadow qualification and ownership cutover requires a separate grill session, OpenSpec change and independently executable rollback.
- [x] 10.7 Capture rollback evidence showing the engine and gap-repair uploader can be disabled while existing observe-only telemetry and all production paths remain unchanged.
- [x] 10.8 Run `make check` and resolve every static, race, formatting, repository-boundary and secret-leak failure.
- [x] 10.9 Run `openspec validate add-generation-bound-network-reconciler --strict` and keep proposal, design, specs and tasks synchronized.
- [x] 10.10 Synchronize validated delta requirements into baseline specs only after synthetic implementation and qualification are complete; do not claim production mutation readiness.
