## MODIFIED Requirements

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
so a reader cannot be left to guess which of the two a number means.

The age bound SHALL be a window an operator can state. Thirty days was
configured and never applied: at twelve megabytes a day the archive reached its
byte bound in about twenty-one, so the age was a number nothing ever used. Seven
days is the window, and it is short enough to be the one that bites.
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

#### Scenario: A record is counted against the bound

- **WHEN** the archive weighs what it holds against its size bound
- **THEN** each stored record counts what the filesystem charges for it, not what it contains
- **AND** the size the archive reports is the same number

#### Scenario: A record is too large for the bound

- **WHEN** a record would occupy more than the whole bound once written
- **THEN** it is refused, whether or not its contents alone would have fitted
