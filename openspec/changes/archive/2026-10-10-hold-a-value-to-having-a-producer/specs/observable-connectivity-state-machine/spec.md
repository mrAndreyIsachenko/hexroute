## MODIFIED Requirements

### Requirement: What the read model publishes is explained where it is owned

Every value and every quantity the read model publishes SHALL be explained in
the reference in the place that owns it: a component's payload under that
component, and a vocabulary under the section that vocabulary belongs to. A
reader meeting a word in a status answer SHALL be able to tell which of its
meanings is in play.

A word SHALL be explained once per place that uses it. The same word carries
different subjects in different places — `configured` counts declared routes in
one payload, configured transports in another and configured relays in a third;
`ready` and `degraded` are both transport counts and component states;
`missing` is both a scoped-routes quantity and a diff classification; `none` is
a diff reason and a value of six payload classes — and one explanation cannot
serve all of them.

A gate holding this SHALL be keyed on the place, not on the word. A check that
accepts the word anywhere in the document can be satisfied by a row belonging to
another vocabulary, which means it cannot notice the explanation it exists to
require being removed.

Measured 2026-10-09, twice. A flat check reported `TransportsPayload` fully
explained because `configured` matched a row about routes and `ready` and
`degraded` matched rows about component states. And the row explaining the
component state `degraded` was deleted from its own section while the gate still
passed, because the word survives where it counts transports. A gate that passes
for the wrong reason is worse than the absent gate it replaces, because it turns
a missing guarantee into a false one.

A gate SHALL refuse rather than widen when the place it is keyed to is absent,
and SHALL refuse a value explained in that place which the code no longer
declares, so the reference cannot describe a vocabulary that has been narrowed.

A vocabulary whose values are a subset of another's SHALL NOT be treated as
explained by that other's rows. Three vocabularies here share their words and
mean different things — a collector's own account of what it asserted, the state
the read model derived from it, and what the summary says of the host — and the
words a reader gets wrong are exactly the shared ones. Each SHALL be explained
in its own right, and a gate satisfied by the containing vocabulary's rows is
keyed on the word again, one level further in.

A published value SHALL either have something that emits it or be written down
as having none, with the reason. A vocabulary fixed before the collectors that
will use it is a deliberate choice and not a defect, but which of its values are
not yet reachable SHALL be a recorded fact rather than something a reader works
out from the code.

A gate holding this SHALL fail in both directions: a value that gains an emitter
while still recorded as having none, and a value that loses its last emitter
without being recorded.

Such a gate SHALL search by the qualified name outside the declaring package,
and SHALL exclude the vocabulary's own declaration and its validity switch.
Those are the three ways this measurement goes wrong, and it went wrong all
three ways before it was written down: counting a validity switch made every
value look produced, excluding a declaring file whole hid the values its own
functions return, and a bare name matched a different package's constant of the
same name.

#### Scenario: A payload gains a quantity

- **WHEN** a component payload gains a field and the reference does not explain it under that component
- **THEN** the gate refuses and names the payload and the field

#### Scenario: A vocabulary gains a value

- **WHEN** a published vocabulary gains a value and the section that owns it does not explain it
- **THEN** the gate refuses and names the vocabulary and the value

#### Scenario: An explanation is removed

- **WHEN** the row explaining a value is deleted from the place that owns it, and the word remains elsewhere in the document
- **THEN** the gate refuses, which is what a check keyed on the word cannot do

#### Scenario: The place is missing

- **WHEN** the section a check is keyed to is absent from the reference
- **THEN** the check refuses rather than searching the whole document

#### Scenario: A word is explained for another payload

- **WHEN** a field's name is explained under a different component, or as a state, a reason or a classification
- **THEN** the gate still refuses, because the explanation is about another subject

#### Scenario: A payload loses a quantity

- **WHEN** a field leaves a payload and its explanation stays
- **THEN** the gate refuses, so the reference cannot describe a quantity nothing reports

#### Scenario: A vocabulary is narrowed

- **WHEN** a value leaves a published vocabulary and its explanation stays in the place that owned it
- **THEN** the gate refuses, for the same reason and in the same way

#### Scenario: Every payload is explained

- **WHEN** every field of every payload is explained under its own component
- **THEN** the gate passes, and says how many payloads and fields it checked

#### Scenario: Every vocabulary is explained where it is owned

- **WHEN** every value of every published vocabulary is explained in the section that owns it
- **THEN** the gate passes, and says how many sections and values it checked

#### Scenario: A vocabulary's values are a subset of another's

- **WHEN** every value of one published vocabulary is also a value of another, and only the other is explained
- **THEN** the gate refuses, because rows about the containing vocabulary are about a different thing

#### Scenario: Several vocabularies share a place

- **WHEN** one section explains more than one vocabulary
- **THEN** each is explained in its own right there, and the gate holds each separately

#### Scenario: A value has nothing that emits it

- **WHEN** a published value is emitted by nothing in the running system and is not recorded as having no producer
- **THEN** the gate refuses and names the vocabulary and the value

#### Scenario: A value recorded as unproduced gains a producer

- **WHEN** something begins emitting a value that is recorded as having no producer
- **THEN** the gate refuses, so the record cannot outlive what it describes

#### Scenario: Only a fixture emits a value

- **WHEN** the only thing emitting a value is a synthetic fixture
- **THEN** it counts as having no producer, because a fixture is not the running system

#### Scenario: A vocabulary names its own values to validate them

- **WHEN** a vocabulary's validity switch names every one of its values
- **THEN** that does not count as emitting any of them
