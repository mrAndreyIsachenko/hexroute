# Tunnel Supervision Delta

## MODIFIED Requirements

### Requirement: The causes are the ones that keep a machine on the network

The runtime SHALL reach a decision to rebuild the tunnel when the sing-box
process is gone, when the machine has slept longer than the wake threshold,
when the set of interfaces carrying the configured destinations has
changed, when connectivity has returned after being absent past its threshold,
or when the payload path has failed past its threshold. It SHALL reach a
decision to reapply routes when the observed routes differ from the planned
ones.

Absence SHALL be counted rather than observed. Connectivity counts as absent
only once the outer path has been unreachable for a configured number of
consecutive complete cycles, and as present again on the first cycle that
reaches it. A cycle that did not reach the probes SHALL neither add to the count
nor reset it, and a runtime that has not yet decided anything SHALL treat the
link as present rather than absent, so that its first successful probe is not a
return.

The threshold is not decoration. Measured on this machine over seven days, the
outer probe failed on 25 of 3,117 cycles and never twice in a row; without a
threshold every one of those was an absence and every following cycle was a
return. Six of them fell in one half hour on 2026-09-12, each asking for a
rebuild of a tunnel that was working, while the runtime that owns that tunnel
did nothing at all in the same window.

A wake gap SHALL be the time the machine spent asleep, measured rather than
inferred from an absence of observations. A runtime that is slow is not a
machine that slept, and the two SHALL NOT be reported as the same thing.

The rule SHALL obtain it from the divergence of two clocks: one that advances
while the machine sleeps and one that does not. Where the platform offers no
such pair the cause SHALL NOT hold, because a gap that cannot be attributed to
sleep is not evidence of one.

Measured on 2026-09-12: the first fold after a reinstall cost 32.4 seconds, the
loop then waited its interval, and ninety-three seconds passed between two
observations against a threshold of ninety. The cycle named a wake gap on a
machine that had been awake throughout. With authority that decision rebuilds a
working tunnel every time this daemon is installed.

These are the six the production supervisor acts on, measured from its own log
rather than derived from its source. Two of them have not occurred in
sixty-one days and are kept: the process exiting is the only thing between this
machine and no network at all, and connectivity returning is how a laptop comes
back from a dead link.

#### Scenario: The process is gone

- **WHEN** the sing-box process is not running
- **THEN** the decision is to rebuild, naming the process

#### Scenario: A wake gap

- **WHEN** the machine has been asleep for longer than the configured wake threshold
- **THEN** the decision is to rebuild, naming the gap

#### Scenario: The observer was slow and the machine was awake

- **WHEN** more than the wake threshold passes between two observations while the machine stayed awake
- **THEN** no wake gap is named and no rebuild is decided

#### Scenario: The carrier changed

- **WHEN** the interfaces carrying the configured destinations differ from the previous cycle's
- **THEN** the decision is to rebuild, naming the carrier

#### Scenario: Connectivity returned

- **WHEN** the outer path has been unreachable for the configured number of consecutive cycles and is then reached
- **THEN** the decision is to rebuild, naming the return

#### Scenario: A single probe fails and the next succeeds

- **WHEN** the outer path is unreachable on one cycle and reachable on the next, below the threshold
- **THEN** no cause is named and no rebuild is decided

#### Scenario: A cycle stops before the probes while the link is absent

- **WHEN** a cycle does not complete
- **THEN** it neither adds to the count of consecutive failures nor resets it

#### Scenario: The payload path failed

- **WHEN** the payload probe has failed for the configured number of consecutive cycles
- **THEN** the decision is to rebuild, naming the payload path

#### Scenario: More than one cause is present

- **WHEN** several causes hold in the same cycle
- **THEN** one decision is reached and it names every cause that held, because a reader comparing it against another runtime needs to know which of them that runtime saw
