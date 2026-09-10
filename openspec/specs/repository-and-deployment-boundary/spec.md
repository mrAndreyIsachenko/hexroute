# Repository And Deployment Boundary Specification

## Purpose

Keep reusable public Hexroute artifacts separate from live provider deployment
state, secrets and legacy production ownership.

## Requirements

### Requirement: Public repository contains no live deployment state

The public repository SHALL contain generic code, schemas, synthetic fixtures,
provider-neutral modules and documentation, and SHALL NOT contain live
Terraform roots, state, provider credentials, private endpoints or raw evidence.

#### Scenario: Public repository is scanned

- **WHEN** repository policy checks the working tree and Git history
- **THEN** tracked state, plan files, credentials and prohibited live deployment values are rejected

### Requirement: Private infrastructure owns live roots

Provider-specific Terraform roots, HCP workspace references, secret identifiers
and private deployment evidence SHALL remain in `hexroute-infra`.

#### Scenario: A reusable Terraform capability is added

- **WHEN** a module has no live identity or provider-account binding
- **THEN** the reusable contract belongs in the public repository
- **AND** the live instantiation remains private

### Requirement: Immutable non-root cloud image

Cloud API, worker and migrator modes SHALL run from reviewed immutable image
content as non-root with a read-only root filesystem and explicit writable paths.

#### Scenario: Deployment selects an image

- **WHEN** infrastructure references a release
- **THEN** it uses immutable image content rather than a mutable tag alone
- **AND** runtime filesystem and user restrictions remain enabled

### Requirement: Legacy ownership remains explicit

Until transactional cutover passes its separate change, Twilight SHALL remain
the sole production owner of sing-box, routes and Keychain-backed Pritunl
recovery.

#### Scenario: Pre-cutover Hexroute is installed

- **WHEN** Hexroute runs beside Twilight
- **THEN** Twilight production labels, files, state and processes remain unchanged
- **AND** Hexroute does not stop, disable or reconfigure AdGuard

### Requirement: Future work uses bounded changes

Provider B, Telegram migration, signed delivery, root cutover and user cutover
MUST be planned as separate OpenSpec changes with explicit rollback and soak
criteria.

#### Scenario: A future phase is proposed

- **WHEN** implementation would cross one of the roadmap ownership boundaries
- **THEN** it cannot reuse the archived umbrella change as implementation authority
- **AND** a repository-owned change defines its affected capability and rollback

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
