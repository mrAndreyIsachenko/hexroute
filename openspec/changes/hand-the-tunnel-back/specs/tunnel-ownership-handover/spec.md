# Tunnel Ownership Handover Delta

## MODIFIED Requirements

### Requirement: The new owner takes the process before it starts one

The transaction SHALL stop the tunnel process it is taking over before it starts
its own, and SHALL do so after the claim is placed rather than before. The claim
is what makes the process the new owner's to stop; stopping first would be a
runtime ending another runtime's job without having said so on disk.

Two tunnel processes SHALL never be started on one tunnel address. The previous
owner stops restarting the process while the claim is held but does not stop the
one already running, so a transaction that only claimed and started would leave
both.

The process the transaction stops SHALL be identified the same way the decision
rule identifies the tunnel process, so the runtime that stops it and the record
that reports on it cannot come to disagree about which process that is. The
identification SHALL NOT be narrowed to a process this runtime started, because
the process being taken over is by definition another runtime's.

The tunnel process SHALL be identified by the configuration its owner runs, and
SHALL NOT be identified by the name of its executable. Other processes run the
same executable: an ingress probe runs it against a temporary configuration, and
on 2026-09-14 the handover stopped the right process only because the tunnel's
process identifier happened to be the lower. A claim on disk means the owner is
this runtime and names the configuration it runs; no claim means the previous
owner and its configuration.

The wait for the previous process to be gone SHALL be bounded, and reaching that
bound SHALL abort rather than start. Not knowing whether anything holds the
tunnel SHALL be treated as something holding it: an observation that failed is
the case where a second process is most likely.

#### Scenario: A tunnel is running when the transaction begins

- **WHEN** the transaction has placed the claim and a tunnel process is running
- **THEN** that process is stopped and observed gone before any new one is started

#### Scenario: Nothing holds the tunnel

- **WHEN** the transaction has placed the claim and no tunnel process is running
- **THEN** nothing is signalled and the transaction starts its own

#### Scenario: The previous process does not stop

- **WHEN** the process being taken over is still running at the bound
- **THEN** the transaction aborts without starting a tunnel, and the claim is released

#### Scenario: The previous process cannot be observed

- **WHEN** the reading that would say whether a tunnel process is running fails
- **THEN** the transaction aborts without starting a tunnel, and the refusal names the reading

#### Scenario: The operator interrupts the transaction

- **WHEN** the terminal holding the transaction is interrupted while it waits for proofs
- **THEN** the attempt ends at once rather than running to its deadline

#### Scenario: A rehearsal takes nothing

- **WHEN** the transaction is rehearsed while a tunnel process is running
- **THEN** nothing is signalled, for the same reason a rehearsal starts nothing

#### Scenario: A probe runs the same executable

- **WHEN** a sing-box runs against a configuration that is not the owner's, such as an ingress probe's
- **THEN** it is not the tunnel process: it is not stopped, not counted as running, and its absence is not a lost tunnel

#### Scenario: The claim names what it covers

- **WHEN** this runtime places a claim
- **THEN** the claim records the configuration this runtime's tunnel runs, and a claim written before it did is read as covering this runtime's default configuration

## ADDED Requirements

### Requirement: The tunnel is handed back in one transaction

Giving the tunnel back SHALL be a transaction with the same discipline as taking
it: the phase recorded before each act, completion only on traffic, and a
terminal that dies leaving a phase a later invocation can undo.

The transaction SHALL stop the tunnel process this runtime runs before it
releases the claim, and SHALL NOT release first. Releasing first leaves two
runtimes each believing the tunnel is theirs, and the previous owner identifies
its stale process by its own configuration, so it starts a second one beside
this runtime's rather than replacing it.

It SHALL complete only on consecutive proofs that traffic traversed the tunnel
the previous owner raised, within a bounded deadline. If the previous owner does
not raise one in time, the transaction SHALL start this runtime's tunnel again
from the signed configuration and place the claim again. A machine with no
tunnel and no owner until a person reads the terminal is the arrangement the
handover refused, and it is no better in this direction.

Aborting a completed handover SHALL remain what it is — an answer that nothing
is in flight — and SHALL NOT give the tunnel back. The two are different acts,
and a command that did either depending on what it found would be a command
nobody could predict.

#### Scenario: The previous owner takes the tunnel

- **WHEN** this runtime holds the claim and runs the tunnel, and release is run
- **THEN** its process is stopped, then the claim is released, and the transaction completes on consecutive traversals through the tunnel the previous owner raised, with one tunnel process running

#### Scenario: The previous owner does not take it

- **WHEN** no traversal is proven within the deadline after the claim was released
- **THEN** this runtime starts its tunnel again from the signed configuration, places the claim again, and reports that the tunnel was not handed back

#### Scenario: The terminal dies mid-release

- **WHEN** a release is interrupted after its own process was stopped or after the claim was released
- **THEN** a later invocation finds the phase and undoes exactly it, leaving a running tunnel with exactly one owner

#### Scenario: Abort after a completed handover

- **WHEN** abort is run and no transaction is in flight
- **THEN** it reports that nothing was in flight and changes nothing, and does not give the tunnel back
