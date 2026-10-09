## ADDED Requirements

### Requirement: What a component reports is explained under that component

Every quantity a component's payload carries SHALL be explained in the
reference under that component, so a reader meeting it in a status answer can
tell which component it belongs to and what it counts there.

A word SHALL be explained once per payload that uses it. The same word carries
different subjects in different payloads — `configured` counts declared routes
in one, configured transports in another and configured relays in a third — and
one explanation cannot serve all of them.

A gate holding this SHALL be keyed on the payload and the component, not on the
word. A check that accepts the word anywhere in the document can be satisfied by
a row belonging to another vocabulary, and these vocabularies overlap: `ready`
and `degraded` are both transport counts and component states, and `missing` is
both a scoped-routes quantity and a diff reason.

Measured 2026-10-09: a flat check reported `TransportsPayload` fully explained
because `configured` matched a row about routes and `ready` and `degraded`
matched rows about component states, and reported the same of `missing` before
its own row existed. A gate that passes for the wrong reason is worse than the
absent gate it replaces, because it turns a missing guarantee into a false one.

#### Scenario: A payload gains a quantity

- **WHEN** a component payload gains a field and the reference does not explain it under that component
- **THEN** the gate refuses and names the payload and the field

#### Scenario: A word is explained for another payload

- **WHEN** a field's name is explained under a different component, or as a state, a reason or a classification
- **THEN** the gate still refuses, because the explanation is about another subject

#### Scenario: A payload loses a quantity

- **WHEN** a field leaves a payload and its explanation stays
- **THEN** the gate refuses, so the reference cannot describe a quantity nothing reports

#### Scenario: Every payload is explained

- **WHEN** every field of every payload is explained under its own component
- **THEN** the gate passes, and says how many payloads and fields it checked
