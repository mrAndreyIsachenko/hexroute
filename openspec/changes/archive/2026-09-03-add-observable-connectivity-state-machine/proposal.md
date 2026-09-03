## Why

Hexroute currently observes and plans each connectivity component through
separate local models, so it cannot reconstruct one causal, generation-bound
view of physical connectivity, DNS, routes, transports, relays and user access.
Before any production mutation authority is introduced, those observations must
feed a deterministic observable state machine instead of allowing future event
handlers to mutate the network directly.

## What Changes

- Add a typed, normalized connectivity snapshot that keeps physical network,
  default path, DNS, scoped routes, managed transports, relay/ingress health,
  Pritunl/user access and session-expiry state separately inspectable while also
  deriving an operator summary.
- Add source-owned, ordered connectivity facts and a bounded crash-safe local
  event journal with deterministic checkpoint-and-replay behavior across
  daemon restart, reboot and sleep/wake.
- Add a pure reducer that combines the prior snapshot, ordered facts and the
  exact active policy generation into a new snapshot, desired state and typed
  reconciliation diff.
- Detect duplicate, stale, out-of-order, gapped and cross-owner observations
  explicitly; never resolve privilege-domain conflicts by last writer wins.
- Produce immutable, generation-bound proposed plans for later authorization,
  but keep the entire first rollout observe-only: no route, DNS, process,
  tunnel, Pritunl, AdGuard or credential mutation is permitted.
- Publish only a bounded redacted projection to existing local status and cloud
  telemetry. Cloud input cannot participate in reduction or request a local
  action.
- Qualify determinism and convergence with replay, restart, reboot, sleep/wake,
  event reordering, collector loss and stale-policy fault tests before a later
  change may introduce a privileged executor.

Non-goals are active reconciliation, tunnel ownership cutover, packet or
session payload capture, credential handling, provider deployment, changing
Twilight or AdGuard, and accepting control input from the cloud. Firezone's
policy-driven event loop and NetBird's normalized connectivity overview are
architectural references only; Hexroute remains an independent Go
implementation with stricter policy-generation and privilege boundaries.

This change belongs in the public Hexroute repository because it defines
provider-neutral models, reducers, synthetic fixtures and redacted schemas.
Private `hexroute-infra` may later deploy the resulting observe-only binaries
and retain private qualification evidence. Twilight remains the sole production
owner throughout this change.

Rollout starts with offline replay, then local shadow recording beside Twilight.
Rollback disables the new reducer/read model and returns to the existing
component observations; because this change has no executor, rollback does not
alter production connectivity or require reversing network state.

## Capabilities

### New Capabilities

- `observable-connectivity-state-machine`: Defines typed connectivity facts,
  normalized snapshots, deterministic reduction, desired-state diffs,
  checkpoint/replay, ownership conflict handling and observe-only plan output.

### Modified Capabilities

- `local-control-plane-foundation`: Requires root and user observe-only runtimes
  to publish source-owned facts into the normalized state machine without
  gaining production mutation authority.
- `cloud-telemetry`: Allows only a bounded redacted connectivity projection to
  leave the host and preserves the cloud's telemetry-only authority.

## Impact

- Adds provider-neutral Go models and reducer/replay packages plus synthetic
  event traces and rendering contracts.
- Extends typed local IPC and status output with bounded connectivity facts,
  snapshot generations, freshness, conflicts and proposed diffs; it adds no
  arbitrary command or mutation operation.
- Reuses the existing crash-safe local journals and active policy-generation
  contracts, with explicit adapters rather than replacing component lifecycle
  state machines.
- Extends telemetry schemas with a redacted connectivity projection that omits
  addresses, hostnames, selectors, route prefixes, process details, credential
  references and session identifiers.
- Does not change live provider resources, DNS, routes, Twilight, AdGuard,
  Pritunl, sing-box or current production ownership.
