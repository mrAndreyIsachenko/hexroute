# Tunnel Supervision Specification Delta

## MODIFIED Requirements

### Requirement: A tunnel owner's decision is reached before it is held

This runtime SHALL decide what a tunnel owner would do on every observation
cycle, and SHALL perform none of it unless it owns the tunnel, holds a grant of
`tunnel_ownership` that authorizes the act in the cycle that decided it, and is
not stopped by its own rate bound.

The decision SHALL be reached from the observations the cycle already takes,
and SHALL name which of its causes produced it rather than reporting only that
it would act. It SHALL be recorded before it is performed, so that a machine
that sleeps between the two leaves the record rather than the silence.

The rule SHALL NOT change in becoming performable. What was proved over seven
days against the runtime that owns the tunnel is what acts, including what that
proof left open.

A cycle that could not put the question to the grant SHALL be recorded as having
asked nothing, and SHALL NOT be recorded as refused. The question carries the
control state it was reached under, and a runtime whose first cycle has none
cannot ask: recording that as the grant refusing names the policy for a runtime
that had nothing to ask with. Measured 2026-09-26, a daemon two seconds old saw
its tunnel gone, held both the reason and the grant, and refused itself while
naming the grant — and because the claim was held, the previous owner was
standing down and the machine went about a minute with no tunnel.

#### Scenario: A cause is present

- **WHEN** an observation cycle finds a cause to rebuild the tunnel and this runtime holds no claim
- **THEN** the decision names that cause
- **AND** nothing is performed

#### Scenario: A cause is present and the tunnel is this runtime's

- **WHEN** an observation cycle finds a cause, this runtime owns the tunnel, the grant authorizes the act in that cycle and the rate bound is not reached
- **THEN** the decision is recorded and then performed

#### Scenario: A cause is present and nothing could be asked

- **WHEN** an observation cycle finds a cause before this runtime has a control state to ask the grant with
- **THEN** nothing is performed, and the record says the question was not asked rather than that it was refused

#### Scenario: No cause is present

- **WHEN** a cycle finds no cause
- **THEN** the decision is to do nothing, and that is recorded as a decision rather than as silence

## ADDED Requirements

### Requirement: The tunnel is observable whatever else the machine is running

The observation a tunnel's loss is decided from SHALL NOT be refused because of
how much else the machine is running. The process listing is bounded by nothing
this runtime controls — any user can lengthen their own command line — so it
SHALL be read without holding all of it, keeping only what could be a tunnel.

A line longer than an observation keeps SHALL be skipped rather than refusing
the listing it is part of. What the observation does keep and cannot hold SHALL
be a refusal rather than a shorter answer: a listing with lines missing can
report a tunnel absent when it is there, and absent is one of the two answers
the loss is decided from.

Measured 2026-10-05: the listing passed the cap on a command's output, every
cycle recorded that it could not observe the process, and for twenty minutes a
machine with no tunnel drew no cause from a runtime that held the claim keeping
the previous owner from starting one. Measured 2026-09-14, two unrelated
processes with long command lines had refused the same listing for a different
reason.

#### Scenario: The machine is running more than the cap holds

- **WHEN** the process listing is larger than a command's output may be
- **THEN** the tunnel is still found if it is running, and still reported absent if it is not

#### Scenario: Somebody else's command line is enormous

- **WHEN** an unrelated process has a command line longer than an observation keeps
- **THEN** that line is skipped and the rest of the listing is read

### Requirement: A record it cannot write costs the record

A runtime SHALL NOT end because it cannot name something it was going to write
down. A route whose role has no event of its own SHALL be reported as a degraded
cycle and passed over, and the cycle SHALL finish.

Measured 2026-10-05: the `inherited` role had no event, the route planner
proposes one whenever there is no tunnel to put the routes back on, and the
summary that could not name it ended the runtime. launchd restarted it and it
ended again, so for twenty minutes the machine had no tunnel while the claim was
still held — the previous owner stood down for it, and the one runtime that could
have rebuilt never finished a cycle.

Every role the configuration may carry SHALL have an event. The skipping above is
what keeps a role nobody has named yet from costing the runtime; it is not a
licence to leave one unnamed.

#### Scenario: A role with no event of its own

- **WHEN** a cycle's plan proposes a route whose role this runtime cannot name
- **THEN** the cycle is recorded as degraded, that route is passed over, and the cycle finishes

#### Scenario: Every configured role

- **WHEN** the configuration carries a route of any role this runtime accepts
- **THEN** that role has an event, and the proposal is recorded under it

### Requirement: A tunnel this runtime replaced is not a tunnel it lost

A change of the tunnel process that this runtime brought about SHALL NOT be read
as the loss its rule rebuilds for. Two bring it about: an exchange of ownership,
which replaces the process by design, and this runtime's own rebuild.

A cycle SHALL have no previous process to compare against when ownership changed
since the last one, exactly as the first cycle of a process has none. Ownership
is read from the configuration the tunnel's owner runs, which is this runtime's
while it holds the claim and the other runtime's otherwise.

A rebuild SHALL tell the memory a loss is decided from which process it put in
place. Measured 2026-09-26: the cycle thirty-eight seconds after a handover read
the exchange as a loss and rebuilt the tunnel it had just been given, and a
second rebuild a minute later reached the rate bound. That memory SHALL NOT be
durable, for the reason the rest of it is not: a process replaced while this
runtime was not running is not one it watched go.

#### Scenario: Ownership changes hands

- **WHEN** the first cycle after a handover sees a tunnel process that is not the one the previous cycle saw
- **THEN** no loss is named, and nothing is rebuilt

#### Scenario: This runtime rebuilt the tunnel itself

- **WHEN** the cycle after a rebuild sees the process that rebuild started
- **THEN** no loss is named

#### Scenario: A loss nobody here caused

- **WHEN** the tunnel process is replaced under one owner by something other than this runtime
- **THEN** the loss is named and the rule acts on it

### Requirement: A rebuild is stop, start, and the routes put back

A rebuild SHALL stop the tunnel process this runtime started, start one from the
signed configuration version, and restore on the new tunnel interface the host
routes that pointed at the old one.

The routes restored SHALL be those the runtime read before stopping, and no
others. The tunnel creates its own interface routes and recreates them itself;
those SHALL NOT be restored. The runtime SHALL NOT bring the machine to a route
plan of its own — the planner continues to propose and is applied nowhere — and
SHALL NOT touch a route that pointed anywhere but at the tunnel it stopped.

The interface a tunnel comes back on is not the one it left: measured across the
owning runtime's own rebuilds, the same address has been carried by seven
different tunnel interfaces. A restore that assumed the name would put the routes
back where nothing is listening.

#### Scenario: The tunnel returns on another interface

- **WHEN** a rebuild starts a tunnel and its interface differs from the one that was stopped
- **THEN** each host route that pointed at the old interface is created on the new one

#### Scenario: A route that was not the tunnel's

- **WHEN** host routes point at interfaces other than the tunnel being rebuilt
- **THEN** they are left untouched, whatever the runtime's own plan would propose for them

#### Scenario: A route cannot be restored

- **WHEN** a host route cannot be created on the new interface
- **THEN** the rebuild is recorded as failed, naming the route, and the tunnel is left running

### Requirement: A rebuild is done when traffic passes through it

A rebuild SHALL count as done only when the payload path passes through the new
tunnel within a bound, and SHALL be recorded as failed otherwise. A process that
started and carries nothing is the failure this exists to catch, and an interface
that came up is not evidence that it carries anything.

The bound SHALL leave the cycle able to run again: the owning runtime reached its
own startup probe 17 seconds after stopping, measured 2026-09-23.

A failed rebuild SHALL count toward the rate bound like any other, and SHALL NOT
be retried inside the cycle that made it.

Time in which the outer path is not reachable SHALL NOT be spent from that bound,
and a proof that ran out of it without the path ever being there SHALL be
recorded against the path and not against the payload. A payload cannot traverse
a tunnel over a path that is absent, so recording it as the tunnel's failure
sends a reader to the tunnel for something it did not do: measured 2026-09-27, a
rebuild after an eleven-minute sleep restored every route and spent its whole
bound while the machine was still finding its network, and traffic passed three
minutes later.

The wait SHALL be no longer for it. What an absent path changes is what the
record says, not how long the runtime waits — a rebuild that waited for the
network would stop the runtime observing for as long as the network took.

#### Scenario: The payload passes

- **WHEN** a rebuild starts the tunnel and the payload path answers within the bound
- **THEN** the rebuild is recorded as done, with what it cost

#### Scenario: The payload does not pass

- **WHEN** a rebuild starts the tunnel and the payload path does not answer within a bound it spent with the outer path reachable
- **THEN** the rebuild is recorded as failed against the payload, and the next cycle decides again from what it observes

#### Scenario: The outer path never came back

- **WHEN** a rebuild starts the tunnel and the outer path is unreachable for the whole of its bound
- **THEN** the rebuild is recorded as failed against the outer path rather than the payload, and it waited no longer than it would have

### Requirement: A suspended cycle decides and does not act

A cycle that runs while the machine is suspended SHALL record its decision and
perform nothing.

A machine dozing on battery wakes for seconds at a time — measured 2026-09-20, 21
such wakes in a night — and a rebuild takes longer than one of them. Acting there
would rebuild a tunnel nobody is using, in a wake too short to finish, and the
rule as proved would have rebuilt fourteen times that night where the owning
runtime rebuilt six.

Nothing is lost by waiting: the gap accrues while the machine sleeps, so the
first waking cycle names it and acts then.

#### Scenario: A cause in a dark wake

- **WHEN** a cycle finds a cause while the machine is in a dark wake or its lid is closed
- **THEN** the decision is recorded, nothing is performed, and the record says the machine was suspended

#### Scenario: The machine comes back

- **WHEN** the first cycle after such a stretch finds the wake gap
- **THEN** it performs the rebuild

### Requirement: The rebuild rate is bounded and the bound outlives the process

The runtime SHALL perform no more than two rebuilds in five minutes, six in an
hour or fifteen in a day, and SHALL stop performing when a bound is reached.

The count SHALL be kept where a restart of this runtime does not clear it. A
daemon that crashes and comes back into the same fault is the case the bound
exists for, and a count in memory would never reach it.

Reaching a bound SHALL end this runtime's ownership by the requirement below, and
SHALL alert.

#### Scenario: A bound is reached

- **WHEN** the rebuilds performed in a window reach its bound
- **THEN** no further rebuild is performed, the tunnel is given up, and the operator is alerted

#### Scenario: The runtime restarts inside a rebuild loop

- **WHEN** this runtime restarts and rebuilds again
- **THEN** the rebuilds it performed before the restart still count toward the bounds

#### Scenario: The operator resumes

- **WHEN** the operator resumes this runtime
- **THEN** the counts are cleared and acting is allowed again, and the tunnel is not taken back by that alone

### Requirement: The runtime gives the tunnel up rather than hold a broken one

The runtime SHALL give up the tunnel when its rate bound is reached, when the
grant that authorizes it lapses, or when the payload path fails for a configured
number of consecutive complete cycles while the outer path is reachable. It SHALL
then observe until the operator resumes it, and SHALL alert on each of the three.

That number SHALL be the executor's own, and not the one a failing payload needed
to be a cause to rebuild. They answer different questions and cost different
things: a rebuild costs seconds, and this costs the tunnel. Measured 2026-09-27
with one number serving both, it was two, 62 of 478 cycles with the outer path up
did not traverse, and ownership ended three times in two days — while a run of
three never happened at all. With them separated the tunnel was held for 171
hours, over which the longest run that counted was still two.

The alert SHALL reach the operator's own session. The runtime that gives the
tunnel up cannot speak to them — the two daemons' only connection runs the other
way, from the operator's session into the root — so it leaves word in a file that
session reads, carrying a reason from a closed list, when it happened and whether
the tunnel went back, and nothing else. A word that cannot be read SHALL announce
nothing: a runtime that reported a tunnel loss because it could not parse a file
would be worse than one that said nothing.

Giving it up SHALL mean handing it to whatever other runtime still holds one —
the same release the operator performs by hand — and, where there is none,
stopping and saying so. A tunnel this runtime can neither repair nor hand on is
not made better by keeping it.

A payload that fails while the outer path is down SHALL NOT count: the tunnel is
not what is broken. A payload that passes SHALL clear the count whatever the
outer path was doing, because traffic traversing the tunnel is the thing being
counted against — a cycle that could not judge is not a cycle that judged
against it. A cycle that did not finish SHALL neither count nor clear: it
carries the previous answer rather than one of its own, and a suspended machine
does not probe. The runtime the tunnel is being reproduced from judges its
own payload the same way, and this runtime's rule does not rebuild on the payload
at all, because it cannot change ingress and would restart into the same one.

#### Scenario: The grant lapses while the tunnel is held

- **WHEN** the generation granting `tunnel_ownership` expires and this runtime holds the tunnel
- **THEN** the tunnel is handed back, the operator is alerted, and this runtime observes

#### Scenario: The tunnel carries nothing

- **WHEN** the payload path fails for the configured number of consecutive complete cycles and the outer path is reachable
- **THEN** the tunnel is handed back, the operator is alerted, and this runtime observes

#### Scenario: The outer path is down

- **WHEN** the payload path fails and the outer path is unreachable
- **THEN** the tunnel is kept, and nothing is handed back

#### Scenario: Traffic passes while the outer path reads as down

- **WHEN** the payload path passes in a cycle whose outer path was unreachable
- **THEN** the count against ownership is cleared

#### Scenario: The operator is told

- **WHEN** this runtime gives the tunnel up for any of those reasons
- **THEN** it leaves word where the runtime that can tell the operator will find it, and that runtime announces it once

#### Scenario: Nobody else holds a tunnel

- **WHEN** the runtime gives up the tunnel and no other runtime is there to take it
- **THEN** it stops its own tunnel, records that the machine has none, and alerts
