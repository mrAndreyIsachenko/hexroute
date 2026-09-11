## MODIFIED Requirements

### Requirement: A spool operation costs what it uses

A spool operation SHALL read and decode only the records it hands out. An
operation needing sequences, sizes or record identity SHALL obtain them from the
directory and from file metadata, and SHALL NOT decode stored payloads to learn
them.

No operation SHALL recompute a value the filesystem already reports.

The cost of appending SHALL NOT include listing the directory, and SHALL NOT
include opening or decoding any stored record. What the spool knows about its
own directory SHALL be learned once and kept current as it publishes and evicts,
so that appending costs the write and nothing that grows with how many records
are stored.

The previous statement of this requirement sanctioned a listing per append and
called the growth acceptable because the size bound caps it. That is true of one
append. A runtime appends eleven records per cycle, so the cost is the listing
multiplied by the appends, and no bound in this system caps the second factor.

A spool MAY discard what it knows about its directory at any time and learn it
again, and SHALL do so whenever a write may have half happened and whenever
anything other than publishing or evicting moves a stable record. It SHALL NOT
be required to notice a record added or removed by something other than itself.

Measured on this repository's records, an append costs about eleven milliseconds
at a thousand stored records and a hundred and ninety at sixty thousand, against
roughly one and a half seconds when every record was decoded. One cycle's eleven
appends against twenty thousand stored records cost 775 milliseconds listing each
time and 97 listing once.

#### Scenario: A record is appended to a full spool

- **WHEN** a record is appended while the spool holds tens of thousands of entries inside its byte bound
- **THEN** no stored record is opened or decoded to complete the append
- **AND** an undecodable stored record neither fails the append nor is noticed by it

#### Scenario: The spool is asked its size

- **WHEN** a caller asks how much the spool holds
- **THEN** the answer comes from file metadata rather than from re-reading the records

#### Scenario: Records are handed to a caller

- **WHEN** an operation returns stored records to a caller
- **THEN** each returned record is decoded and proved before it is handed out

#### Scenario: A cycle appends several records

- **WHEN** a runtime appends many records to the same spool without anything else moving its stable records
- **THEN** the directory is listed at most once, however many records are appended

#### Scenario: Something other than an append moves a record

- **WHEN** an upload is acknowledged, a record is quarantined, or a write does not finish
- **THEN** the spool discards what it knew and learns it again before answering anything
