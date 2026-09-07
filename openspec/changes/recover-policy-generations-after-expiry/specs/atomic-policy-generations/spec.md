## MODIFIED Requirements

### Requirement: Local authorization suspension

Each daemon SHALL maintain an `authorization_suspended` overlay independent of
policy generations. Active-bundle corruption, signature or digest mismatch,
domain mismatch, clock anomaly or IPC ownership violation SHALL suspend new
mutations, preserve the existing data plane and emit a bounded incident. The
overlay SHALL clear only after local revalidation and SHALL never expand policy.
Its bounded reason SHALL be one of `corruption`, `invalid_signature`,
`digest_mismatch`, `domain_mismatch`, `clock_anomaly` or `ipc_ownership`, and
the overlay SHALL NOT replace the active lifecycle state or generation identity.

Every cause of suspension SHALL be a local fault that an operator action can
clear. An active generation reaching its own `expires_at` SHALL NOT suspend
authorization and SHALL NOT be reported as a clock anomaly: the clock is
correct, the generation is over, and no local correction exists.

#### Scenario: Active payload is corrupted on disk

- **WHEN** periodic or startup validation detects an active payload digest mismatch
- **THEN** the daemon suspends new authorization immediately
- **AND** it does not manufacture or activate a replacement generation

#### Scenario: Valid deny generation is activated

- **WHEN** an emergency policy is needed to revoke authorization
- **THEN** the operator compiles and signs a normal deny generation
- **AND** load failure or suspension is not treated as a substitute policy

#### Scenario: The active generation expires

- **WHEN** the active generation's validity window ends while the clock is sound
- **THEN** no suspension is raised and no clock anomaly is reported
- **AND** new mutations remain refused because no generation is active

### Requirement: Two-level monotonic generations

The effective snapshot SHALL carry one monotonic `bundle_generation` and
independent monotonic `root_policy_generation` and `user_policy_generation`
values. A domain action SHALL bind the bundle and its owning domain generation,
and a cross-domain action SHALL require both daemons to report the same bundle.

Monotonicity SHALL NOT be broken by the passage of time or by a change of static
authority. A generation whose validity has ended, or which was compiled against
a superseded safety envelope, SHALL still establish the parent of its successor
for as long as its stored evidence verifies. Resetting the chain SHALL NOT be
the recovery path for either event.

#### Scenario: Only user policy changes

- **WHEN** a semantic policy change affects only the user payload
- **THEN** the bundle and user policy generations advance
- **AND** the root policy generation and root payload digest remain unchanged

#### Scenario: Domains report different active bundles

- **WHEN** root and user daemons report different active bundle generations
- **THEN** new local mutations are blocked
- **AND** the existing data plane remains running

#### Scenario: The safety envelope gains a capability

- **WHEN** a new capability changes the compiled static digest
- **THEN** the next generation continues the existing chain rather than starting a new one
- **AND** the daemons still require a reviewed static installation and restart before that generation can govern
