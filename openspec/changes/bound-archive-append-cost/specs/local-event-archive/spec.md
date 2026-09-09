## ADDED Requirements

### Requirement: An archive operation costs what it uses

An archive operation SHALL read and decode only the records it hands out or is
about to evict. An operation needing a record's sequence or size SHALL obtain it
from the directory and from file metadata, and SHALL NOT decode a stored record
to learn it.

Appending SHALL NOT decode a stored record on the path that evicts nothing,
beyond the one that establishes the oldest retained record's age.

The cost of appending SHALL be that of listing the directory, reading each
record's metadata, and reading that one record. It therefore still grows with how
many records are stored, and that growth is bounded by the existing size bound:
the archive cannot hold more records than its byte limit admits at the smallest
record size. Stating it the stronger way would be false — the total size is what
requires touching every entry, and the filesystem is the only thing that reports
it.

Deciding whether the age bound has been reached SHALL NOT require reading every
retained record. Records carry a monotonic sequence and are appended in order,
so the oldest retained record is the lowest retained sequence: an append MAY
read that record to learn whether anything has expired, and SHALL read further
records only while they are themselves expired.

A record that cannot be decoded SHALL NOT fail the append. Its age cannot be
established, so it is neither retained on the strength of a timestamp it does
not have nor allowed to stop the walk: the walk continues to the next sequence.
The observation being recorded is the larger loss, and a record already beyond
proving is beyond saving.

Deciding what to evict for the size bound MAY read records, because eviction
chooses by priority and priority is a property of the record. That reading SHALL
happen only when the size bound is actually reached — the path that is about to
rewrite the directory in any case.

Measured on this repository's records: appending cost about 900 milliseconds at
41,492 stored records when every record was decoded, and about 123 milliseconds
once it was not — against 15 milliseconds at a thousand records. Paid twice per
published fact through the journal's mirror, the first of those numbers is what
stopped a root daemon answering its socket inside a caller's deadline while
nothing about the caller, the network or the exchange was wrong.

#### Scenario: A record is appended to a large archive

- **WHEN** a record is appended while the archive holds tens of thousands of records inside its age and size bounds
- **THEN** no stored record is opened or decoded beyond the one that establishes the oldest retained record's age
- **AND** what the append does pay grows only with the directory listing and the per-record metadata the filesystem reports, not with the size of the records

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

- **WHEN** a stored record cannot be decoded
- **THEN** the append does not fail on it
- **AND** it is treated as a record whose age cannot be established, so the walk continues to the next sequence rather than stopping
