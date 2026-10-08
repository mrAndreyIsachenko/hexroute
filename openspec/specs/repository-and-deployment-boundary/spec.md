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

### Requirement: The repository carries the shape of every configuration a runtime reads

For each runtime it installs, the public repository SHALL carry an example
configuration holding every setting that runtime's decoder accepts, so the
configuration can be reconstructed, reviewed and compared against without
reading the one host that runs it.

A setting a decoder accepts and its example omits SHALL fail a gate, and so
SHALL a setting an example carries and the decoder no longer accepts. The gate
SHALL take the settings from the decoder itself rather than from a list written
beside it, because a list is the thing that was not updated: measured
2026-10-08, the root example carried 46 settings against the 112 the installed
configuration held, with no `tunnel_supervision` and no `policy_control` at all,
so ten settings — the whole tunnel executor — existed only on one machine.

The example SHALL hold placeholder values and no live ones, and SHALL be
accepted by the same decoder the runtime uses once the trust material a
deployment supplies has been supplied, so that the record is a template and not
an inventory.

The repository SHALL NOT hold trust material at all, not even a placeholder:
that boundary is already drawn and is stricter than a placeholder rule, because
a key-shaped string in a tracked artifact is the leak whether or not it opens
anything. So the example SHALL carry such a setting as a key with an empty
value — the shape, recorded, and nothing else — and whatever needs a
configuration a runtime will load SHALL supply the material itself.

This is the boundary stated from the side that was missing. The requirement that
the public repository hold no live deployment state says what may not be there;
this says what must: the shape, never the values.

#### Scenario: A setting is added to a runtime

- **WHEN** a decoder gains a setting and the example does not
- **THEN** the gate refuses and names the setting, before any install is attempted

#### Scenario: A setting is removed from a runtime

- **WHEN** a decoder no longer accepts a setting the example still carries
- **THEN** the gate refuses, so the record cannot describe a configuration the runtime would reject

#### Scenario: The example is read by the decoder

- **WHEN** the runtime's own decoder is given the example with the trust material a deployment supplies supplied
- **THEN** it accepts it, because the record is the same kind of thing as the file it stands for

#### Scenario: A setting is trust material

- **WHEN** the example must record a key or a fingerprint
- **THEN** it carries the setting with an empty value, so the shape is recorded and the repository holds no key-shaped string at all

#### Scenario: A domain has no trust material to record

- **WHEN** a decoder accepts no trust setting
- **THEN** its example carries none either, and the absence is checked rather than passed over

#### Scenario: The comparison before an install has a record

- **WHEN** an operator compares a prepared configuration against the installed one
- **THEN** the repository holds the shape both are instances of, so a missing setting is visible in the repository rather than only on the host
