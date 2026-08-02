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
