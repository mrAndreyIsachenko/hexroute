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
