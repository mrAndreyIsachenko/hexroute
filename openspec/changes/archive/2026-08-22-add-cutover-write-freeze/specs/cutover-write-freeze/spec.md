## ADDED Requirements

### Requirement: Singleton cutover control state

PostgreSQL SHALL store one copyable cutover-control record containing the
cutover identifier, freeze state, freeze timestamp, and deadline. Runtime roles
SHALL NOT be able to mutate this record directly.

#### Scenario: Normal runtime reads control state

- **WHEN** an API or worker runtime evaluates cutover state
- **THEN** it can read the singleton record
- **AND** it cannot change the record with its runtime database identity

#### Scenario: Database is copied to green

- **WHEN** the source database is copied after freeze
- **THEN** the same cutover identifier, timestamps, and frozen state are present on green

### Requirement: Transactional write exclusion

Every runtime `INSERT`, `UPDATE`, and `DELETE` transaction SHALL acquire a
shared lock on the singleton control record before changing application state.
Enabling freeze SHALL acquire the conflicting lock and SHALL return only after
all earlier runtime write transactions have finished.

#### Scenario: Freeze waits for an in-flight write

- **WHEN** an earlier runtime transaction holds the shared cutover lock
- **THEN** the freeze transaction waits for that transaction to commit or roll back
- **AND** no later runtime write can commit after freeze succeeds

#### Scenario: Runtime write starts while frozen

- **WHEN** a runtime attempts to mutate protected state while freeze is active
- **THEN** PostgreSQL rejects the transaction with the stable `write_frozen` condition
- **AND** no partial application write is committed

### Requirement: Freeze fails closed

An active freeze SHALL remain active across process restarts and after its
deadline. Deadline expiry SHALL NOT automatically restore writes.

#### Scenario: Freeze deadline expires

- **WHEN** the freeze deadline passes before cutover completion or abort
- **THEN** runtime writes remain blocked
- **AND** public readiness reports not ready

#### Scenario: Pre-cutover abort is authorized

- **WHEN** private infrastructure proves the public edge still targets old and explicitly aborts
- **THEN** the migrator identity can clear the old source freeze
- **AND** no public HTTP endpoint participates in thawing

### Requirement: Freeze state is redacted

Runtime responses and logs SHALL expose only the boolean freeze state and a
stable public error code. They SHALL NOT expose cutover identifiers, database
credentials, provider identities, or private endpoints.

#### Scenario: Frozen request is rejected

- **WHEN** an external client receives a frozen-write response
- **THEN** the response contains `write_frozen` and retry guidance
- **AND** it contains no secret or private cutover evidence

### Requirement: Production ownership remains isolated

The public application SHALL only enforce provider-neutral write exclusion.
Live freeze/thaw commands, edge checks, data copy, DNS mutation, and evidence
SHALL remain in private infrastructure, while Twilight continues to own local
production recovery until its separate cutover completes.

#### Scenario: Cloud cutover fails

- **WHEN** old-to-green activation is aborted or unavailable
- **THEN** no Hexroute cloud path changes AdGuard, Twilight, local routes, VPN processes, or local credentials
- **AND** local recovery remains independently available
