## ADDED Requirements

### Requirement: Lineage survives what authority does not

A generation's place in the chain SHALL be derivable from the local store for as
long as the store's evidence is intact, whether or not that generation may still
govern. The derivation SHALL verify the signature against the pinned key, every
artifact digest, the immutable artifacts and the trusted compiler, and SHALL NOT
compare the predecessor's validity window or static digest against the present.

The result SHALL carry generation numbers, the payload digest, the policy schema
and the predecessor's own validity and static digest, and SHALL NOT carry the
manifest, the payload or the approval, so that it cannot be used to authorize.

Deriving the current generation SHALL NOT fall back to an operator-declared
value when the store holds intact evidence.

#### Scenario: The predecessor has expired

- **WHEN** the active generation's validity window has passed and its artifacts are intact
- **THEN** its generation numbers and payload digest are still derivable
- **AND** the successor can be installed with that generation as its parent

#### Scenario: The safety envelope changed under the predecessor

- **WHEN** the installed static digest differs from the one the active generation was compiled against
- **THEN** lineage is still derivable, because which generation came before is not a question about the current envelope
- **AND** the candidate being installed is still refused if its own static digest does not match the installed one

#### Scenario: The predecessor's evidence is damaged

- **WHEN** a digest, a signature or an immutable artifact of the active generation does not verify
- **THEN** lineage is refused, because the chain is what is proven and nothing else establishes it

#### Scenario: A lapsed generation is offered for a decision

- **WHEN** any caller holds a derived lineage record
- **THEN** it cannot evaluate policy from it, because the record carries no payload, manifest or approval

### Requirement: Expiry ends authority without raising a fault

An expired active generation SHALL report `state: none` with reason `expired`
and SHALL NOT raise the authorization-suspension overlay. Mutations SHALL remain
refused for the absence of an active generation rather than for a suspension.

The reason SHALL distinguish a generation that lapsed from one that never
existed, because the first means the store holds a chain to build on.

#### Scenario: The active generation reaches its expiry

- **WHEN** the active generation's `expires_at` passes
- **THEN** the daemon reports `state: none, reason: expired` and raises no suspension
- **AND** no mutation is authorized, because there is no active generation

#### Scenario: No generation was ever installed

- **WHEN** the store holds no active pointer at all
- **THEN** the reported reason is `no_valid_generation` rather than `expired`

#### Scenario: A valid generation is activated after a lapse

- **WHEN** a successor generation is installed and confirmed
- **THEN** the daemon reports it active without requiring a restart

### Requirement: The end of validity is announced before it arrives

Both daemons SHALL report the active generation's `expires_at`. The user runtime
SHALL raise a bounded `policy_expiry` incident when remaining validity first
falls below seven days, again when it first falls below forty-eight hours, and
again once the generation has lapsed and has not been replaced.

The incident SHALL NOT be critical, so that it defers to the morning digest
inside the configured night window, and its text SHALL come from the closed
template allowlist and SHALL carry no policy content.

A crossing already announced SHALL stay announced across a restart. The record
of what was delivered SHALL outlive the process, because a suppression that
lasts only as long as the process announces the same crossing again on every
start, and an announcement repeated is an announcement stopped being read.

Only a delivery whose identity outlives the process SHALL be remembered. A
policy generation is such an identity; a runtime's state generation is not,
because it counts from zero when the daemon starts and a delivery remembered
against it would silence a different incident that reached the same number.

A record that cannot be read SHALL NOT prevent an announcement. Losing it costs
one repeated announcement; refusing to announce because a bookkeeping file is
damaged would lose the thing the announcement exists for.

#### Scenario: A generation approaches its expiry

- **WHEN** remaining validity first falls below seven days
- **THEN** one actionable non-critical incident is raised
- **AND** crossing the same threshold is not announced again for the same generation

#### Scenario: The daemon restarts after announcing

- **WHEN** the runtime restarts after a threshold was announced for a generation
- **THEN** that threshold is not announced again for that generation
- **AND** the next generation's crossing is still announced

#### Scenario: An incident is identified by something that restarts with the process

- **WHEN** an incident's generation counts from zero at start rather than naming a policy generation
- **THEN** the delivery is not remembered across a restart, so a later incident reaching the same number is still announced

#### Scenario: The record of what was delivered cannot be read

- **WHEN** the durable delivery record is missing or damaged
- **THEN** the announcement is made rather than withheld

#### Scenario: A generation lapses unreplaced

- **WHEN** the active generation has expired and no successor is active
- **THEN** the state remains announced rather than silent

#### Scenario: A threshold is crossed at night

- **WHEN** a threshold is crossed inside the configured night window
- **THEN** delivery waits for the morning digest rather than waking the operator

### Requirement: Installed policy configuration is checkable before installation

An operator SHALL be able to validate a prepared daemon policy configuration
offline, before it is installed, using the same validation the daemon applies
rather than a second statement of the rules.

#### Scenario: A prepared configuration is malformed

- **WHEN** the offline check runs against a configuration the daemon would reject
- **THEN** it reports the rejection without the daemon being restarted

#### Scenario: A prepared configuration is sound

- **WHEN** the offline check accepts a configuration
- **THEN** the daemon accepts the same file, because both ask the same function
