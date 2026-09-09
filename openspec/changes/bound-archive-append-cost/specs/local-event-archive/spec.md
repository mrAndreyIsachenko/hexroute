## ADDED Requirements

### Requirement: An archive operation costs what it uses

An archive operation SHALL read and decode only the records it hands out or is
about to evict. An operation needing a record's sequence or size SHALL obtain it
from the directory and from file metadata, and SHALL NOT decode a stored record
to learn it.

Appending SHALL NOT decode a stored record on the path that evicts nothing. Its
cost SHALL NOT grow with how many records the archive holds.

Deciding whether the age bound has been reached SHALL NOT require reading every
retained record. Records carry a monotonic sequence and are appended in order,
so the oldest retained record is the lowest retained sequence: an append MAY
read that record to learn whether anything has expired, and SHALL read further
records only while they are themselves expired.

Deciding what to evict for the size bound MAY read records, because eviction
chooses by priority and priority is a property of the record. That reading SHALL
happen only when the size bound is actually reached — the path that is about to
rewrite the directory in any case.

Measured on this repository's records, one append cost about nine hundred
milliseconds at forty-one thousand stored records when every record was decoded,
against about eighteen milliseconds at one thousand. Paid twice per published
fact through the journal's mirror, that is what stopped a root daemon answering
its socket inside a caller's deadline while nothing about the caller, the
network or the exchange was wrong.

#### Scenario: A record is appended to a large archive

- **WHEN** a record is appended while the archive holds tens of thousands of records inside its age and size bounds
- **THEN** no stored record is opened or decoded beyond the one that establishes the oldest retained record's age
- **AND** the append does not slow as the archive grows

#### Scenario: Nothing has expired

- **WHEN** the oldest retained record is inside the configured age window
- **THEN** no further record is read to establish that nothing has expired

#### Scenario: Some records have expired

- **WHEN** the oldest retained records fall outside the configured age window
- **THEN** only the expired records are read, and reading stops at the first retained record that is inside the window

#### Scenario: The size bound is reached

- **WHEN** appending would exceed the configured size
- **THEN** records may be read to choose the lowest-priority eviction, because priority cannot be known from the directory
- **AND** that reading happens only on the path that evicts

#### Scenario: An undecodable record is stored

- **WHEN** a stored record cannot be decoded and the append evicts nothing
- **THEN** the append neither fails on it nor notices it
