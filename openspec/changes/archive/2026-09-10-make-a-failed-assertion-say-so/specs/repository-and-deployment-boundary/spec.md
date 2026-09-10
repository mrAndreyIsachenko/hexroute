## ADDED Requirements

### Requirement: A gate's assertion can fail and says which one did

Every assertion in a repository gate SHALL be written so that a false claim
ends the gate, on every shell the gate is run with, and SHALL name the claim
that was false.

This is not a style rule. A conditional written as a bare statement does not end
a script under the bash that ships with macOS, which is the only platform where
these gates run, so an assertion in that shape reports false and lets the gate
reach its success message. An assertion that cannot fail is worse than none: it
is a claim in the repository that something is checked, and every later reader
believes it.

#### Scenario: A claim in a gate is false

- **WHEN** a gate asserts something that does not hold
- **THEN** the gate fails, whatever shell it was run with

#### Scenario: A gate reports a failure

- **WHEN** a gate fails on an assertion
- **THEN** its output names the claim that was false, rather than only that the gate failed

#### Scenario: The shape that cannot fail is reintroduced

- **WHEN** a gate is written with an assertion that a shell would evaluate and continue past
- **THEN** a gate refuses it, so the shape cannot return by being copied from a neighbour
