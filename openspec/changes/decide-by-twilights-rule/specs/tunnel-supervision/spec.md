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

A wake gap SHALL be the wall time between two cycles of the same runtime
process, compared with the threshold inclusively. The owning runtime reads the
wall clock after each sixty-second sleep and compares it with the previous tick's,
so what it compares is the interval, the previous tick's work and any time the
machine was asleep, and nothing in it tells those apart.

This runtime measured the sleep instead, as the divergence of a clock that stops
while the machine sleeps from one that does not, so that a slow runtime would not
look like a sleeping machine. That clock does not stop for every sleep this
machine takes. With `pmset sleepnow` on 2026-09-13 it stopped for all but one
second of a sleep of two and a half minutes; across idle sleeps of 136, 997 and
394 seconds on 2026-09-14 it did not stop at all, and over 2,365 recorded
decisions the largest sleep it measured was 115 milliseconds. A rule built on it
decides no wake the owning runtime rebuilds on. The sleep SHALL still be recorded
as a ground, and SHALL decide nothing.

A runtime slow enough that two of its cycles are the threshold apart therefore
names a wake gap, as the owning runtime does when its own tick is that slow. The
slowest cycle measured, a fold costing 32.4 seconds after a reinstall on
2026-09-12, made a gap of 93 seconds against this threshold of 180. The first
cycle of a process has no previous cycle and names no gap, so a reinstall is not
a wake.

The process SHALL count as gone when the tunnel process the previous cycle of
this runtime saw is no longer the one running, whether or not another has
replaced it. The owning runtime watches its own child and restarts it within
seconds, faster than a cycle: measured 2026-09-17, a tunnel process stopped at
11:38:09Z was noticed at 11:38:28Z and running again at 11:38:32Z, and a rule that
asked only whether a tunnel ran saw one on both cycles around it. The process a
cycle saw SHALL be remembered only in the running runtime, so the first cycle
after an installation compares nothing.

A process nobody looked at SHALL NOT be named gone. A cycle that could not take
the observation has seen nothing, and reading "not running" out of it named a
loss on every cycle a machine spent in dark wake: measured on the night of
2026-09-18, eight losses reported and none of them real.

The observations the causes are decided from SHALL be taken before any
observation that waits on a network, and a cycle SHALL take them whatever the
lid is doing. The owning runtime decides on a tick of a sixty-second sleep and a
few local checks, and it decides in the seconds a dark wake lasts. Measured over
that same night, a machine cycling on battery through 21 dark wakes: the owning
runtime rebuilt for six wakes, this runtime named eight, and only four of them
were the same event, because its cycle reached its decision after probes that
outlasted the wake. What waits on a network SHALL therefore be a ground only,
and a cycle that did not take such an observation SHALL carry the last answer
rather than invent one.

The carrier SHALL be which interface carries the upstream probe address and each
ingress target, and nothing else. A signature over every configured destination
changes whenever a fallback route comes or goes and whenever the configuration
gains a destination, neither of which is a change of carrier; the previous
signature counted both.

#### Scenario: The process is gone

- **WHEN** the tunnel process is not running
- **THEN** the decision is to rebuild, naming the process

#### Scenario: The process was replaced between two cycles

- **WHEN** a tunnel process is running and it is not the one the previous cycle saw
- **THEN** the decision is to rebuild, naming the process, and the grounds say it was replaced

#### Scenario: A cycle that could not look at the process

- **WHEN** a cycle did not take the process observation
- **THEN** no loss is named, and the grounds say the process was not observed

#### Scenario: A dark wake or a closed lid

- **WHEN** the machine is in a dark wake or its lid is closed
- **THEN** the cycle observes the tunnel process and the carrier and decides from them
- **AND** it proposes nothing for the network and waits on no probe, carrying the last payload answer as a ground

#### Scenario: A runtime's first cycle

- **WHEN** a runtime process observes the tunnel for the first time
- **THEN** no replacement is named, however the process came to be running

#### Scenario: A wake gap

- **WHEN** the wall time since the previous cycle of the same process reaches the wake threshold
- **THEN** the decision is to rebuild, naming the gap

#### Scenario: A sleep the steady clock did not see

- **WHEN** the machine slept, the clock that stops across sleep kept running, and the wall time between two cycles reaches the threshold
- **THEN** a wake gap is named, and the sleep recorded as a ground is what that clock measured

#### Scenario: A gap short of the threshold

- **WHEN** 179 seconds of wall time pass between two cycles at a threshold of 180
- **THEN** no wake gap is named

#### Scenario: The observer was slow and the machine was awake

- **WHEN** the wake threshold passes between two cycles while the machine stayed awake
- **THEN** a wake gap is named, as the owning runtime names one when its own tick is that slow

#### Scenario: The first cycle of a process

- **WHEN** a runtime process decides for the first time
- **THEN** no wake gap is named, however long ago another process decided

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

A silence SHALL count as observed instead when this runtime accounted for it in a
wake gap it decided: either the wake was decided within three cycles of the
silence ending, or the gap it was decided on covers the silence. A machine on
battery wakes for seconds and sleeps again, so the cycle that finishes and
decides can be several sleeps after the silence it names — measured 2026-09-20, a
silence of 46 minutes whose wake was decided 42 minutes after it ended, on a gap
of 88 minutes that spanned both. A decision that says how long its own gap was
has accounted for every silence inside it. A machine asleep writes nothing, and the soak
needs wakes; the same process ran on both sides of that silence, and the decision
it made about it is the one the comparison judges. A runtime stopped and started
again has no previous cycle and decides no wake, so its silence stays a hole.

#### Scenario: A sleep inside the soak

- **WHEN** no record was written for longer than three cycles and this runtime decided a wake gap within three cycles of the silence ending
- **THEN** the silence counts as observed, wherever it falls

#### Scenario: A sleep whose wake was decided long after

- **WHEN** no record was written for a stretch, and a wake gap decided later was decided on a gap that covers that stretch
- **THEN** the silence counts as observed

#### Scenario: A runtime restarted inside the soak

- **WHEN** no record was written for longer than three cycles and no wake gap was decided after it
- **THEN** the soak is not judgeable

A process-gone episode the owning runtime made no process rebuild for SHALL NOT be
a disagreement when the owning runtime restarted its tunnel within the window for
a reason of its own. Watching from outside, a restart replaces the process as a
loss does, and the owner's log says which it was. Such episodes SHALL be listed
as explained rather than dropped. Only a process-gone episode is explained so.

#### Scenario: The owner restarted its tunnel for another reason

- **WHEN** this runtime decided the process gone and the owning runtime restarted its tunnel inside the window for a carrier change, a wake gap or any reason other than a lost process
- **THEN** the episode is listed as explained and is not a disagreement

#### Scenario: Nothing explains the process-gone episode

- **WHEN** this runtime decided the process gone and the owning runtime neither rebuilt for a lost process nor restarted its tunnel inside the window
- **THEN** it is a disagreement

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
