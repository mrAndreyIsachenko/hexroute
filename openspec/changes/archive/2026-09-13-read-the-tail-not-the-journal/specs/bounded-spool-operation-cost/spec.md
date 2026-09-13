# Bounded Spool Operation Cost Delta

## ADDED Requirements

### Requirement: A caller may read one record without reading the rest

A spool SHALL say which sequences it holds without opening any record, and SHALL
hand back a single record named by its sequence, proved the way any returned
record is proved.

Without these the only way to reach a record is to ask for all of them. A caller
that wanted the newest few therefore decoded the whole spool, which is how a
daemon came to spend 152 seconds of every start finding four records among
136,397.

Asking for a sequence the spool does not hold SHALL be an ordinary answer rather
than an error. Records leave by eviction and by acknowledgement, so a caller
holding a listing from a moment ago is asking a reasonable question about a
record that has since gone.

#### Scenario: A caller asks which sequences are held

- **WHEN** a caller asks what the spool holds
- **THEN** it receives the sequences without any stored record being opened

#### Scenario: A caller asks for one record

- **WHEN** a caller names a sequence the spool holds
- **THEN** that record is decoded and proved, and no other record is opened

#### Scenario: A caller asks for a record that has gone

- **WHEN** a caller names a sequence that has been evicted or acknowledged
- **THEN** the spool says it does not hold it, and this is not an error
