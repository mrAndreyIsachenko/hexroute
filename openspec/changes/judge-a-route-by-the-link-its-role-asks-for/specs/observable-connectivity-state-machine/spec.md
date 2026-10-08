## ADDED Requirements

### Requirement: A component fact is judged against its own configured expectation

A fact about a configured component SHALL be computed against the expectation
that component's configuration sets, and not against one expectation chosen for
every part of it. Where a configuration assigns parts of a component to
different places, a part SHALL be judged against the place its own
configuration asks for.

A part the configuration does not ask for SHALL NOT be counted as present or as
diverging. It is outside the question, and counting it answers a question nobody
asked.

The lifecycle a fact reports SHALL be reachable for every configuration the
runtime accepts. A fact whose ready condition cannot hold for a valid
configuration reports the same value for the life of that configuration and
therefore reports nothing: measured 2026-10-08, `scoped_routes` read `degraded`
with `configured 21, conflicting 14, installed 7` while every other component
read `ready`, and the 14 were exactly the routes whose role asks for a link
other than the managed tunnel — correct placement, counted as conflict, by a
ready condition requiring all 21 on one link.

A quantity SHALL answer one question. A part the configuration asks for and the
host lacks, and a part the host has in the wrong place, are different states
with different remedies, and SHALL be counted separately rather than summed
under one name.

The rule that decides where a part belongs SHALL have one statement. A fact
SHALL be derived from the output of that rule rather than from a second
statement of it beside the collector.

#### Scenario: A component's parts belong in different places

- **WHEN** a configuration assigns parts of one component to several places
- **THEN** each part is judged against the place its own configuration names
- **AND** a part correctly in a place other than the first is not counted as diverging

#### Scenario: A part the configuration does not ask for

- **WHEN** the configuration asks for no such part under the current conditions
- **THEN** it is counted neither as present nor as diverging

#### Scenario: A part is asked for and absent

- **WHEN** the configuration asks for a part the host does not have
- **THEN** the fact counts it apart from a part the host has in the wrong place

#### Scenario: Ready is reachable

- **WHEN** every part the configuration asks for is where it asks for it
- **THEN** the component reads ready, whatever places the configuration spread its parts across

#### Scenario: The deciding rule is not restated

- **WHEN** a fact needs to know where a part belongs
- **THEN** it takes the answer from the one rule that decides it, so the two cannot disagree
