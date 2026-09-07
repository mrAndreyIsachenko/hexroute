## ADDED Requirements

### Requirement: A spool operation costs what it uses

A spool operation SHALL read and decode only the records it hands out. An
operation needing sequences, sizes or record identity SHALL obtain them from the
directory and from file metadata, and SHALL NOT decode stored payloads to learn
them.

No operation SHALL recompute a value the filesystem already reports. Appending,
reporting size and acknowledging SHALL NOT grow more expensive as the spool
fills within its size bound.

#### Scenario: A record is appended to a full spool

- **WHEN** a record is appended while the spool holds tens of thousands of entries inside its byte bound
- **THEN** the cost of appending does not grow with the number of entries already stored
- **AND** no stored payload is decoded to complete the append

#### Scenario: The spool is asked its size

- **WHEN** a caller asks how much the spool holds
- **THEN** the answer comes from file metadata rather than from re-reading the records

#### Scenario: Records are handed to a caller

- **WHEN** an operation returns stored records to a caller
- **THEN** each returned record is decoded and proved before it is handed out

### Requirement: A damaged record stops only its own use

A record that cannot be proved SHALL be set aside and reported, and SHALL NOT
prevent other records from being appended, returned or acknowledged. Setting
aside SHALL NOT delete it: the damaged record stays on disk, because it is the
only evidence of what happened.

A spool SHALL NOT refuse to record a new observation because a stored record is
damaged. The observation being lost is the larger loss, and the damaged record
is already beyond saving.

#### Scenario: A stored record cannot be decoded

- **WHEN** a stored record fails to prove on the path that reads it
- **THEN** it is set aside, still present on disk, and reported as an incident
- **AND** appending, returning other records and acknowledging continue

#### Scenario: A damaged record is present when a new observation arrives

- **WHEN** a new record is appended while a damaged record is stored
- **THEN** the append succeeds

#### Scenario: The directory itself is unusable

- **WHEN** the spool directory cannot be read, or its ownership or mode are wrong
- **THEN** the spool refuses to operate, because nothing about it can be trusted

### Requirement: A publication waits less than the cycle that issues it

A component publishing to another SHALL bound its wait by a value derived from
its own observation interval rather than by a fixed default, so that a slow peer
costs one publication instead of the cycle.

A publication that does not complete SHALL be abandoned rather than buffered,
and the next cycle SHALL publish current facts. A fact held back and delivered
later would describe a moment that has passed.

#### Scenario: The peer is slow to answer

- **WHEN** a publication does not complete within its deadline
- **THEN** the publisher abandons it and the observation cycle continues on schedule
- **AND** the next cycle publishes what is true then

#### Scenario: The observation interval is reconfigured

- **WHEN** the observation interval changes
- **THEN** the publication deadline changes with it, staying below one cycle
