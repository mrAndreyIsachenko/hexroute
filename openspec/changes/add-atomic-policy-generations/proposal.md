## Why

Hexroute already has generation-guarded lifecycle state and privilege-separated
local daemons, but policy is loaded independently at process start and is not an
atomically activated, signed authorization snapshot. Before Hexroute gains any
production recovery authority, it needs a fail-closed policy-generation layer
that prevents partial, ambiguous, stale or replayed authorization from turning
one intended change into unrelated local mutations.

## What Changes

- Add a strict operator-policy compiler that composes all declarative sources
  into one canonical, signed effective snapshot with a global bundle generation
  and independent root and user domain generations.
- Reject conflicting selectors, unsupported schemas, semantic no-ops, invalid
  signatures, clock anomalies and policies outside the compiled safety envelope
  before activation; keep the last valid generation active.
- Add immutable generation storage and an explicit typed
  prepare/commit/abort/rollback transaction across the root and user daemons.
- Bind every authorized mutation to a short-lived, one-time action lease carrying
  the policy and state generations plus the exact action-plan digest.
- Add mandatory semantic diff and deterministic offline replay against synthetic
  invariants and recent redacted observations before a policy can be signed.
- Add redacted local and cloud-visible policy status without exposing selectors,
  endpoints, source paths, leases or credential references.
- Enforce a repository boundary in which public Hexroute contains only policy
  contracts, schemas, synthetic examples and tests; live policy sources, signed
  bundles, trust fingerprints and activation evidence remain operational
  artifacts outside public Git.
- Authorize only the existing `operator_resume` state transition in the first
  active rollout; it still cannot restart a process, reconnect Pritunl, change a
  route, alter sing-box or access a credential.
- Keep OpenShell as an attributed architectural reference only; Hexroute does not
  depend on the OpenShell runtime, gateway, OPA or Rust crates.

Non-goals are production tunnel or Pritunl cutover, route/process/credential
mutation, L7/GraphQL/JSON-RPC/MCP policy, provider deployment and cloud control
authority. Rollout begins in shadow validation and must pass a 72-hour gate,
sleep/wake and reboot cycles, and specified fault injection before
`operator_resume` enforcement is enabled. Rollback is a new, monotonically
increasing signed generation derived from previously valid content; it never
decrements counters or revives expired leases or revoked credentials.

This change belongs in the public Hexroute repository. Private infrastructure
may later deploy binaries and collect private evidence, while the legacy
Twilight repository remains the immutable production owner throughout this
change.

## Capabilities

### New Capabilities

- `atomic-policy-generations`: Defines source composition, conflict rejection,
  canonical signed policy bundles, domain-aware activation, one-time action
  leases, replay gates, immutable storage and redacted status.

### Modified Capabilities

- `local-control-plane-foundation`: Makes `operator_resume` authorization depend
  on an active matching policy generation and action lease while preserving the
  pre-cutover observe-only production boundary.

## Impact

- Adds a separate `hexroute-policy` operator binary and policy model, compiler,
  validator, replay, signing and storage packages in public Go code.
- Extends versioned root/user IPC and `hexroutectl` with typed policy lifecycle
  operations that carry generations and digests, never arbitrary paths or
  policy payloads.
- Extends root/user daemon state and journals with prepared/active/rejected,
  restart-required, mismatch, suspension and action-lease metadata.
- Adds synthetic conflict, replay, crash-recovery, sleep/wake and reboot fixtures
  plus bounded, secret-redacted telemetry schemas.
- Adds ignore and secret-canary gates that reject live policy material from the
  public source tree; private infrastructure may retain only redacted digests and
  deployment references while secret values stay in their designated stores.
- Does not change live provider resources, DNS, AdGuard, Twilight, Pritunl,
  sing-box, routes or current cloud mutation authority.
