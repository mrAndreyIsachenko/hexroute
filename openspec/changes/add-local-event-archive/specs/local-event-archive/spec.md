## ADDED Requirements

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
