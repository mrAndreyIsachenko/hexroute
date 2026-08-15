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

### Requirement: Bounded acknowledgement-driven sequence gap repair

Signed ingestion acknowledgements SHALL bind node identity, request identity and
the durably accepted high-watermark and MAY include a bounded sorted set of
missing node-sequence ranges. The local uploader SHALL replay only exact retained
immutable records for those ranges and SHALL NOT synthesize records, renumber
events or expose the acknowledgement to policy, reduction or action packages.

#### Scenario: Server reports a retained missing range

- **WHEN** an authenticated acknowledgement names a bounded missing sequence range still present in the local journal
- **THEN** the uploader resends the exact signed records idempotently
- **AND** local connectivity and action state remain unchanged

#### Scenario: Gap request exceeds its bounds

- **WHEN** an acknowledgement contains too many ranges, an oversized range, an invalid order, another node identity or an unrelated request binding
- **THEN** the uploader rejects the gap request
- **AND** it performs no unbounded scan or local control operation

#### Scenario: Requested evidence has expired locally

- **WHEN** retention no longer contains all records in a valid requested range
- **THEN** the uploader emits one bounded redacted `telemetry_gap_unrecoverable` record after the gap
- **AND** it leaves the server-side gap visible while allowing newer telemetry uploads

### Requirement: Action evidence remains redacted and non-authoritative

Cloud projection of reconciliation evidence SHALL contain only allowlisted
record class, lifecycle/outcome, bounded reason, retry class, generation,
freshness bucket and canonical redacted correlation identifiers. It SHALL omit
commands, arguments, topology, endpoints, selectors, paths, process output,
session identity, credential references and values. Cloud storage and delivery
state SHALL NOT authorize or alter a local action.

#### Scenario: Action outcome is uploaded

- **WHEN** the local uploader projects a committed, denied, cancelled, rolled-back, failed or safe-mode outcome
- **THEN** the cloud receives only the bounded redacted projection
- **AND** acknowledgement affects report delivery state only

#### Scenario: Cloud sends an action-like field

- **WHEN** an ingestion response contains a command, capability request, target, policy override or local callback
- **THEN** strict response decoding rejects it
- **AND** no local readiness, lease, execution or recovery state changes
