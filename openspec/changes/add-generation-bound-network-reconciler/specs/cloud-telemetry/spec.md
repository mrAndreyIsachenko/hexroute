## ADDED Requirements

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
