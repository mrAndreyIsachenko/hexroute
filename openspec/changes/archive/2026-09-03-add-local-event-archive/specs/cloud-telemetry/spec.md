## MODIFIED Requirements

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
