## MODIFIED Requirements

### Requirement: A refusal names the check that made it

A refusal SHALL carry the ground it was refused on, as far as the protocol can
express it, and the runtime that refused SHALL write down what the protocol
cannot. A caller SHALL NOT have to read source to learn why a request for the
one production act was declined.

The refusals are separate because they send the reader to different places: to
who asked, to what signed for it, to the generation in force, to the runtime's
own view of the outer path, and to the service itself. Collapsing them costs an
outage's worth of diagnosis, which is what it cost on 2026-09-09.

An attempt that was made and did not work SHALL say where it stopped. A
reconnect passes through several steps that fail for unrelated reasons — a
one-time-code window too short to use, credentials it could not read, a client
that would not start the session — and they send the reader to different places
in the same way a refusal does. Reporting only that the attempt failed leaves
the next reader to choose between explanations by reading source, which cannot
say which one happened.

#### Scenario: The peer or the authority is refused

- **WHEN** the caller is not the operator, or nothing signed for the act
- **THEN** the refusal is reported as one of authority, and which of the two is recorded by the runtime that refused

#### Scenario: The generation is not the one in force

- **WHEN** the request names a generation other than the one authorizing the act
- **THEN** the refusal says so, rather than reporting a failed precondition

#### Scenario: One of the runtime's own checks is not satisfied

- **WHEN** the outer path is not ready, or the service is not in the state the act repairs
- **THEN** the refusal reports a precondition, and the runtime records which of them refused

#### Scenario: The caller records what it was told

- **WHEN** a request is refused
- **THEN** the caller's own record names the ground, so it is legible without reading the other runtime's log

#### Scenario: The runtime cannot act at all

- **WHEN** an authority was granted and what would exercise it is absent
- **THEN** that is recorded as a fault in the deployment, distinct from an attempt that did not work

#### Scenario: The act was attempted and did not work

- **WHEN** the act is attempted and the attempt fails
- **THEN** that is recorded as such, and not as an absence of the means to try
- **AND** the step it stopped at is recorded with it

#### Scenario: The credentials cannot be read

- **WHEN** a reconnect cannot obtain what it would submit
- **THEN** that is what is recorded, and not confused with a client that refused the attempt

#### Scenario: The client would not start the session

- **WHEN** the submission is made and the client does not start the session
- **THEN** that is what is recorded, and not confused with credentials it never had

#### Scenario: The window is too short to use

- **WHEN** the one-time code would expire before it could be used
- **THEN** that is recorded as a reason to wait rather than as a failure of the attempt

#### Scenario: The refusal is made above the act

- **WHEN** a request for the act is refused before the runtime evaluates the act itself — because the runtime's standing to act at all is not satisfied
- **THEN** the refusing runtime records it with the ground, as it does for every refusal it reaches by evaluating

#### Scenario: A request nothing answered is not a refusal

- **WHEN** the answering runtime does not take up the request and answers with a failure of its own
- **THEN** that is recorded by both runtimes as a failure of the runtime asked, and the asking runtime SHALL NOT report it as a refusal
