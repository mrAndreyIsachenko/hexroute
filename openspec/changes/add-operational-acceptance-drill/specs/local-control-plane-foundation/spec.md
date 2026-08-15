## ADDED Requirements

### Requirement: Operational acceptance gate before runtime ownership changes

Future Hexroute runtime cutover, production adapter activation, ownership
transfer or failover enablement SHALL require a passing operational acceptance
drill for the relevant candidate state before the change can proceed beyond
observe-only qualification.

#### Scenario: Runtime change lacks acceptance evidence

- **WHEN** a future change proposes production mutation authority, ownership transfer or failover enablement without current operational acceptance evidence
- **THEN** the change is blocked before cutover
- **AND** Twilight remains the production owner

#### Scenario: Acceptance evidence is present

- **WHEN** a future runtime or cutover change cites current passing operational acceptance evidence
- **THEN** the change may continue through its own safety and rollback gates
- **AND** the acceptance evidence does not replace the change-specific qualification requirements
