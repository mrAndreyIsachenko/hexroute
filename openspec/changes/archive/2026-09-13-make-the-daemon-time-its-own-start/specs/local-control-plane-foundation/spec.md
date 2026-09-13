# Local Control Plane Foundation Delta

## ADDED Requirements

### Requirement: A runtime reports quantities about itself

A component SHALL be able to record how long a named step of its own took, and
the steps that may be named SHALL be a closed vocabulary like every other field
of a log record. A duration SHALL be a number of milliseconds and nothing else:
it carries no path, no identity and no free text, so it cannot become a channel
for what the record is redacted of.

Opening durable stores SHALL be timed and recorded before the component reports
that it started, because that is the window in which it is not yet observing and
the one nothing outside it can attribute.

A component SHALL NOT depend on being sampled from outside to answer how long
its own work took. Three attributions taken that way in one session were wrong —
seventeen seconds assigned to a store that cost seven, thirteen to listings that
cost 951 milliseconds, and a window read as work that the samples show was spent
waiting. A sample tree says where a process is, and reading presence there as
weight is a mistake available to anyone holding it.

#### Scenario: A daemon opens its stores

- **WHEN** a daemon opens the durable stores it needs before it can observe
- **THEN** it records how long each one took, named, before it reports that it started

#### Scenario: A step that was not timed

- **WHEN** a record names a step outside the closed vocabulary
- **THEN** the record is refused rather than written

#### Scenario: A record carries no duration

- **WHEN** an event has no duration to report
- **THEN** the field is absent rather than zero, because a step that took no time and a step that was not timed are different claims
