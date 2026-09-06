## ADDED Requirements

### Requirement: Delivery does not interpret what it delivers

The configuration version format, its verification and the ingress agent SHALL
treat a version's content as opaque bytes. They SHALL NOT parse it, validate it
against a runtime's schema, or behave differently according to what is inside
it.

This is what makes the path usable by a runtime it was not written for. Code
that reads inside a delivered configuration belongs where that runtime's
parameters belong, which is not this repository.

#### Scenario: A version carries a configuration for an unfamiliar runtime

- **WHEN** a version's content is a configuration this repository knows nothing about
- **THEN** it is signed, delivered, verified, applied and returned from exactly as any other

#### Scenario: Content is inspected

- **WHEN** the delivery path is examined
- **THEN** nothing in it decodes a version's content beyond the digest that binds it

### Requirement: An operator can recover the content of a published version

An operator SHALL be able to verify a published version and obtain the exact
bytes it carries, under the same checks a host performs: the signature against
the pinned operator public key and the digest against the content.

The emitted content SHALL NOT be written into this repository, and the operation
SHALL require no network and no database, so that the ability to read a version
is not also the ability to publish one.

#### Scenario: A published version is recovered

- **WHEN** an operator verifies a published version
- **THEN** the exact content the host will run is emitted
- **AND** it is emitted unparsed

#### Scenario: A version fails verification

- **WHEN** the signature, the signing key or the digest does not match
- **THEN** nothing is emitted, and the check that failed is named

#### Scenario: Recovery is attempted with publishing authority absent

- **WHEN** the recovering operator has no store credential and no database
- **THEN** the recovery still succeeds, because it needs neither
