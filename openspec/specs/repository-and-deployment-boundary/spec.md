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
