# Tunnel Ownership Handover Delta

## ADDED Requirements

### Requirement: One owner at a time, named on disk

Exactly one runtime SHALL own the tunnel process at any moment, and which one
SHALL be a fact on disk rather than an assumption in either.

The owning claim SHALL be readable by both runtimes, SHALL be consulted by the
previous owner on every tick and before every start of the process, and SHALL
survive that owner being restarted. The supervisor is kept alive by launchd and
comes back within ten seconds of exiting; a claim it learned only at startup
would be forgotten exactly when it matters.

Only the operator's transaction SHALL write the claim, and abandoning the
transaction SHALL remove it. Neither daemon SHALL write it: a daemon that could
claim ownership could claim it while the other still held the process.

#### Scenario: The previous owner restarts mid-handover

- **WHEN** the supervisor exits and is restarted while the claim is held
- **THEN** it reads the claim before starting the process, and does not start it

#### Scenario: The claim is absent

- **WHEN** no claim is on disk
- **THEN** the supervisor owns the process as it always has, and the new runtime performs nothing

#### Scenario: A daemon tries to claim ownership

- **WHEN** anything other than the operator's transaction writes the claim
- **THEN** it is refused

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

#### Scenario: A rehearsal takes nothing

- **WHEN** the transaction is rehearsed while a tunnel process is running
- **THEN** nothing is signalled, for the same reason a rehearsal starts nothing

### Requirement: The handover completes on traffic, not on an interface

The transaction SHALL complete only on evidence that traffic traversed the
tunnel, and SHALL treat an interface that came up as no evidence at all.

Completion SHALL require more than one such proof in succession, on the same
reasoning that gave the payload cause a threshold: a single answer says what one
moment was like. The transaction SHALL bound its own wait and SHALL abandon
rather than wait longer.

Abandoning SHALL return the process to the previous owner rather than leave it
unowned. A machine with no owner for the tunnel has no network for as long as
nobody is looking at the terminal.

#### Scenario: Traffic traverses twice

- **WHEN** two consecutive payload probes prove traversal inside the deadline
- **THEN** the handover completes and the claim stands

#### Scenario: The deadline passes

- **WHEN** the deadline passes without two consecutive proofs
- **THEN** the claim is removed, the process this runtime started is stopped, and the previous owner takes it back

#### Scenario: The interface came up and carried nothing

- **WHEN** the tunnel interface is present and the payload does not traverse
- **THEN** the handover does not complete on that evidence

### Requirement: A transaction outlives the terminal that started it

The transaction SHALL record its phase durably as it advances, so that a
terminal closing does not leave a machine whose owner nobody can determine.

The record SHALL carry what an abort needs and nothing else: which phase was
reached, what the previous owner's state was, and what this runtime started. It
SHALL be readable by a later invocation, which SHALL be able to complete or
abort what it finds.

#### Scenario: The terminal dies mid-transaction

- **WHEN** the operator's session ends while a transaction is in flight
- **THEN** a later invocation reads the phase reached and can abort it to the previous owner

#### Scenario: A transaction is already in flight

- **WHEN** a transaction is started while another's record is present
- **THEN** it is refused rather than started beside it

### Requirement: The configuration the tunnel runs is signed

The bytes sing-box runs SHALL be a signed configuration version, verified
against the host it is for, at every start rather than once at installation.

The first version SHALL be byte for byte what the machine runs today, so that
the handover changes who owns the tunnel and nothing about what it is. The
verification SHALL NOT reach the network: a host needs its tunnel configuration
exactly when it has none.

A version that does not verify SHALL NOT be started, and ownership SHALL return
to the previous owner. Falling back to a file on disk would make the signature
decorative — anyone able to write that file could bypass it by breaking the
verification.

#### Scenario: The version verifies

- **WHEN** the signed version verifies against this host
- **THEN** its content is what the process is started with

#### Scenario: The version does not verify

- **WHEN** the signature, the digest or the target does not hold
- **THEN** the process is not started and ownership returns to the previous owner

#### Scenario: The first version is published

- **WHEN** the first version is published
- **THEN** its content is byte for byte the configuration already running

### Requirement: The transaction can be rehearsed

The transaction SHALL support a rehearsal that performs every phase except
claiming ownership and starting the process, and SHALL say plainly which it
was.

Every part of this runtime that has been applied to the live machine for the
first time has been wrong about something, and the transaction is the part whose
first mistake costs the network. A rehearsal exercises the envelope, the
verification, the probes and the abort where being wrong costs nothing.

#### Scenario: A rehearsal runs

- **WHEN** the transaction is run as a rehearsal
- **THEN** no claim is written, no process is started, and the report says it was a rehearsal

#### Scenario: A rehearsal fails a phase

- **WHEN** a phase fails during a rehearsal
- **THEN** it is reported exactly as it would be in a real transaction
