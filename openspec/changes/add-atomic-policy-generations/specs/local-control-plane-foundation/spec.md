## ADDED Requirements

### Requirement: Policy-authorized operator resume

`operator_resume` SHALL require an active compatible policy bundle that
predeclares the exact domain and target, a matching bundle generation, matching
domain policy and control-state generations, and a valid one-time action lease.
Resume SHALL remain a bounded lifecycle-state transition from `SAFE_MODE` to
`DEGRADED` and SHALL NOT directly restart, reconnect, change routes, access
credentials or mutate the production data plane.

#### Scenario: Matching resume is authorized

- **WHEN** the target is in `SAFE_MODE`, active policy authorizes `operator_resume`, all generations match and the one-time lease is valid
- **THEN** the owning daemon clears the exhausted budget and transitions the target to `DEGRADED`
- **AND** it records the lease as committed without applying a production mutation

#### Scenario: Resume uses a stale policy or state generation

- **WHEN** the request or lease carries a stale bundle, domain-policy or control-state generation
- **THEN** the owning daemon rejects the resume without changing target state
- **AND** the rejected lease cannot be replayed

#### Scenario: Policy domains are mismatched

- **WHEN** root and user daemons do not report the same active bundle generation
- **THEN** neither daemon accepts a new resume mutation
- **AND** observation, diagnostics and the existing Twilight data plane remain available

#### Scenario: Resume targets the wrong privilege domain

- **WHEN** a validly encoded resume request names a target not owned by the receiving daemon
- **THEN** the daemon rejects the request without forwarding it or exposing credentials
