# Design

## Three causes, and only three act

The previous rule's six causes stay in its vocabulary, because the archive holds
records that name them and a reader of history has to be able to read them. What
changes is which of them lead to an action: only the process being gone, a wake
gap, and a carrier change produce `rebuild_tunnel`. A returned link, a payload
failure and drifted routes are still observed and still recorded as grounds, so
the record keeps saying what the cycle saw; they no longer decide anything.
`reapply_routes` is no longer produced.

## The tick gap, not the sleep

Twilight sleeps sixty seconds, does its tick, and compares the wall clock at the
start of this tick with the start of the last against 180 seconds. What it
compares is therefore the interval plus the tick's own work plus any time the
machine was suspended.

This runtime measures the suspended time directly, as wall time minus the steady
clock, which is what separates a machine that slept from an observer that was
slow. Comparing that sleep alone against 180 seconds would miss every sleep
between 120 and 180 seconds that Twilight rebuilds on. Comparing the raw wall gap
would bring back the fault the steady clock fixed: a slow fold named a wake on a
machine that was awake.

So the cause holds when the configured interval plus the measured sleep reaches
the threshold, with `>=` as Twilight has it. The work of a tick is left out on
purpose. Where Twilight's own tick is slow enough to cross 180 seconds without a
sleep, the two will disagree, and that disagreement is the soak's to find and
record rather than this design's to reproduce in advance.

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
