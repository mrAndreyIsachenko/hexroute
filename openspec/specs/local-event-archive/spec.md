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

The size bound SHALL count what the filesystem charges for the records rather
than what the records contain, and a record not yet written SHALL be weighed by
what it will take once written. The archive holds one file per record at about
1.25 kilobytes against a four-kilobyte allocation unit, so a bound counting
contents promises about a third of the room it actually takes: on 2026-09-11 an
archive bounded at 256 megabytes held 96.7 megabytes of records and occupied
315 megabytes of disk. A bound is a promise about a disk.

What the archive reports as its size SHALL be what it counts against that bound,
so a reader cannot be left to guess which of the two a number means. Choosing
what to evict SHALL count what each record occupies in the same units.

The size bound SHALL be large enough that the age window is what binds. A size
bound reached first makes the window a number nothing uses, which is what the
thirty-day window was before it: measured 2026-09-15 the runtime wrote 19,383
records a day, one four-kilobyte block each, so a 256-megabyte bound held about
3.6 days against a window of seven, and the oldest operational record was 2 days
18 hours old. A week of that rate occupies about 530 megabytes; the bound is a
gigabyte.

The age bound SHALL be a window an operator can state. Thirty days was
configured and never applied: at twelve megabytes a day the archive reached its
byte bound in about twenty-one, so the age was a number nothing ever used. Seven
days is the window, and it is short enough to be the one that bites.
Size eviction SHALL remove lower-priority records before higher-priority ones,
age eviction SHALL apply regardless of priority, and reaching either bound
SHALL produce a durable overflow record naming the class of records dropped and
the sequence range they covered.

Overflow records SHALL NOT be written at the rate records are appended. An
overflow record is critical and size eviction never removes one, so an archive
that named every evicting append filled with them: on 2026-09-14 the live
archive held 14,832 overflow records among 65,536, and its oldest operational
record was three days old in a seven-day window. An eviction for size SHALL
free a sixty-fourth of the bound beyond what the append needs, when that much
can be evicted. An eviction for age SHALL NOT happen until the oldest record is
a sixty-fourth of the window outside it, and SHALL then evict every record
outside the window. The overflow record SHALL still be written when the
eviction happens, never held back to be written later.

#### Scenario: The size bound is reached

- **WHEN** appending would exceed the configured size
- **THEN** diagnostics are evicted before operational records, and operational before critical
- **AND** an overflow record remains readable afterwards

#### Scenario: The age bound is reached

- **WHEN** records fall outside the configured window by more than a sixty-fourth of it
- **THEN** every record outside the window is evicted regardless of priority
- **AND** the window the archive actually covers remains reportable

#### Scenario: Only critical records remain and the bound is still exceeded

- **WHEN** no lower-priority record is left to evict
- **THEN** the archive refuses the append rather than dropping a critical record silently
- **AND** the refusal is visible as an overflow condition

#### Scenario: A record is counted against the bound

- **WHEN** the archive weighs what it holds against its size bound
- **THEN** each stored record counts what the filesystem charges for it, not what it contains
- **AND** the size the archive reports is the same number

#### Scenario: A record is too large for the bound

- **WHEN** a record would occupy more than the whole bound once written
- **THEN** it is refused, whether or not its contents alone would have fitted

#### Scenario: An archive stays past its size bound

- **WHEN** records keep arriving at an archive that is at its size bound
- **THEN** each eviction frees a sixty-fourth of the bound and is named in one overflow record per class dropped
- **AND** records are evicted by what they occupy, not by what they contain

#### Scenario: Records keep expiring

- **WHEN** records reach the end of the window about as often as they are appended
- **THEN** nothing is evicted for age until the oldest is a sixty-fourth of the window outside it
- **AND** no record is kept longer than the window and that share

#### Scenario: The window is what binds

- **WHEN** the runtime writes at the rate measured on this machine
- **THEN** records leave because they fall outside the window, not because the archive is full
- **AND** the archive reports the window it states

#### Scenario: Less than the share can be evicted

- **WHEN** what lower-priority records occupy covers what the append needs but not the share
- **THEN** they are evicted and the append goes ahead

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

The cost of appending SHALL NOT include listing the directory. What the archive
knows about its own directory SHALL be learned once and kept current as it
publishes and evicts, so that appending costs the write and the one record whose
age is established, and nothing that grows with how many records are stored.

The previous statement of this requirement sanctioned a listing per append and
called the growth acceptable because the size bound caps it. That was wrong in
the way that matters: a runtime appends many records per cycle, so the cost is
the listing multiplied by the appends, and the bound it was measured against
doubled within two days of being written.

An archive MAY discard what it knows about its directory at any time and learn it
again, and SHALL do so whenever a write may have half happened. It SHALL NOT be
required to notice a record added or removed by something other than itself: an
archive has one writer, and a reader's handle cannot write.

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

The second number was the one this requirement then permitted, and it was the
whole cost only for a single append. One cycle's worth of eleven appends against
20,000 stored records cost 767 milliseconds when the directory was listed each
time and 109 when it was listed once. On 2026-09-11 a profile of the live root
process, taken while its operator loop had been unresponsive for six to thirteen
seconds at a time against a fifteen-second deadline, put its time in the
directory listing and the sort inside it.

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

#### Scenario: A cycle appends several records

- **WHEN** a runtime appends many records to the same archive without anything else touching its directory
- **THEN** the directory is listed at most once, however many records are appended

#### Scenario: A write does not finish

- **WHEN** a write leaves the directory in a state the archive did not complete
- **THEN** the archive discards what it knew and learns it again before answering anything
