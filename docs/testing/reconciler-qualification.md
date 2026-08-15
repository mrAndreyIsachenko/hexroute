# Generation-Bound Reconciler Qualification

This document defines the synthetic-only qualification gate for the
generation-bound network reconciler. It is evidence for the engine boundary; it
does not enable production adapters, live shadow cutover or mutation authority.

The first rollout remains synthetic. Twilight stays the production owner of live
networking, AdGuard, Pritunl, sing-box, routes, DNS and both Codex access paths.

## Gate

Run the gate before enabling the synthetic runtime feature flag:

```sh
make check
```

`make check` includes these reconciler-relevant suites:

- `make test`: unit, schema-boundary, crash-recovery, replay and IPC package
  tests.
- `make race`: race coverage for action lifecycle, cancellation, compensation
  and operation-session transitions.
- `make fuzz`: fuzz-smoke compilation across all Go packages. Today this is
  `go test ./... -run '^$'`; when explicit `Fuzz*` entrypoints are added, this
  target is the stable place to run them.
- `bash tests/reconciler_shadow_integration_test.sh`: disabled-gate IPC,
  peer-authentication, cloud-loss and capability-leak checks.
- `make secret-test`: secret-canary and repository-boundary checks.
- `make spec-check`: strict OpenSpec validation for all active and baseline
  requirements.

The gate is not satisfied by a single package test. It must cover unit, race,
fuzz-smoke, crash-recovery, replay, IPC, schema-boundary, secret-canary and
capability-leak evidence in one `make check` run.

## Interpretation

Passing qualification means deterministic synthetic traces replay to the same
canonical sessions, checkpoints, plans, transitions, outcomes and provenance.
It also means repository checks did not find production mutation dependencies in
the reconciler or synthetic adapter surface.

Passing qualification does not grant production route, DNS, firewall, process,
tunnel, Pritunl, Keychain, OTP or credential authority. Every real adapter, live
shadow qualification and ownership cutover requires a separate grill session,
OpenSpec change and independently executable rollback.

## Operator Semantics

Readiness is not raw health. Raw component observations remain visible as
observed facts, while action readiness is derived from a fresh canonical
snapshot, exact policy and control generations, boot identity, source
watermarks, stability thresholds, action budget, backoff and cooldown. The only
readiness classes are `ready`, `temporarily_blocked` and `denied`.

Acknowledgements are not generic success booleans. `accepted` means either a
semantic no-op was proven or the exact action was durably accepted. It does not
mean an adapter has already executed. `temporarily_rejected` means the same
policy could allow the action later after freshness, threshold, budget, backoff
or cooldown clears. `denied` means schema, policy, ownership, generation,
target, capability or provenance validation failed; replaying the same request
identity cannot turn it into an accepted action.

Crash and cancellation recovery are explicit. A claimed attempt is never rerun
by a different process, boot or attempt. Startup must observe target state and
prove the recovery path. Cancellation prevents the next unstarted step; if an
owned prefix was applied, only the verified transaction-owned inverse may run.
Foreign, ambiguous or changed state is not purged, adopted or restarted.

Telemetry gap repair is upload-only. A signed ingestion acknowledgement may ask
for bounded retained sequence ranges, and the uploader may replay only exact
retained immutable records. If evidence has expired, it emits one redacted
`telemetry_gap_unrecoverable` diagnostic and continues newer uploads. Gap
repair never feeds readiness, policy, reduction, lease issuance, execution,
verification, compensation or recovery.

## Future Capability Rule

This repository contains no production mutation adapter for the reconciler.
Adding a route, DNS, firewall, process, tunnel, Pritunl, Keychain, OTP or
credential adapter requires its own grill session, OpenSpec change, safety
envelope, fault matrix, live shadow qualification, guarded cutover and rollback.

The same rule applies to ownership transfer. A future live shadow qualification
or root/user ownership cutover cannot be bundled into this synthetic engine
change merely because the engine, replay harness or telemetry projection exists.

## Rollback Evidence

Rollback is disablement, not network inversion:

- The synthetic engine is disabled by leaving `FeatureGate{}` or the equivalent
  runtime feature flag unset. `TestStartupSurfaceRequiresExplicitSyntheticFeatureGate`
  proves proposal translation, execution IPC and synthetic capability IDs remain
  absent.
- Synthetic shadow comparison and replay are separately gated.
  `TestStartupSurfaceKeepsShadowComparisonAndReplayBehindFeatureGates` proves
  replay cannot surface without the comparison gate.
- The gap-repair uploader is disabled with `WithGapRepairEnabled(false)`.
  `TestUploaderCanDisableGapRepairWithoutDisablingBaseUpload` proves normal
  telemetry upload and acknowledgement still run while missing-sequence replay
  and unrecoverable-gap diagnostics are skipped.
- `TestShadowCloudLossAndEngineFailureDoNotMutateProtectedState` proves cloud
  loss or engine failure leaves observe-only state, Twilight, AdGuard, Pritunl,
  sing-box, routes, DNS and both Codex paths unchanged.

Because this change has no production adapter and no live mutation authority,
rollback does not stop, restart or reconfigure production networking. Existing
observe-only telemetry and production paths continue under their current owners.
