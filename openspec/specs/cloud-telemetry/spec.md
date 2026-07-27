# Cloud Telemetry Specification

## Purpose

Define the implemented telemetry-only Hexroute cloud service and its failure
isolation from local recovery.

## Requirements

### Requirement: Signed idempotent ingestion

The API SHALL validate versioned event schemas and Ed25519 request envelopes,
deduplicate event UUIDs and request IDs, track node sequence gaps and acknowledge
only durably accepted records.

#### Scenario: A batch is retried

- **WHEN** the API receives an already accepted signed batch
- **THEN** no duplicate event is created
- **AND** the prior acknowledgement can be returned safely

#### Scenario: A signature or request ID is invalid

- **WHEN** signature validation fails or a revoked/replayed identity is used
- **THEN** the batch is rejected without changing telemetry state

### Requirement: Explicit isolated runtime modes

The cloud image SHALL expose explicit API, worker and migrator modes with
separate database identities and least-privilege grants.

#### Scenario: API runtime starts

- **WHEN** the API component receives its runtime identity
- **THEN** it cannot assume worker, migrator or maintenance database privileges

#### Scenario: Migration job runs repeatedly

- **WHEN** the same checksum-verified migration set and dashboard bootstrap are applied again
- **THEN** the operation remains idempotent

### Requirement: Telemetry-only authority

The cloud service SHALL NOT expose an endpoint, event or configuration that can
request a local route, process, VPN or credential mutation.

#### Scenario: Cloud service is unavailable

- **WHEN** API, PostgreSQL or the worker cannot be reached
- **THEN** local observation and recovery policy continue independently
- **AND** bounded local journals retain eligible events for retry

### Requirement: Durable incident and retention model

The worker SHALL correlate sleep-aware heartbeat state, open and resolve durable
incidents, maintain configured retention and calculate eligible-interval SLOs.

#### Scenario: A sleeping node is silent

- **WHEN** a signed sleep interval covers the missing heartbeat window
- **THEN** the worker excludes that interval from false downtime and SLO penalties

#### Scenario: Detailed telemetry expires

- **WHEN** a record crosses its configured retention boundary
- **THEN** bounded detail is removed while required incident, deployment and aggregate history remains

### Requirement: Actionable bounded alert delivery

The worker SHALL deliver deduplicated incident transitions through Telegram,
track transactional delivery state and support night suppression plus a morning
recovery digest.

#### Scenario: The same incident transition is reconciled twice

- **WHEN** worker execution retries after an ambiguous delivery result
- **THEN** the persisted delivery identity prevents an unintended duplicate alert

### Requirement: Passkey-protected read-only dashboard

The dashboard SHALL require WebAuthn/passkey authentication and SHALL expose no
control endpoint capable of mutating local or provider infrastructure.

#### Scenario: Authenticated operator opens the dashboard

- **WHEN** a registered passkey assertion is valid
- **THEN** the operator can inspect current state, incidents and SLO evidence
- **AND** no restart, route or failover command is available

### Requirement: Dependency-aware readiness

Public readiness SHALL fail when required PostgreSQL or worker freshness gates
fail, while liveness SHALL continue to represent the serving process itself.

#### Scenario: Worker heartbeat becomes stale

- **WHEN** the worker freshness threshold is exceeded
- **THEN** readiness fails
- **AND** liveness is evaluated independently
