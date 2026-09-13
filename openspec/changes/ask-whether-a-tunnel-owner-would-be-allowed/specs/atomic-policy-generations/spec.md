# Atomic Policy Generations Delta

## ADDED Requirements

### Requirement: Owning the tunnel is a capability of its own

The policy model SHALL carry a capability for owning the tunnel, distinct from
every other, and it SHALL be grantable to the root domain and to no other. The
user domain holds a keychain and a one-time code; it has no business restarting
a tunnel, and a model that allowed the grant would let a generation say so.

The capability SHALL cover rebuilding the tunnel and reapplying its routes as
one grant rather than two. They are two acts of one ownership, and two
capabilities could be revoked one at a time — leaving a runtime permitted to tear
the tunnel down and not to put its routes back, which is worse than either.

A runtime SHALL be able to ask whether the capability would authorize an action
without performing it, and the answer SHALL be recorded. A grant first read at
the moment another runtime is booted out is read where being wrong costs the
network.

#### Scenario: A generation grants tunnel ownership to the user domain

- **WHEN** a candidate generation grants the tunnel capability to the user domain
- **THEN** it is refused at compile time

#### Scenario: A runtime asks before any generation grants it

- **WHEN** the runtime asks whether it would be authorized and no active generation carries the capability
- **THEN** the answer is a refusal naming the reason, and nothing is performed

#### Scenario: A generation granting it becomes active

- **WHEN** a generation carrying the capability is active and the runtime asks
- **THEN** the answer is an authorization, and nothing is performed until a change grants an executor
