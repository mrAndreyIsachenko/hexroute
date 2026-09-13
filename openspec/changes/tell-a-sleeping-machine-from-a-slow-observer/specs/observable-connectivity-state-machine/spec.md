# Observable Connectivity State Machine Delta

## ADDED Requirements

### Requirement: The observation loop aims at a period

The runtime SHALL schedule each observation relative to when the previous one
began rather than to when its work finished, so that a cycle which overran does
not push the next observation out by however long it took. A cycle that has
already consumed its period SHALL begin the next observation immediately rather
than waiting a further period.

This is not about speed. The interval between observations is the quantity the
tunnel decision compares against a wake threshold, and a loop that adds its own
work to that interval reports a gap that nothing outside the runtime caused.
Measured on 2026-09-12: a fold costing 32.4 seconds turned a sixty-one second
period into a ninety-three second gap, which crossed a ninety second threshold.

#### Scenario: A cycle takes longer than its period

- **WHEN** an observation and everything it writes take longer than the configured interval
- **THEN** the next observation begins immediately rather than after a further interval

#### Scenario: A cycle finishes early

- **WHEN** a cycle finishes well inside its period
- **THEN** the next observation begins one period after the previous one began, not one period after this one ended
