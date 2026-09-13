# Make the daemon time its own start

## Why

The root daemon takes tens of seconds to open, observes nothing while it does,
and cannot say where the time goes. Every answer about it has come from sampling
the process from outside, and three attributions taken that way in one session
were wrong:

- seventeen seconds were assigned to the archive's open, which turned out to be
  about seven;
- thirteen were assigned to listing the spools, which the same listings do in
  951 milliseconds;
- and the most recent sample shows this repository's frames holding about one
  percent of the window, so the daemon is waiting rather than working, and
  nothing outside it can say on what.

Each of those was read from a sample tree, where a frame's presence was taken
for its weight. The instrument was wrong three times because the machine has no
way to be right: its log records carry a level, an event, a result and a reason,
and no number at all.

This is the same shape as the tunnel decision record, which carries an action and
its causes and nothing they were decided from. A runtime that cannot report a
quantity about itself makes every question about it a guess by someone reading
its stacks.

## What Changes

- A log record may carry a duration, and the vocabulary of what may be timed is
  closed, like every other field of that record.
- Opening the connectivity host reports how long each store took, and the daemon
  records one line per store before it reports starting.
- What the daemon then says about its own start is a measurement rather than an
  inference, and stays true when the stores change size.

## Impact

- Affected specs: `local-control-plane-foundation`
- Affected code: `internal/logging`, `internal/connectivityhost`, `internal/rootdaemon`
- Not in scope: making the open faster. Two of its costs have been removed
  already and what remains is unattributed — which is the reason for this change
  rather than something it fixes.
