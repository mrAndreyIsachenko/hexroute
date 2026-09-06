## ADDED Requirements

### Requirement: Recovery acts only under a signed authorization

Hexroute SHALL recover a Pritunl session only while an active signed policy
generation grants that capability, and SHALL consult the existing mutation gate
before every act. Losing the generation SHALL remove the authority at once.

No second authorization mechanism SHALL exist for this path: not a build, not a
setting, not a file.

#### Scenario: The capability is granted

- **WHEN** an active signed generation grants the recovery capability and the mutation gate allows it
- **THEN** the user runtime may reconnect the session and may request a service restart

#### Scenario: The generation is rolled back

- **WHEN** the granting generation is rolled back or no generation is active
- **THEN** the runtime plans as before and acts on nothing
- **AND** the refusal is recorded as absent authority rather than as a failure

#### Scenario: Authority is claimed from somewhere else

- **WHEN** any path other than an active generation would permit the act
- **THEN** it is refused, because an authority nobody signed is not an authority

### Requirement: The privilege boundary survives the grant

The user runtime SHALL hold the credentials and SHALL NOT restart a system
service. The root runtime SHALL restart the service and SHALL NOT read a
credential, generate a one-time code, or learn one from the request.

A request crossing that boundary SHALL be typed and credential-free, and the
root runtime SHALL revalidate the situation with its own observations before
acting rather than trusting the request.

#### Scenario: A stale service is restarted

- **WHEN** the user runtime requests a restart and root's own probes confirm the service is stale and the outer path ready
- **THEN** root restarts that one named service

#### Scenario: Root is asked for something else

- **WHEN** a request names any other action
- **THEN** it is refused, whatever else about it is valid

#### Scenario: The request is inspected

- **WHEN** a crossing request is examined
- **THEN** it carries no PIN, seed or one-time code
- **AND** root reaches its own conclusion before acting on it

### Requirement: A secret never becomes an argument

Hexroute SHALL submit a Pritunl credential through the client's password-read
path. It SHALL NOT place a PIN, a seed or a one-time code in a command-line
argument, an environment variable, a log line or an error.

#### Scenario: A session is reconnected

- **WHEN** the runtime reconnects a session
- **THEN** the credential reaches the client through its input
- **AND** no argument, environment entry or log line carries it

#### Scenario: The reconnect fails

- **WHEN** the client exits non-zero
- **THEN** the recorded reason names the failure without reproducing what was submitted

### Requirement: A session that carries no traffic is recoverable

A session reporting itself connected, with a client address, while measurement
shows the path carrying no traffic, SHALL be grounds for requesting a service
restart. It SHALL NOT be grounds for reconnecting the session.

What Pritunl reports about its own session stays authoritative for reconnecting.
A path measurement is affected by the outer tunnel and the routes, and would
otherwise reconnect Pritunl for faults that are not its.

#### Scenario: The profile is connected and the path is dead

- **WHEN** the session reports connected with an address and the path carries no traffic
- **THEN** a service restart is requested
- **AND** no reconnect is attempted on that ground alone

#### Scenario: The session reports itself disconnected

- **WHEN** Pritunl reports the session not connected
- **THEN** the reconnect decision follows what Pritunl reports, not the path measurement
