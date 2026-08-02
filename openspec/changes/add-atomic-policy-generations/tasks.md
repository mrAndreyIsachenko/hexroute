## 1. Policy Model And Canonical Format

- [x] 1.1 Add typed manifest, root/user payload, selector, authorization-lease, action-lease and policy-status models with strict bounds and enum validation.
- [x] 1.2 Add strict operator YAML decoding that rejects duplicate keys, anchors, aliases and unknown fields, with malformed-source regression fixtures.
- [x] 1.3 Add RFC 8785 canonical JSON and SHA-256 digest support with published conformance vectors and deterministic ordering tests.
- [x] 1.4 Define the compiled safety envelope, protected static fields and disjoint root/user namespaces, and test that dynamic sources cannot expand them.
- [x] 1.5 Add schema-version, compiler-identity, static-digest and downgrade compatibility validation for both daemon domains.

## 2. Composition And Conflict Validation

- [x] 2.1 Implement complete effective-snapshot composition with compiled-deny precedence, authorization-lease intersection and semantic duplicate elimination.
- [x] 2.2 Implement host, port and path overlap detection without specificity precedence and add wildcard/concrete conflict fixtures.
- [x] 2.3 Implement route/CIDR overlap, action-target and credential-reference conflict detection with cross-domain ownership tests.
- [x] 2.4 Reject the complete candidate on any conflict and emit only bounded conflict codes that contain no selector, endpoint, path or credential data.
- [x] 2.5 Add semantic no-op detection so equivalent effective content cannot advance bundle or domain generations.

## 3. Compiler, Diff, Replay And Signing

- [x] 3.1 Add the separate `cmd/hexroute-policy` binary with `compile`, `diff`, `replay`, `sign` and `rollback` subcommands and no daemon/runtime side effects.
- [x] 3.2 Emit one canonical manifest and separate canonical root/user payloads with bundle, parent and independently advancing domain generations.
- [x] 3.3 Implement semantic diff classification for newly allowed, newly denied and changed plans, with explicit authorization-expansion highlighting.
- [x] 3.4 Extend deterministic replay to evaluate candidate policy against synthetic safety fixtures and recent redacted observation traces offline.
- [x] 3.5 Block signing when conflict, compatibility, semantic no-op or replay safety gates fail, and bind the report digest to the candidate workflow.
- [x] 3.6 Add a macOS user-presence Keychain Ed25519 signer adapter, signer-fingerprint pinning and manual Touch ID integration evidence without logging private key material.
- [x] 3.7 Add signature, tampered-manifest, tampered-domain-payload, wrong-signer and expired-validity regression tests using synthetic keys only.

## 4. Immutable Domain Stores

- [x] 4.1 Add separate root and user policy stores with fixed paths, mode/owner checks, regular-file and no-symlink enforcement, and immutable generation filenames.
- [x] 4.2 Add atomic signed active-pointer, durable prepare-receipt and signed commit-intent persistence with crash-point tests around every rename and fsync boundary.
- [x] 4.3 Retain 16 resolved valid generations plus unresolved prepares, remove rejected payload bodies and enforce a 90-day bounded redacted audit index.
- [x] 4.4 Implement startup revalidation of signature, canonical digest, static binding, schema, validity and active-pointer consistency.
- [x] 4.5 Implement monotonic rollback as a newly compiled and signed generation and test that expired leases and revoked references are not revived.

## 5. Typed IPC And Cross-Domain Activation

- [x] 5.1 Extend the versioned bounded IPC protocol with typed policy status, `PreparePolicy`, `CommitPolicy` and `AbortPolicy` messages carrying no paths or payloads.
- [x] 5.2 Add peer-UID, transaction-ID, generation and digest validation tests for root and user sockets, including arbitrary-path and unknown-operation rejection.
- [x] 5.3 Integrate independent prepare verification and durable receipts into `hexrouted` and `hexroute-userd` without granting process, route or credential authority.
- [x] 5.4 Implement the `hexroutectl policy status|prepare|commit|abort|rollback` coordinator and require matching receipts before commit.
- [x] 5.5 Implement crash recovery that detects a one-domain commit, reports `domain_mismatch`, blocks mutations and converges the lagging domain forward after full revalidation.
- [x] 5.6 Add deterministic fault-injection tests for root failure, user failure and process termination before prepare, between prepares, between commits and after active-pointer replacement.

## 6. Suspension, Time And Existing-State Safety

- [x] 6.1 Add the local `authorization_suspended` overlay for corruption, signature/digest mismatch, domain mismatch, clock anomaly and IPC ownership violation.
- [x] 6.2 Verify suspension and invalid candidates preserve the last valid policy or enter observe-only `SAFE_MODE` without stopping AdGuard, Twilight, Pritunl, sing-box or established sessions.
- [ ] 6.3 Add UTC policy validity and clock-skew checks plus monotonic lease timing, boot-ID invalidation and sleep-counts-toward-TTL tests.
- [ ] 6.4 Add active-policy reboot revalidation and prove unfinished pre-reboot leases cannot resume.
- [ ] 6.5 Add `grandfathered_noncompliant` and `reconcile_by` state reporting with tests proving no implicit disconnect or hidden stop action exists.

## 7. Action Leases And Transaction Plans

- [ ] 7.1 Implement durable one-time action leases bound to bundle, domain-policy and control-state generations, exact target, plan digest, boot ID and nonce.
- [ ] 7.2 Reject replayed, expired, stale and boot-mismatched leases immediately before every step and commit, with durable committed/aborted/expired outcomes.
- [ ] 7.3 Add immutable ordered plan, verification and inverse-plan primitives that roll back only transaction-owned state and never foreign or ambiguous state.
- [ ] 7.4 Add failure tests proving stale mid-plan actions stop, verified owned steps roll back, and rollback failure moves only the target to `SAFE_MODE` with a critical incident.

## 8. Operator Resume Enforcement

- [ ] 8.1 Add regression tests for policy-authorized `operator_resume`, including exact domain/target matching, generation mismatch, domain mismatch and one-time lease replay.
- [ ] 8.2 Route the existing resume controller through shadow policy evaluation while preserving its current control-state generation guard and state-only behavior.
- [ ] 8.3 Prove the resume plan can only clear the target budget into `DEGRADED` and cannot invoke command, route, Pritunl, sing-box or credential code paths.
- [ ] 8.4 Enable active policy enforcement only for `operator_resume` after the documented shadow gate; leave every data-plane mutation capability disabled.

## 9. Redacted Status, Telemetry And Advisory Output

- [ ] 9.1 Extend local snapshots, `hexroutectl` status and bounded journals with allowlisted policy generations, digests, lifecycle states, timestamps and reason codes.
- [ ] 9.2 Add redacted policy telemetry schemas and secret-canary tests that reject selectors, endpoints, source paths, leases, credential references and credential values before persistence or upload.
- [ ] 9.3 Prove cloud API, PostgreSQL and worker loss cannot block local compile, prepare, commit, resume, suspension or safe-mode behavior and cannot request a local mutation.
- [ ] 9.4 Add a redacted advisor output format that can draft operator YAML changes but has no merge, signing, installation or activation path.
- [ ] 9.5 Add Git ignore, repository-boundary and secret-canary checks that reject live YAML, signed bundles, trust fingerprints, non-synthetic selectors and credential references from public Git.

## 10. Documentation And Shadow Qualification

- [ ] 10.1 Document the compiler/signing workflow, static installation boundary, typed activation transaction, status interpretation and monotonic rollback procedure.
- [ ] 10.2 Document the OpenShell architectural attribution and Hexroute's independent Go implementation and excluded L7/runtime dependencies.
- [ ] 10.3 Add a shadow qualification recorder for 72 eligible hours, two sleep/wake cycles, one reboot and the four mandatory injected failures.
- [ ] 10.4 Install the candidate beside Twilight using disjoint Hexroute labels, paths, sockets and stores, and capture evidence that normal and Twilight Codex paths remain available.
- [ ] 10.5 Capture rollback evidence showing that disabling `operator_resume` enforcement or activating a higher deny/rollback generation leaves Twilight and AdGuard unchanged.
- [ ] 10.6 Update the local operator, root/user observe and roadmap documentation only after the corresponding implementation and qualification evidence exists.

## 11. Verification And Spec Synchronization

- [ ] 11.1 Run focused unit, race, crash-recovery, replay, secret-canary and macOS integration tests for all affected packages.
- [ ] 11.2 Run `make check` and resolve every static, race, formatting and secret-leak failure.
- [ ] 11.3 Run `openspec validate add-atomic-policy-generations --strict` and keep proposal, design, specs and tasks synchronized with the implementation.
- [ ] 11.4 Sync the validated delta requirements into baseline specs only when implementation and shadow qualification for this change are complete.
