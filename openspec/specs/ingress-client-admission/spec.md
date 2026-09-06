# Ingress Client Admission Specification

## Purpose

Record who reaches an ingress and what for, so that admitting one more is a
decision with a stated meaning rather than an edit, and so that removing one is
an act with a known effect.

## Requirements

### Requirement: Every client has a recorded identity and purpose

Each client admitted to an ingress SHALL have an identity of its own and a
recorded purpose, before it is admitted. Two clients SHALL NOT share an
identity, so that losing one is answered by removing one.

The record SHALL name no live credential, address or transport parameter: what
is recorded is that a client exists, what it is for, and what it may reach.

#### Scenario: A client is admitted

- **WHEN** a client is admitted to an ingress
- **THEN** its purpose and its own identity are recorded before the version admitting it is published

#### Scenario: Two clients are proposed with one identity

- **WHEN** a second client would use an identity an existing client already holds
- **THEN** it is refused, because removing either would remove both

#### Scenario: A client record is read

- **WHEN** the client record is read
- **THEN** it states what each client is for
- **AND** it carries no credential, address or transport parameter

### Requirement: A client is removed by publishing a version without it

A client SHALL be removed by publishing a configuration version that does not
carry it. Removal SHALL NOT depend on recovering, revoking or deleting anything
already given to that client.

#### Scenario: A client must be removed

- **WHEN** a client is to lose access
- **THEN** a configuration version without that client is published and applied
- **AND** the client loses access whether or not its profile still exists anywhere

#### Scenario: The removing version does not prove

- **WHEN** the version removing a client does not prove within its window
- **THEN** the host returns to the retained version and the client is not removed
- **AND** the failure to remove is visible rather than assumed

### Requirement: A client profile is derived from the published version

A client profile SHALL be derived from the configuration version the host has
published and verified, and SHALL NOT be derived from the draft it was
authored from.

A profile SHALL NOT be published anywhere a client fetches it. It is carried to
the client by the operator.

#### Scenario: A profile is produced

- **WHEN** a profile is produced for a client
- **THEN** it is derived from the bytes of a published, verified version
- **AND** it therefore describes the server that will answer

#### Scenario: A draft was never published

- **WHEN** a draft configuration was authored and a different version was published
- **THEN** a profile derived from the draft is not admissible, because it describes a server that is not answering

#### Scenario: A profile is delivered

- **WHEN** a client receives its profile
- **THEN** it is carried over by the operator
- **AND** no endpoint exists from which a client fetches one
