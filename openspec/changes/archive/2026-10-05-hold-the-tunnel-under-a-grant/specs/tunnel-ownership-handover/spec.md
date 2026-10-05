# Tunnel Ownership Handover Specification Delta

## ADDED Requirements

### Requirement: A supervisor running a script that is no longer installed is refused

The preflight SHALL refuse a handover while the supervisor holding the tunnel
started before the script installed for it. Such a supervisor is running bytes
nobody can read from disk, and every claim the preflight makes about how it will
behave under a claim is a claim about a file it is not executing.

This SHALL be established from what can be observed from outside the other
runtime: when its process started, against when its script was last written.
Nothing in the runtime being taken from is changed to answer it. The weakness is
recorded rather than hidden — an install that preserved the script's timestamp
would satisfy a check that only compares times.

The handover SHALL restart that supervisor as a process before the exchange, and
the same comparison SHALL then prove the restart took. Measured 2026-09-25, the
supervisor on this machine had been running since 2026-09-14 11:14 against a
script installed at 16:07 the same day — eleven days on bytes that were replaced
within hours.

A restart SHALL preserve the settings the supervisor runs under. They come from a
file it reads at start rather than from its launch definition, so a restart
through its own launcher keeps them; the preflight SHALL confirm the value it
depends on from what the restarted supervisor announces, not from the file's
default. On this machine the default in the script is a ten-second health
interval and the running value is sixty.

#### Scenario: The running supervisor predates its script

- **WHEN** the supervisor holding the tunnel started before the script installed for it was written
- **THEN** the preflight refuses, naming both times

#### Scenario: The supervisor is restarted

- **WHEN** the handover restarts that supervisor and it comes back
- **THEN** the preflight passes only if the new process started after the script was written, and if it announces the settings the handover expects

### Requirement: The runtime may give the tunnel back on its own

Giving the tunnel back SHALL be available to the runtime as well as to the
operator, and SHALL be the same transaction in both cases. A runtime that has
decided it should not hold the tunnel is in no position to invent a second way of
letting go of it.

Taking the tunnel SHALL remain an act of the operator alone. A handover
interrupts traffic for as long as the exchange takes, and a runtime that took the
tunnel back on its own — on a renewed grant, or after a guard was cleared —
would do that at a moment nobody chose.

`resume` SHALL clear what stopped the runtime from acting and SHALL NOT take the
tunnel. The two are separate acts because one is safe at any moment and the other
is not.

Whether the tunnel was given back SHALL be read from the claim and not from
whether the transaction completed. A giving-up does not take the tunnel back, so
after releasing the claim it waits for the other runtime's tunnel to carry
traffic and reports a failure when it does not — measured 2026-09-26, the claim
was released, the previous owner raised its own within the minute, and the record
said the tunnel had not been given back. A claim that cannot be read SHALL say
nothing was given: a runtime that cannot read it has not shown it let go.

#### Scenario: The runtime hands the tunnel back

- **WHEN** the runtime reaches a condition that ends its ownership
- **THEN** it performs the release transaction, with the same phases and the same completion on traffic as the operator's

#### Scenario: The release let go and could not prove it afterwards

- **WHEN** the runtime releases the claim and the other runtime's tunnel does not carry traffic inside the deadline
- **THEN** the record says the tunnel was given back, because the claim is gone

#### Scenario: The grant is renewed after a handback

- **WHEN** the grant becomes valid again and this runtime holds no tunnel
- **THEN** it observes, and takes nothing until the operator runs the handover

#### Scenario: The operator resumes a stopped runtime

- **WHEN** the operator resumes this runtime after a guard trip
- **THEN** acting is allowed again and the tunnel stays where it is
