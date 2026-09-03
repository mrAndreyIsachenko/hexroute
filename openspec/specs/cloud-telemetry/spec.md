# Cloud Telemetry Specification

## Purpose

Define the implemented telemetry-only Hexroute cloud service and its failure
isolation from local recovery.

## Requirements

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

### Requirement: Bounded acknowledgement-driven sequence gap repair

Signed ingestion acknowledgements SHALL bind node identity, request identity and
the durably accepted high-watermark and MAY include a bounded sorted set of
missing node-sequence ranges. The local uploader SHALL replay only exact retained
immutable records for those ranges from the upload journal, SHALL NOT read the
local event archive as a source for replay, and SHALL NOT synthesize records,
renumber events or expose the acknowledgement to policy, reduction or action
packages.

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

#### Scenario: The archive still holds a record the journal has dropped

- **WHEN** a valid requested range is absent from the upload journal but present in the local archive
- **THEN** the uploader still reports the gap as unrecoverable
- **AND** it does not upload the archived copy, because archiving a record is not a decision to send it

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

### Requirement: Redacted one-way connectivity projection

The local runtime MAY upload a signed bounded projection of the normalized
connectivity snapshot containing only schema and snapshot generation, policy
generations, aggregate and component-class states, freshness buckets, bounded
reason codes, gap/conflict counts and proposal classes/counts. The projection
SHALL exclude IP addresses, hostnames, route prefixes, selectors, endpoints,
source paths, process details, event UUIDs, session identifiers, proposal
digests, credential references and credential values. Cloud data SHALL NOT be
read as reducer input or converted into a local action request.

#### Scenario: Connectivity snapshot is prepared for upload

- **WHEN** the local telemetry adapter projects a normalized snapshot
- **THEN** only allowlisted aggregate fields are serialized and signed
- **AND** secret and private-topology canaries fail serialization before persistence or upload

#### Scenario: Cloud projection disagrees with current local state

- **WHEN** delayed cloud data carries an older snapshot or policy generation
- **THEN** local reduction and proposals continue from local accepted facts only
- **AND** the cloud cannot roll back, replace or reconcile local state

#### Scenario: Cloud service is unavailable

- **WHEN** ingestion, PostgreSQL or workers cannot accept the projection
- **THEN** eligible redacted events remain in bounded local storage for retry
- **AND** local observation, reduction and existing recovery continue independently

### Requirement: Stored connectivity read model without control authority

The cloud MAY persist the latest redacted connectivity projection per node and
render it read-only. Persistence SHALL be ordered by the host's own event
position, so a projection describing an earlier position SHALL NOT replace a
stored later one, and re-processing an already consumed projection SHALL change
nothing. The stored schema SHALL constrain every text column to the bounded
token alphabet the projection allows. No stored column, endpoint, rendering
path or exported operation SHALL be readable by a host, and the dashboard role
SHALL hold no write grant on the read model.

#### Scenario: A projection arrives after a newer one

- **WHEN** a projection describing an earlier host event position is processed
- **THEN** the stored read model keeps the later projection unchanged
- **AND** the outcome is not reported as an ingestion failure

#### Scenario: The same projection is processed twice

- **WHEN** a projection already folded into the read model is offered again
- **THEN** nothing is written and no stored value changes

#### Scenario: A host restarts its snapshot generation

- **WHEN** a projection at a later host position carries a lower snapshot generation
- **THEN** the later projection is stored
- **AND** the generation regression is recorded as a lineage reset rather than
  reconciled away

#### Scenario: The cloud is lost while a host keeps observing

- **WHEN** ingestion, PostgreSQL, the worker or the dashboard is unavailable
- **THEN** local reduction continues to advance its snapshot generation and
  produce proposals from local accepted facts alone
- **AND** the undeliverable projection waits in bounded local storage at a
  priority below retained evidence
