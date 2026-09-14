# Tunnel Supervision Delta

## MODIFIED Requirements

### Requirement: The causes are the ones that keep a machine on the network

The runtime SHALL reach a decision to rebuild the tunnel when the tunnel process
is gone, when the gap between two cycles reaches the wake threshold, or when the
interface carrying the upstream probe address or any ingress target has changed.
These are the three causes the runtime that owns the tunnel acts on in its own
definitions, and the executor this rule is written for reproduces that runtime
rather than improving on it.

The runtime SHALL NOT decide to rebuild the tunnel when connectivity returns or
when the payload path fails, and SHALL NOT decide to reapply routes when they
drift. It SHALL still observe all three and record them as grounds. The owning
runtime does not rebuild on a failed health probe or a returned link, by recorded
decision; its payload rebuild follows a failover this runtime does not perform;
and its routes stay its own. A rule that acted on them would disagree with the
runtime it is proved against on every occasion they occurred — measured before
this change, 125 rebuilds decided where 2 were made.

Absence SHALL be counted rather than observed. Connectivity counts as absent
only once the outer path has been unreachable for a configured number of
consecutive complete cycles, and as present again on the first cycle that
reaches it. A cycle that did not reach the probes SHALL neither add to the count
nor reset it, and a runtime that has not yet decided anything SHALL treat the
link as present rather than absent, so that its first successful probe is not a
return.

A wake gap SHALL be the configured cycle interval plus the time the machine
spent asleep, compared with the threshold inclusively. The owning runtime compares
the wall time between the starts of two ticks that sleep sixty seconds apart, so
what it compares is the interval and any sleep; comparing the sleep alone would
miss every sleep that falls short of the threshold by less than an interval.

The sleep SHALL be measured from the divergence of two clocks, one that advances
while the machine sleeps and one that does not, and not inferred from the time
between observations. A runtime that is slow is not a machine that slept.
Measured on 2026-09-12: the first fold after a reinstall cost 32.4 seconds and a
wake gap was named on a machine that had been awake throughout. The work of a
cycle is therefore left out of the gap, and where the owning runtime's own tick is
slow enough to cross the threshold without a sleep, the two will disagree; that is
for the comparison to find.

The carrier SHALL be which interface carries the upstream probe address and each
ingress target, and nothing else. A signature over every configured destination
changes whenever a fallback route comes or goes and whenever the configuration
gains a destination, neither of which is a change of carrier; the previous
signature counted both.

#### Scenario: The process is gone

- **WHEN** the tunnel process is not running
- **THEN** the decision is to rebuild, naming the process

#### Scenario: A wake gap

- **WHEN** the configured interval plus the measured sleep reaches the wake threshold
- **THEN** the decision is to rebuild, naming the gap

#### Scenario: A sleep shorter than the threshold still makes a gap

- **WHEN** the machine slept for less than the threshold, and the interval plus that sleep reaches it
- **THEN** a wake gap is named, as the owning runtime's tick gap would name it

#### Scenario: The observer was slow and the machine was awake

- **WHEN** more than the wake threshold passes between two observations while the machine stayed awake
- **THEN** no wake gap is named and no rebuild is decided

#### Scenario: The carrier changed

- **WHEN** the interface carrying the upstream probe or an ingress target differs from the previous cycle's
- **THEN** the decision is to rebuild, naming the carrier

#### Scenario: A fallback route comes and goes

- **WHEN** a route to a destination that is neither the upstream probe nor an ingress target appears, disappears or moves
- **THEN** the carrier has not changed and no rebuild is decided

#### Scenario: Connectivity returned

- **WHEN** the outer path has been unreachable for the configured number of consecutive cycles and is then reached
- **THEN** no rebuild is decided, and the return is recorded in the grounds

#### Scenario: A single probe fails and the next succeeds

- **WHEN** the outer path is unreachable on one cycle and reachable on the next, below the threshold
- **THEN** no cause is named and no rebuild is decided

#### Scenario: A cycle stops before the probes while the link is absent

- **WHEN** a cycle does not complete
- **THEN** it neither adds to the count of consecutive failures nor resets it

#### Scenario: The payload path failed

- **WHEN** the payload probe has failed for the configured number of consecutive cycles
- **THEN** no rebuild is decided, and the failure is recorded in the grounds

#### Scenario: Routes drifted

- **WHEN** the observed routes differ from the planned ones
- **THEN** no action is decided, and the drift is recorded in the grounds

#### Scenario: More than one cause is present

- **WHEN** several causes hold in the same cycle
- **THEN** one decision is reached and it names every cause that held, because a reader comparing it against another runtime needs to know which of them that runtime saw

## ADDED Requirements

### Requirement: The rule is judged against the owning runtime by a program

Whether the rule reproduces the owning runtime SHALL be decided by a program
rather than by reading records side by side. The judgement is what gives a
runtime authority over the network, and a judgement made by hand is one nobody
can run twice.

The program SHALL pair this runtime's rebuild decisions with the owning runtime's
rebuilds by cause, inside a window of two cycles, and SHALL report agreements and
disagreements in both directions: a rebuild decided and not made, and a rebuild
made and not decided. It SHALL count agreements on events the operator induced
apart from natural ones, from a record the operator keeps as they induce them,
because neither runtime's record can tell a killed process from a crashed one.

A decision record SHALL carry the tick gap its wake cause was compared on, so
that a disagreement about a wake can be read from the record that made it.
Records written before it carried one SHALL remain readable.

The soak SHALL pass only after at least seven days with no disagreement, at
least three natural wake-gap agreements, two carrier agreements and one induced
process loss. A change to the rule SHALL restart the seven days.

A stretch in which this runtime wrote nothing for longer than three cycles SHALL
make the soak not judgeable, wherever it falls: at the start of a collection,
between two, or inside one. Nothing decided there can disagree with anything. A
collection that did not measure the silences inside it SHALL NOT count as having
observed its window.

#### Scenario: A rebuild decided and made

- **WHEN** this runtime decides a rebuild for a cause and the owning runtime rebuilds for the same cause inside the window
- **THEN** it is one agreement for that cause

#### Scenario: A rebuild decided and not made

- **WHEN** this runtime decides a rebuild and the owning runtime makes none for that cause inside the window
- **THEN** it is a disagreement, and the soak does not pass

#### Scenario: A rebuild made and not decided

- **WHEN** the owning runtime rebuilds and this runtime decided none for that cause inside the window
- **THEN** it is a disagreement, and the soak does not pass

#### Scenario: An induced loss

- **WHEN** the operator has recorded inducing a cause and both runtimes agree on it inside the window
- **THEN** it counts as an induced agreement and not as a natural one

#### Scenario: An old record

- **WHEN** a decision recorded before the tick gap was carried is read
- **THEN** it is read without a tick gap rather than refused
