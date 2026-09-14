# Design

## The six that move and the six that stay

Measured from the live supervisor, not read off its source.

Moving: starting sing-box and restarting it when the process is gone; the
carrier changing; the wake gap; the payload path failing; the scoped routes;
holding the machine awake.

Staying: selecting the ingress with its quarantine and automatic restore; the
Codex fallback's health; the startup probe; the reserve probe tick; the health
probe that only reports.

Keep-awake is in the moving set and looks out of place there. It is not: the
supervisor runs `caffeinate`, and the wake gap is one of the six causes this
runtime decides on. Leaving it behind would change how often the cause it was
tuned against occurs, on the same day ownership changed, and the two would be
impossible to tell apart afterwards.

## Why the supervisor stays

The grill settled that the unit is the whole supervisor. Two measurements
revised it.

There is no reduced mode: `TWILIGHT_SUPERVISOR_MODE` is written by the installer
and read by nothing, so "the supervisor without the tunnel" does not exist and
has to be built either way.

And its ingress selection changed 91 times in 61 days — 33 quarantines of the
current ingress, 30 automatic restores, 24 switches to a candidate that passed.
Booting it out to take the tunnel would drop a behaviour that fires half again a
day to gain one that fires every other week.

So the supervisor keeps running and loses one job. The boundary is drawn through
the process rather than through trust: it does not stop being able to start
sing-box, it stops being the one that does.

## The claim is a file, and only the operator writes it

It has to survive the supervisor being restarted, because launchd keeps it alive
and brings it back within ten seconds. It has to take effect without a restart,
because a restart is the worst moment to need one. A file read every tick and
before every start is the only one of the three candidates that does both.

Neither daemon writes it. A daemon that could claim ownership could claim it
while the other still held the process, and the whole point of the claim is that
exactly one does.

## Abort returns, it does not release

Abandoning gives the process back rather than leaving it stopped. The supervisor
already restarts sing-box when it finds the process missing — measured today at
fifty-seven seconds from kill to running — so abort is: remove the claim, stop
what this runtime started, and let the path that already exists run.

"Stop and wait for a person" was rejected. A machine with no owner for the
tunnel has no network for as long as nobody is looking at the terminal.

## Completion is two proofs, not one

The payload prober exists and answers whether traffic traversed rather than
whether something accepted a socket. One answer says what one moment was like,
which is the mistake the link cause made six times in half an hour before it was
given a threshold. Two consecutive, within 120 seconds — twice the fifty-seven
the incumbent takes to restart.

## Rehearsal first

Every part of this runtime applied to the live machine for the first time has
been wrong about something: the decision ran only on complete cycles, the
carrier signature was keyed by the wrong field, the link cause had no threshold,
the wake gap measured the observer. The transaction is the part whose first
mistake costs the network.

So it runs once performing every phase except the claim and the start. The
envelope, the verification, the probes and the abort are exercised where being
wrong costs nothing.

## What this does not decide

Whether the supervisor's remaining six should eventually move. They are outside
this change and the machine keeps doing them.
