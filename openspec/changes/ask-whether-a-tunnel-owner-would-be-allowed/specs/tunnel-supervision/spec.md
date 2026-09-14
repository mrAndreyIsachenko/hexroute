# Tunnel Supervision Delta

## MODIFIED Requirements

### Requirement: The decision is recorded beside what the owner did

The runtime SHALL record each decision durably, and the record SHALL be
comparable against what the owning runtime actually did.

A decision that agreed and a decision that was never reached look the same in
an empty log. The comparison is the whole purpose of deciding without acting,
so the record SHALL exist on every cycle, including the cycles that decided to
do nothing.

A record SHALL carry the observations the decision was reached from, and every
cause that can hold SHALL have a ground in the record that a reader can check it
against. A decision naming a cause and nothing behind it reads as convincingly
when it is wrong as when it is right: on 2026-09-12 this runtime recorded six
rebuilds for a returned link in half an hour, and working out that the link had
not returned took the connectivity archive laid beside the decisions and matched
on time — because that archive, not the decision, held the count of endpoints
that answered.

That worked only because one runtime writes both stores. The comparison these
records exist for is against a runtime that writes neither, and when this rule is
granted authority the record is what a rebuilt tunnel would have to be explained
from.

A record SHALL also carry whether the runtime would have been permitted to do
what it decided. Deciding and being allowed are different questions, and a
record that answers only the first cannot show that the second was ever asked.

The answer SHALL be recorded on the cycles that decided to act, including every
cycle on which it is a refusal. A refusal recorded before any generation grants
the capability is what proves the question reaches the policy handler at all.

The grounds SHALL carry no identity. What carries the tunnel is recorded as a
digest and a count, never as the destinations it is made of, on the same terms
as every other projection here: how many there are and whether they changed,
never which they are.

#### Scenario: A cycle decides

- **WHEN** any cycle completes
- **THEN** its decision is recorded, whether or not it found a cause

#### Scenario: The owner acted

- **WHEN** the owning runtime rebuilt the tunnel
- **THEN** the record can be read to say whether this runtime would have, and for which cause

#### Scenario: A decision that would not be permitted

- **WHEN** a cycle decides to act and no active generation carries the capability
- **THEN** the record says the decision was not authorized, and names why

#### Scenario: The refusal is policy's, not the question's

- **WHEN** a cycle decides to act under a control state and an active generation that grants nothing
- **THEN** the question carries that control-state generation and a digest of the decision, and the recorded reason is the policy's — never that the request was malformed

#### Scenario: No control state yet

- **WHEN** a cycle decides to act before the runtime has a control-state generation
- **THEN** nothing is asked, and the record carries no authorization rather than a refusal

#### Scenario: A cause is read back

- **WHEN** a recorded decision names a cause
- **THEN** the same record carries the observation that cause was reached from
- **AND** a reader can tell a cause that held from one that should not have, without another store beside it

#### Scenario: A decision is recorded for a cycle that saw nothing

- **WHEN** a cycle stopped before it could observe
- **THEN** its record says so, and the grounds it could not gather are absent rather than reported as zero

#### Scenario: The grounds name what carries the tunnel

- **WHEN** a decision records what the configured destinations were carried by
- **THEN** it records a digest of it and how many entries it covers, and not the destinations
