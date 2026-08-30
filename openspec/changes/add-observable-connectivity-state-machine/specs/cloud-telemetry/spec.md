## ADDED Requirements

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
