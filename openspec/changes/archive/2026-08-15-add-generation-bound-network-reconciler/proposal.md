## Why

Hexroute can compile atomic policy and is about to build a deterministic
observe-only connectivity state machine, but it does not yet define the guarded
boundary that turns a fresh reconciliation proposal into a recoverable action.
Without that boundary, later route, tunnel or user-access cutovers could regress
into imperative event handlers, opaque retries or blind process restarts.

## What Changes

- Add a generation-bound reconciliation engine that translates only a fresh,
  authorized proposal into an exact typed action plan under a one-time lease.
- Add a durable action lifecycle that separates authorization, claim, execution,
  verification, cancellation, compensation and reporting, with explicit
  terminal and retryable outcomes.
- Add typed acknowledgements (`accepted`, `temporarily_rejected`, `denied`) and
  stable reason/retry classes instead of ambiguous success/failure booleans.
- Require policy-defined stability thresholds, cooldown and action budgets before
  provisional observations can become action-ready state.
- Define reconnect-safe rehydration through current-versus-desired diffs and
  capability-specific tunnel, route, DNS, firewall, process and user-access
  plans; missing state is never repaired by an unscoped restart.
- Add explicit cancel/disconnect cleanup for temporary helpers, files and
  credential leases plus verified compensation of transaction-owned state.
- Add checkpointed operation-session envelopes for long-running local control
  workflows, binding manifest, policy, snapshot, child-action and attempt
  evidence before any resume, suspend, cancel, fail or replay-gated continuation.
- Carry a minimal typed provenance header across proposal, lease, execution
  attempt, step result, compensation, outcome and incident records without
  introducing a generic polymorphic evidence body.
- Extend telemetry acknowledgements with bounded missing-sequence ranges so a
  local uploader can replay retained evidence gaps independently of execution.

The first implementation is engine-only and uses synthetic capability adapters.
Production route, DNS, tunnel, firewall, process, Pritunl, Keychain and OTP
mutation adapters, ownership cutover and automatic failover are non-goals and
require separate OpenSpec changes. Arbitrary shell commands, cloud-requested
actions, implicit transport fallback, AdGuard changes and mutation of Twilight
state are also non-goals.

This change belongs in the public Hexroute repository because it defines
provider-neutral action contracts, state machines, synthetic adapters, replay
fixtures and redacted telemetry schemas. Private `hexroute-infra` owns any later
live deployment and evidence. Twilight remains the sole production owner, and
the cloud remains telemetry-only, throughout this change.

Rollout begins only after the atomic-policy and observable-connectivity
prerequisites are qualified. It proceeds through deterministic simulation,
crash/fault replay and local shadow comparison with every production adapter
absent. Rollback disables the engine and gap-repair uploader while retaining the
existing observe-only state machine and telemetry retry path; no network inverse
is needed because this change performs no production mutation.

## Capabilities

### New Capabilities

- `generation-bound-network-reconciler`: Defines action-readiness gating, exact
  proposal-to-plan translation, durable execution lifecycle, typed
  acknowledgements, reconnect-safe diff/rehydration, cancel-safe compensation
  and end-to-end provenance for capability-scoped reconciliation.

### Modified Capabilities

- `local-control-plane-foundation`: Requires the pre-cutover reconciliation
  engine to remain synthetic and non-authoritative while preserving root/user
  ownership, typed IPC and the absence of production mutation adapters.
- `cloud-telemetry`: Adds bounded acknowledgement-driven sequence-gap repair for
  retained event and action-evidence uploads without granting cloud control over
  local reduction or execution.

## Impact

- Adds provider-neutral Go packages for action state, readiness evaluation,
  proposal translation, operation-session checkpointing, execution
  coordination, compensation and replay.
- Extends strict local schemas and journals with action attempts, typed outcomes,
  operation-session checkpoints, provenance links and bounded cancellation
  state; credential values remain prohibited.
- Adds only synthetic capability adapters and capability-leak tests. No package
  may import production route, DNS, process, VPN or Keychain mutation paths.
- Extends signed ingestion acknowledgements and the local uploader with bounded
  missing-range replay while preserving idempotency and local independence.
- Depends on completed atomic-policy and observable-connectivity qualification,
  but does not modify their active generations or completion evidence.
- Does not change live providers, macOS launchd ownership, Twilight, AdGuard,
  Pritunl, sing-box, routes, DNS or either Codex access path.
