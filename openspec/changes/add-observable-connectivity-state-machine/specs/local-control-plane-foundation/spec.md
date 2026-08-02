## ADDED Requirements

### Requirement: Privilege-separated normalized observation flow

Root and user observe-only runtimes SHALL publish only source-owned complete
connectivity facts. The root runtime SHALL sequence the aggregate snapshot and
the user runtime SHALL send only a bounded credential-free projection through
authenticated typed IPC. Missing user IPC SHALL make user-owned components
stale or unknown and SHALL NOT grant root authority to inspect Keychain values,
generate OTPs, submit Pritunl credentials or request a user action.

#### Scenario: User runtime publishes Pritunl state

- **WHEN** the authenticated user daemon reports a complete Pritunl component fact
- **THEN** root can include its lifecycle, freshness and bounded reason in the aggregate snapshot
- **AND** no PIN, TOTP seed, generated OTP, Keychain reference or session secret crosses IPC

#### Scenario: User runtime is unavailable

- **WHEN** the user fact stream exceeds its freshness deadline
- **THEN** root marks user-owned components stale or unknown
- **AND** it does not impersonate the user daemon or attempt a reconnect

### Requirement: Non-executable observe-only reconciliation output

The local control plane MAY publish normalized snapshots, desired-state diffs
and generation-bound reconciliation proposals, but it SHALL expose no IPC,
command or callback that executes a proposal. Existing component observations,
Twilight production processes, AdGuard and normal and fallback Codex paths SHALL
remain unchanged throughout rollout and rollback.

#### Scenario: Root observes a route divergence

- **WHEN** the normalized diff reports a missing, unexpected or divergent route
- **THEN** the runtime records an observe-only proposal
- **AND** it does not add, delete or replace any route

#### Scenario: State-machine integration is disabled

- **WHEN** the new reducer and aggregate status path are rolled back
- **THEN** existing component observe-only paths continue operating
- **AND** no network inverse action or Twilight restart is required
