## ADDED Requirements

### Requirement: Durable private incident evidence bundles

The worker MAY assemble a durable evidence bundle for an incident from events
already linked to it. A bundle SHALL contain only records that pass the strict
event decoder, SHALL be bounded in both record count and encoded size, and
SHALL be addressed by the digest of its complete encoded content so that the
same incident snapshot reuses its stored row rather than producing a second
object. A bundle SHALL carry a recorded expiry, and expiry SHALL remove the
stored object as well as the row that names it.

A bundle SHALL NOT be readable as input to anything. It is redacted evidence a
person reads, and no host, reducer, policy generation, action lease or
mutation may be derived from one.

Bundle storage SHALL be configured outside this repository and SHALL provide
private access with no public URL, an idempotent write when key and content
are identical, and a lifecycle ceiling no longer than the recorded expiry.
Where that storage is not configured, the worker SHALL create no bundle and
SHALL remain otherwise unchanged.

#### Scenario: An incident's evidence is assembled

- **WHEN** the worker assembles a bundle from events already linked to an incident
- **THEN** every record passes the strict event decoder before it is included
- **AND** an unknown schema, a malformed payload or unrestricted raw output is excluded rather than carried

#### Scenario: The same incident snapshot is bundled twice

- **WHEN** an assembly produces content identical to a bundle already retained
- **THEN** the stored row is reused and no second object is written

#### Scenario: A bundle reaches its recorded expiry

- **WHEN** a retained bundle crosses the expiry recorded with it
- **THEN** both the stored object and the row naming it are removed
- **AND** a later request may repopulate the row and starts a new expiry from that point

#### Scenario: Bundle storage is not configured

- **WHEN** the worker reaches a bundle pass and no bundle storage is configured
- **THEN** it creates no bundle, records that the pass was not attempted, and continues its other work unchanged

#### Scenario: A bundle is offered back to the control plane

- **WHEN** any path attempts to read a bundle as input to a reduction, a policy decision, an action lease or a mutation
- **THEN** it is refused
- **AND** the refusal does not depend on the bundle's content
