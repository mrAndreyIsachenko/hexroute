# Local Control Plane Foundation Specification

## Purpose

Define the implemented pre-cutover Hexroute runtime without granting it
production route, process or Pritunl mutation authority.

## Requirements

### Requirement: Disjoint observe-only runtime

Hexroute SHALL install and execute pre-cutover local components under paths,
labels, sockets, state and logs that are disjoint from the active Twilight
runtime, and observe-only execution SHALL NOT mutate production state.

#### Scenario: Observe-only runtime evaluates host state

- **WHEN** `hexrouted` or `hexroute-userd` runs before active cutover
- **THEN** it may publish observations and proposed actions
- **AND** it does not apply routes, restart production processes or submit Pritunl credentials

### Requirement: Deterministic bounded policy

The local policy engine SHALL use typed lifecycle states, monotonic generations,
consecutive thresholds, action budgets, exponential backoff, verification
windows and cooldowns.

#### Scenario: Isolated observation fails

- **WHEN** one observation fails below its configured threshold
- **THEN** the lifecycle does not issue a restart action
- **AND** the persisted generation remains monotonic

#### Scenario: Action budget is exhausted

- **WHEN** a recovery target consumes its configured budget
- **THEN** further automatic actions stop for the cooldown interval
- **AND** the condition remains visible to the operator

### Requirement: Authenticated typed local IPC

Local daemons SHALL expose only versioned allowlisted requests with bounded
payloads and SHALL authenticate the peer UID before accepting a request.
Root IPC SHALL recreate its fixed volatile socket parent on daemon start and
SHALL reject group- or world-writable socket parents before binding.

#### Scenario: Unauthorized request reaches a socket

- **WHEN** a peer with the wrong UID sends a validly encoded request
- **THEN** the daemon rejects it without executing an action

#### Scenario: Mac reboots before root observe starts

- **WHEN** macOS removes `/var/run/hexroute-observe` during reboot
- **THEN** `hexrouted` recreates the root-owned non-writable socket parent before binding
- **AND** the typed root status socket becomes available without manual directory repair

#### Scenario: Socket parent is unsafe

- **WHEN** the root socket parent is group- or world-writable
- **THEN** `hexrouted` rejects startup instead of binding a privileged local IPC socket

#### Scenario: Arbitrary command is requested

- **WHEN** a request does not match an allowlisted typed operation
- **THEN** the daemon rejects it as unsupported

### Requirement: Bounded redacted local journal

Root and user components SHALL maintain separate crash-safe priority journals,
preserve critical events ahead of diagnostics and never serialize protected
credential classes.

#### Scenario: Journal reaches its size bound

- **WHEN** new records would exceed the configured capacity
- **THEN** diagnostic records are evicted before critical transitions or incidents
- **AND** overflow remains observable

#### Scenario: Secret canary reaches serialization

- **WHEN** a protected credential canary appears in an event candidate
- **THEN** serialization or its test gate fails before persistence or upload

### Requirement: Privilege-separated recovery planning

Root observations and user Keychain/Pritunl observations SHALL remain separated,
and credential values SHALL NOT cross IPC, logs, telemetry or diagnostics.

#### Scenario: User runtime plans a reconnect

- **WHEN** outer readiness, full wake, OTP timing and policy thresholds permit a reconnect proposal
- **THEN** the proposal contains no PIN, TOTP seed or generated OTP
- **AND** observe-only mode performs no reconnect

### Requirement: Evidence-gated sentinel

The sentinel SHALL require both stale root heartbeat and failed independent
legacy data-path evidence before one bounded Hexroute-daemon recovery action,
and SHALL have no authority over routes, Pritunl, AdGuard or VPS services.

#### Scenario: Only the heartbeat is stale

- **WHEN** the legacy data path remains healthy
- **THEN** the sentinel does not restart the root daemon

#### Scenario: Both evidence sources fail

- **WHEN** root heartbeat and independent data-path evidence fail their gates
- **THEN** the sentinel permits at most one bounded daemon recovery attempt
- **AND** enters a long cooldown after verification
