## ADDED Requirements

### Requirement: Sentinel recovery is observed before it is authorized

While the sentinel has no recovery authority it SHALL still run the planner
that would decide a recovery, and SHALL record what it would have done. The
record SHALL name the phase the planner is in, the action it selected, and
that no action was taken. A sentinel without authority SHALL hold no means of
acting: it SHALL be constructed without a restarter rather than with one it
declines to use.

The bound SHALL be observable. Reaching the point where an authorized sentinel
would have stopped after its one permitted attempt SHALL be recorded as that
point, distinctly from continuing to observe.

#### Scenario: Both evidence sources fail while the sentinel is observing

- **WHEN** stale root heartbeat and failed data-path evidence meet the gate and the sentinel has no authority
- **THEN** it records the action it would have taken and the phase it moved to
- **AND** it takes no action, and holds no restarter that could have taken one

#### Scenario: An observing sentinel reaches its attempt bound

- **WHEN** the planner reaches the point at which one permitted attempt would have been spent
- **THEN** that is recorded as the bound being reached rather than as another cycle of evidence
- **AND** the sentinel continues to observe without acting

#### Scenario: Evidence is incomplete

- **WHEN** only one of the two evidence sources fails
- **THEN** the planner selects no action
- **AND** what is recorded says which phase it remains in
