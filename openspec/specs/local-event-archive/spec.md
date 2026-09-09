# Local Event Archive Specification

## Purpose

Define durable local retention of typed events, held by age and total size
rather than by upload state, so the host can answer questions about last week
after telemetry has acknowledged and the spool has let go.

## Requirements

### Requirement: Durable local retention independent of upload

Hexroute SHALL retain typed event records locally in an append-only archive
whose retention is a function of age and total size only. Acknowledgement,
success or failure of telemetry upload SHALL NOT remove a record from the
archive, and archiving SHALL NOT delay, duplicate or block an upload.

#### Scenario: An uploaded record is acknowledged

- **WHEN** telemetry acknowledges a record and the spool removes it
- **THEN** the archived copy remains readable until its own age or size bound evicts it

#### Scenario: Upload is unavailable

- **WHEN** telemetry cannot reach the cloud
- **THEN** archiving continues unaffected
- **AND** no archived record is uploaded as a side effect of being archived

### Requirement: Archive carries only what the typed schemas express

The archive SHALL store records that decode under the existing registered event
schemas and SHALL reject anything else. It SHALL NOT introduce a free-form
field, an alternative encoding or a second representation of an event.

#### Scenario: An unregistered record is offered

- **WHEN** a record does not decode under a registered schema
- **THEN** the archive refuses it
- **AND** the refusal is recorded as a bounded diagnostic rather than discarded

#### Scenario: A record is read back

- **WHEN** an archived record is read
- **THEN** it decodes to the same typed event that was appended
- **AND** no field exists that the event schemas cannot express

### Requirement: Bounded archive with observable overflow

The archive SHALL enforce a configured maximum age and maximum total size.
Size eviction SHALL remove lower-priority records before higher-priority ones,
age eviction SHALL apply regardless of priority, and reaching either bound
SHALL produce a durable overflow record naming the class of records dropped and
the sequence range they covered.

#### Scenario: The size bound is reached

- **WHEN** appending would exceed the configured size
- **THEN** diagnostics are evicted before operational records, and operational before critical
- **AND** an overflow record remains readable afterwards

#### Scenario: The age bound is reached

- **WHEN** records fall outside the configured window
- **THEN** they are evicted regardless of priority
- **AND** the window the archive actually covers remains reportable

#### Scenario: Only critical records remain and the bound is still exceeded

- **WHEN** no lower-priority record is left to evict
- **THEN** the archive refuses the append rather than dropping a critical record silently
- **AND** the refusal is visible as an overflow condition

### Requirement: Crash-safe archive writes

Every archive write SHALL use a staged file, a file synchronisation, an atomic
rename and a directory synchronisation. An interruption at any of those
boundaries SHALL leave the archive readable, with either the complete new
record present or absent, and never a partial one.

#### Scenario: The host stops during an append

- **WHEN** a write is interrupted at any boundary in that sequence
- **THEN** reopening the archive succeeds
- **AND** every readable record decodes completely

#### Scenario: A staged file survives a crash

- **WHEN** an unpublished staged file is found at startup
- **THEN** it is removed rather than read
- **AND** its absence does not create a gap in the retained sequence

### Requirement: Deterministic weekly review

The weekly review SHALL derive its findings from the archive by deterministic
computation over a requested window: counts by schema and component, observed
transitions, and a rarity ranking. Equal archives and equal windows SHALL
produce equal reports.

#### Scenario: The same window is reviewed twice

- **WHEN** the same archived window is reviewed again
- **THEN** the report is byte-identical

#### Scenario: The archive covers less than the requested window

- **WHEN** eviction has shortened the available history
- **THEN** the report states the window it actually covers
- **AND** it does not present a partial window as a complete one

#### Scenario: The window contains no records

- **WHEN** the requested window is empty
- **THEN** the report says so explicitly
- **AND** an empty window is not reported as a quiet, healthy one

### Requirement: Local model commentary cannot select findings

A local model pass SHALL be optional and disabled by default. When enabled it
SHALL receive the already-computed report and MAY attach commentary to existing
findings. It SHALL NOT add, remove, reorder or reweight a finding, and a report
SHALL remain valid and complete when the model is absent, slow or wrong.

#### Scenario: The model is unavailable

- **WHEN** the local model cannot be reached
- **THEN** the report is produced unchanged without commentary
- **AND** the absence is recorded in the report

#### Scenario: The model returns unusable output

- **WHEN** model output does not parse or references a finding that does not exist
- **THEN** the commentary is discarded
- **AND** the deterministic findings are unaffected

#### Scenario: Commentary is compared with the ranking

- **WHEN** a report is produced with and without the model
- **THEN** the ordered findings are identical in both
- **AND** only the commentary field differs

### Requirement: Review is local, unprivileged and observe-only

The weekly review SHALL run without network access, without credentials and
without any privileged or mutating operation. It SHALL write only its report,
and a failed review SHALL leave the archive and every production path
unchanged.

#### Scenario: A review runs

- **WHEN** the scheduled review executes
- **THEN** no route, DNS, process, tunnel, Pritunl, AdGuard or credential state changes
- **AND** nothing leaves the host

#### Scenario: A review fails

- **WHEN** the review cannot complete
- **THEN** the archive is unchanged and readable
- **AND** the next scheduled run is unaffected

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
