# Design

## Three causes, and only three act

The previous rule's six causes stay in its vocabulary, because the archive holds
records that name them and a reader of history has to be able to read them. What
changes is which of them lead to an action: only the process being gone, a wake
gap, and a carrier change produce `rebuild_tunnel`. A returned link, a payload
failure and drifted routes are still observed and still recorded as grounds, so
the record keeps saying what the cycle saw; they no longer decide anything.
`reapply_routes` is no longer produced.

## The tick gap, on the wall clock

Twilight sleeps sixty seconds, reads the wall clock, and compares it with the
reading from the tick before against 180 seconds, with `>=`. What it compares is
the interval, the previous tick's work and any time the machine was suspended,
and nothing in it tells those apart.

This design first measured the suspended time directly, as wall time minus the
steady clock, and compared the interval plus that sleep. It was written so a slow
fold would not name a wake on a machine that was awake, as one had on 2026-09-12
against the old threshold of 90 seconds. It rested on the steady clock stopping
whenever the machine sleeps, verified once with `pmset sleepnow`.

It does not stop for every sleep. The soak had run a day when the archive was read
across the three idle sleeps of 2026-09-14 morning — 136, 997 and 394 seconds by
the power log, with the archive silent for 162 and 1586 seconds across them — and
every decision after them recorded a sleep of zero. Across all 2,365 decisions
carrying grounds, the largest was 115 milliseconds. Twilight, holding the tunnel,
recorded a wake after each of the three sleeps in the power log from 2026-09-11 to
2026-09-13. The rule as designed would have decided none of those, and the soak
would have failed on its first night.

So the cause holds when the wall time between two cycles of the same process
reaches the threshold, as Twilight's does. The measured sleep stays in the record
as a ground. What this gives up is the distinction the steady clock was for: a
cycle slow enough to be 180 seconds after the last names a wake. The slowest
measured was 93 seconds apart. Where this runtime's cycle is that slow and
Twilight's tick is not, the two disagree, and that is the soak's to find.

A reinstall is not a wake. The previous cycle is kept in memory and not on disk,
so the first cycle of a process has nothing to be a gap from.

The rule changed a day into the soak, so the seven days start again.

The meaning of `wake_threshold_seconds` changes with this. Its installed value is
read on the machine before installing; it has to be Twilight's value, which the
supervisor logs.

## The carrier is three paths

The signature is which interface carries the upstream probe address and each
ingress target — the addresses Twilight's watchdog asks about. The cycle already
observes the probe route and every target route; it kept only the targets. It
keeps the probe's observation now, and the signature is built from it and from
the ingress targets alone. That the configured addresses are the ones Twilight
logs is measured on the machine, not assumed: a signature over the wrong three
addresses would agree with nothing.

## The comparison is a program, not a reading

The previous soak compared by hand, and this one decides whether a runtime is
given authority over the network. Its judgement lives in a package with tests:
given this runtime's decisions and Twilight's transitions, it pairs rebuilds by
cause inside a window, and reports agreements, disagreements in each direction,
and whether an agreement was induced. Induction is recorded by the operator when
it is done — a timestamp and a cause — because nothing in either store can tell a
killed process from a crashed one.
