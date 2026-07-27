## MODIFIED Requirements

### Requirement: Signed idempotent ingestion

The API SHALL validate versioned event schemas and Ed25519 request envelopes,
deduplicate event UUIDs and request IDs, track node sequence gaps and acknowledge
only durably accepted records. While cutover write freeze is active, the API
SHALL reject ingestion before persistence with HTTP 503, `write_frozen`, and
retry guidance.

#### Scenario: A batch is retried

- **WHEN** the API receives an already accepted signed batch
- **THEN** no duplicate event is created
- **AND** the prior acknowledgement can be returned safely

#### Scenario: A signature or request ID is invalid

- **WHEN** signature validation fails or a revoked/replayed identity is used
- **THEN** the batch is rejected without changing telemetry state

#### Scenario: A batch arrives during cutover freeze

- **WHEN** a client submits ingestion while write freeze is active
- **THEN** the API returns HTTP 503 with `write_frozen` and `Retry-After`
- **AND** no batch, event, sequence, node, or audit state changes

### Requirement: Explicit isolated runtime modes

The cloud image SHALL expose explicit API, worker and migrator modes with
separate database identities and least-privilege grants. Runtime identities
SHALL be able to read freeze state and invoke fixed write-gate functions but
SHALL NOT mutate cutover control state.

#### Scenario: API runtime starts

- **WHEN** the API component receives its runtime identity
- **THEN** it cannot assume worker, migrator or maintenance database privileges
- **AND** it cannot enable or clear write freeze

#### Scenario: Migration job runs repeatedly

- **WHEN** the same checksum-verified migration set and dashboard bootstrap are applied again
- **THEN** the operation remains idempotent

### Requirement: Actionable bounded alert delivery

The worker SHALL deliver deduplicated incident transitions through Telegram,
track transactional delivery state and support night suppression plus a morning
recovery digest. During write freeze, it SHALL remain alive without claiming,
delivering, or persisting alert work.

#### Scenario: The same incident transition is reconciled twice

- **WHEN** worker execution retries after an ambiguous delivery result
- **THEN** the persisted delivery identity prevents an unintended duplicate alert

#### Scenario: Alert cycle occurs during freeze

- **WHEN** the worker reaches an alert cycle while write freeze is active
- **THEN** it performs no Telegram delivery and no database mutation

### Requirement: Passkey-protected read-only dashboard

The dashboard SHALL require WebAuthn/passkey authentication and SHALL expose no
control endpoint capable of mutating local or provider infrastructure. During
write freeze, existing passkey assertions MAY create in-memory sessions without
persisting credential counters or authentication timestamps, while passkey
registration SHALL be rejected with HTTP 503 and `write_frozen`.

#### Scenario: Authenticated operator opens the dashboard

- **WHEN** a registered passkey assertion is valid
- **THEN** the operator can inspect current state, incidents and SLO evidence
- **AND** no restart, route or failover command is available

#### Scenario: Existing passkey logs in during freeze

- **WHEN** an existing credential assertion is cryptographically valid while frozen
- **THEN** the dashboard creates an in-memory authenticated session
- **AND** no credential counter, credential metadata, or last-authentication value is written

#### Scenario: Passkey registration is attempted during freeze

- **WHEN** an operator starts or finishes passkey registration while frozen
- **THEN** the API returns HTTP 503 with `write_frozen` and `Retry-After`
- **AND** no credential is stored

### Requirement: Dependency-aware readiness

Public readiness SHALL fail when required PostgreSQL or normal-mode worker
freshness gates fail, while liveness SHALL continue to represent the serving
process itself. During a valid bounded write freeze, readiness SHALL require a
readable valid control record and PostgreSQL, bypass worker freshness, and
report `status=ready` with `write_frozen=true`. Expired or malformed freeze
state SHALL fail readiness.

#### Scenario: Worker heartbeat becomes stale

- **WHEN** the worker freshness threshold is exceeded in normal mode
- **THEN** readiness fails
- **AND** liveness is evaluated independently

#### Scenario: Worker is quiescent during a valid freeze

- **WHEN** write freeze is active, its deadline has not passed, and PostgreSQL is available
- **THEN** readiness succeeds without a fresh worker heartbeat
- **AND** the response reports `write_frozen=true`

#### Scenario: Frozen deadline expires

- **WHEN** write freeze remains active after its deadline
- **THEN** readiness fails while writes remain blocked
- **AND** liveness is evaluated independently
