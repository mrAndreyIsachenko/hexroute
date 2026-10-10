## MODIFIED Requirements

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

The word it leaves is for whoever is at the machine, and that is not always
anybody. Giving the tunnel up SHALL therefore also open an incident the host can
report, so the state reaches an operator who is somewhere else. The two are not
alternatives: the file stays because the reason for it has not changed — the two
daemons' only connection runs from the operator's session into the root — and the
incident leaves by the path the host uses for everything it sends, which carries
no authority back.

Taking the tunnel again SHALL end that incident. A condition that only ever opens
makes the next alert arrive against the background of one that never closed,
which is how an operator learns to stop reading a channel. The operator resuming
this runtime is the end of the state the alert described, so it is where the end
is recorded.

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

#### Scenario: Nobody is at the machine

- **WHEN** this runtime gives the tunnel up and no operator session is reading
- **THEN** the word it left is still there to be announced later
- **AND** an incident is opened that the host reports, so the state can reach the operator elsewhere

#### Scenario: The operator resumes this runtime

- **WHEN** the operator resumes it and it takes the tunnel again
- **THEN** the incident opened when it gave the tunnel up is recorded as ended

#### Scenario: Reporting is unavailable while the tunnel is given up

- **WHEN** the host cannot upload at the moment it gives the tunnel up
- **THEN** it still hands the tunnel back, leaves its word and observes
- **AND** the incident waits in the upload queue rather than delaying any of that
