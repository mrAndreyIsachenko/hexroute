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

### Requirement: A probe that cannot run still reaches the decision

An observation the runtime could not make SHALL reach the planner as an
observation it could not make. It SHALL NOT end the cycle before the planner is
consulted, and it SHALL NOT leave the previous conclusion standing as the
current one.

This is what makes the supervised subject's absence detectable at all. Reading
the Pritunl profile goes through the service; reading the service goes through
launchd and answers whether or not the service is there. Ordering the first
ahead of the second, and ending the cycle when it fails, puts the only probe
that can see a missing service behind a probe that requires it — so the one
condition meaning "the thing I supervise is gone" becomes the one condition
under which nothing is decided.

A runtime that cannot see SHALL NOT report health. Reporting the last state as
though it were current is how a supervised service stayed absent for nine
minutes while its supervisor reported HEALTHY with zero consecutive failures.

#### Scenario: The service is gone and its profile cannot be read

- **WHEN** the Pritunl service is absent, so reading the profile fails
- **THEN** the planner is still given the cycle's observation
- **AND** the runtime does not report the state it last observed as current

#### Scenario: An observation that does not depend on the subject

- **WHEN** one probe requires the supervised service and another does not
- **THEN** the one that does not is not made unreachable by the failure of the one that does

#### Scenario: A probe fails for a reason unrelated to the subject

- **WHEN** an observation cannot be made and nothing about the subject is established
- **THEN** the planner decides on what is known rather than on an assumption about what is not

### Requirement: The answering runtime confirms the fault for itself

A request naming a session that carries nothing SHALL carry the evidence — the
address the session claims — and not the conclusion drawn from it. The answering
runtime SHALL look for that address on its own interfaces before acting, and
SHALL refuse when it finds it.

Both runtimes SHALL read an interface the same way. Two parses of one command
agree until one is edited, and the disagreement would read as the second opinion
refusing what the first asked.

An address that cannot be read, or a look that could not be taken, SHALL NOT be
reported as an absence. Absence is what grounds a restart, and defaulting to it
would obtain one from a malformed request or a command that did not run.

Evidence SHALL be offered only when there is any. A session whose address is on
an interface is carrying, and asking about it would put a false premise in front
of the only runtime that can restart a production service.

#### Scenario: The session carries nothing and both runtimes see it

- **WHEN** the session reports itself connected, and neither runtime finds its address on any interface
- **THEN** the restart is approved, although the service is running

#### Scenario: The answering runtime finds the address

- **WHEN** the asking runtime reports the address missing and the answering one finds it
- **THEN** the request is refused

#### Scenario: The evidence cannot be read, or the look cannot be taken

- **WHEN** the address does not parse, or the command that would look does not run
- **THEN** the request is refused rather than treated as an absence

#### Scenario: The outer path is not ready

- **WHEN** the session carries nothing and the outer path is not ready
- **THEN** the request is refused, because restarting into a path that is not there repairs nothing

### Requirement: A refusal names the check that made it

A refusal SHALL carry the ground it was refused on, as far as the protocol can
express it, and the runtime that refused SHALL write down what the protocol
cannot. A caller SHALL NOT have to read source to learn why a request for the
one production act was declined.

The refusals are separate because they send the reader to different places: to
who asked, to what signed for it, to the generation in force, to the runtime's
own view of the outer path, and to the service itself. Collapsing them costs an
outage's worth of diagnosis, which is what it cost on 2026-09-09.

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

### Requirement: A service that is not running is asked about

The service beneath the session SHALL reach the decision that can ask root to
restart it. A service observed not running, or that could not be observed at
all, SHALL be grounds for requesting a restart when the session is not
connected, and SHALL take precedence over reconnecting — the reconnect goes
through the service that is not there.

Those two cases are one answer to the only question asked here, and a service
that cannot be found is not one that is running. Conflating them is safe because
what it grounds is a request: root reaches its own conclusion before restarting
anything.

The value a caller gets by saying nothing SHALL ask for nothing. This grounds a
request for root to restart a production service, so an unset observation is a
caller that made none, not a caller reporting a stopped service.

#### Scenario: The service is not running

- **WHEN** the session is not connected and the service is observed not running
- **THEN** root is asked to restart it, rather than the session being reconnected through it

#### Scenario: The service cannot be observed

- **WHEN** the service cannot be observed at all
- **THEN** it is treated as not running, and root reaches its own conclusion before acting

#### Scenario: The service is running

- **WHEN** the service is observed running
- **THEN** its state is not grounds for a restart, whatever the session is doing

#### Scenario: Nothing was observed of the service

- **WHEN** an observation carries no statement about the service
- **THEN** no restart is requested on that ground

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
