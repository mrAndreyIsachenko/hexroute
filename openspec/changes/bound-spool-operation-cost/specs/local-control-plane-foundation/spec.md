## MODIFIED Requirements

### Requirement: Bounded redacted local journal

Root and user components SHALL maintain separate crash-safe priority journals,
preserve critical events ahead of diagnostics and never serialize protected
credential classes.

A journal SHALL be bounded in the cost of using it as well as in its size.
Reading, appending to or measuring a journal SHALL NOT grow more expensive as it
fills within its size bound, and a journal that has reached that bound SHALL
remain usable at the same cost as an empty one.

A damaged record SHALL stop its own use and nothing else. A journal SHALL
continue to record while holding a record it cannot prove, because refusing to
observe is a larger loss than the record already lost.

#### Scenario: Journal reaches its size bound

- **WHEN** new records would exceed the configured capacity
- **THEN** diagnostic records are evicted before critical transitions or incidents
- **AND** overflow remains observable

#### Scenario: Journal is full of small records

- **WHEN** a journal holds tens of thousands of records inside its size bound
- **THEN** appending to it costs no more than appending to an empty one
- **AND** the component serving it continues to answer its socket

#### Scenario: Secret canary reaches serialization

- **WHEN** a protected credential canary appears in an event candidate
- **THEN** serialization or its test gate fails before persistence or upload

#### Scenario: A stored record is damaged

- **WHEN** a record in the journal cannot be proved
- **THEN** it is set aside and reported, and new records are still recorded
