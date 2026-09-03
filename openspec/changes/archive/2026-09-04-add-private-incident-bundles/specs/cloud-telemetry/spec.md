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

The worker SHALL assemble a bundle for every closed incident that has evidence
linked to it and has never been bundled, and SHALL NOT assemble one for an
incident whose bundle has already been removed at its recorded expiry. Removal
happens only at that expiry, and assembling again would restore what retention
took away; a bundle that is wanted after its expiry SHALL be requested
deliberately rather than restored by a pass.

Bundle storage SHALL be configured outside this repository and SHALL provide
private access with no public URL, an idempotent write when key and content
are identical, and a lifecycle ceiling no longer than the recorded expiry.
Where that storage is not configured, the worker SHALL create no bundle and
SHALL remain otherwise unchanged.

#### Scenario: An incident's evidence is assembled

- **WHEN** the worker assembles a bundle from events already linked to an incident
- **THEN** every record passes the strict event decoder before it is included
- **AND** an unknown schema, a malformed payload or unrestricted raw output is excluded rather than carried

#### Scenario: A closed incident has never been bundled

- **WHEN** the worker reaches a bundle pass and an incident is closed, has evidence linked to it, and has no bundle
- **THEN** a bundle is assembled for it
- **AND** the same incident is not selected again on the next pass

#### Scenario: A closed incident has nothing linked to it

- **WHEN** an incident is closed and no event is linked to it
- **THEN** the pass does not select it
- **AND** the pass records no failure for it, on this interval or any later one

#### Scenario: A closed incident's bundle was removed at its expiry

- **WHEN** an incident is closed and the bundle it once had was removed at its recorded expiry
- **THEN** the pass does not assemble a replacement
- **AND** retention remains the reason the object is gone

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
