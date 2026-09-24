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

## A process gone, as the owner watches its child

The first induced process loss of the soak, on 2026-09-17, disagreed. Twilight
watches the process it started with `kill -0` on its own child every tick, and
restarts it within seconds: stopped at 11:38:09Z, noticed at 11:38:28Z, running
again at 11:38:32Z. This runtime asked each cycle whether a tunnel process ran,
and one did on both cycles around the loss.

So a process is gone when the one the previous cycle saw is not the one running
— by PID, remembered in memory only, so an installation is not a loss. That
brings in what the owner does for its own reasons: every restart it makes for a
carrier change, a wake, a restored outer path or a payload failure replaces the
process too, and this runtime sees a loss it cannot tell from those. The
comparison therefore reads every restart in Twilight's log — each transition to
`STARTING` or `SINGBOX_EXITED` — and a process-gone episode with no process
rebuild beside it but a restart inside the window is listed as explained, not as
a disagreement. As an owner, change three's executor knows which restarts are
its own and needs none of this.

The rule changed again, so the seven days start again.

## Deciding inside a dark wake

A night on battery is not one sleep. Measured 2026-09-18, a machine with its lid
closed went through 21 dark wakes: it woke for seconds, wrote a few records and
slept again. Twilight's tick — a sleep and a few local checks — fits in that
window and decided six wakes. This runtime's cycle reached its decision after
probes that wait on a network, often did not finish, and named eight wakes, only
four of which were the same events as Twilight's. Its longest recorded gap
covered 112 minutes and four of Twilight's ticks.

So the tunnel's own observations — the process and what carries the three
addresses — are taken first and cost a process listing and a few route lookups,
and they are taken whatever the lid is doing. The probes follow the decision. A
cycle in a dark wake stops before them, which is what the state already meant
for the network it proposes; it no longer means the tunnel goes unwatched.

That gate is also what made every one of those nights' decisions name a lost
process: the cycle returned before looking, and the rule read "not running" out
of an observation nobody took. A cause now needs its observation.

The judgement had to follow. A silence counted as observed only if a wake was
decided within three cycles of its end, and on a night of dark wakes the cycle
that finishes is several sleeps later: measured 2026-09-20, a silence of 46
minutes whose wake was decided 42 minutes after it ended. A decision carries the
gap it was decided on, so the ledger keeps it, and a silence inside that gap is
one the runtime accounted for.

Then a silence appeared that no wake covered, because the cycle inside it wrote
nothing at all: it decided at 04:26:33Z and the machine slept again before the
rest of that cycle — the read model, the heartbeat, the record. Two answers, both
taken. The decision is written first now, so a cycle that reaches it leaves the
one record the comparison needs even if it never reaches the rest. And the
judgement reads the chain: a decision names the cycle before it by its gap, so a
cycle whose record was lost is still visible, and a silence it ran inside is a
lost record rather than an unobserved stretch.

The payload path waits on a network, so a suspended cycle does not probe it and
carries the last answer as a ground. It decides nothing either way.

## A dozing machine is not judged

With the judgement finally able to read the nights, it reported twenty
disagreements in one: eight wakes decided here that Twilight did not make, five
it made that were not decided, and seven process losses of the same shape. None
of them is a fault in the rule. A machine dozing on battery wakes for seconds
every quarter of an hour, and the two runtimes wake in different ones: 02:06
against 02:14, 03:15 against 03:33. The comparison pairs events two minutes
apart, and these are twenty apart.

A real sleep is the opposite: the lid closed on 2026-09-16, and the two runtimes
named the wake five seconds apart.

So a stretch where this runtime's cycles did not finish is not judged at all.
That stretch is already legible in the records: a cycle that stops before the
probes records an incomplete decision, and every decision through that night is
one. Nothing in such a stretch is compared, in either direction, and a silence
inside it is not a hole.

One disagreement survived that exclusion and showed where it was too narrow: the
owning runtime named a wake at 10:21:47Z, after the machine was up, on a gap of
1,625 seconds that began at 09:54:42Z — inside the doze. This runtime's cycles
had resumed by then and its own gap was a minute. The event was reached while
awake and is about a stretch nobody judges. So an event whose gap began inside a
dozing stretch is not judged either: for this runtime the gap is the cycle its
decision names, for the owning runtime the last time its log says it wrote
anything.

The first induced process loss after that showed the marker was the wrong one. A
cycle that does not finish is not the same thing as a dozing machine: the tunnel
went down, the route to an ingress target could not be read, the cycle stopped
early — and the judgement read that as a doze and dropped the event it existed to
compare. The record says which it was now, from the cycle's own power
observation, and only records written before it said so fall back to the older
reading.

What this gives up is stated in the spec rather than hidden. The rule is not
proved for a dozing machine, and with authority it would have rebuilt the tunnel
fourteen times that night where Twilight rebuilt six. That is the executor's to
answer, in change three, and it is written down as owed rather than solved here.

## The carrier is three paths

The signature is which interface carries the upstream probe address and each
ingress target — the addresses Twilight's watchdog asks about. The cycle already
observes the probe route and every target route; it kept only the targets. It
keeps the probe's observation now, and the signature is built from it and from
the ingress targets alone. That the configured addresses are the ones Twilight
logs is measured on the machine, not assumed: a signature over the wrong three
addresses would agree with nothing.

## A change shorter than a cycle

The first natural carrier change of the soak was one neither runtime could have
compared. On 2026-09-23 at 11:00:52Z an ingress target left the upstream tunnel
for the physical interface, and the owning runtime — which looks once a tick —
saw it and rebuilt; its own rebuild put the route back by 11:01:09Z. This
runtime's cycles at 11:00:26Z and 11:01:26Z read one and the same signature, and
the seventeen seconds between them were never its to see.

Both runtimes sample at sixty seconds in different phases, so this is not a
difference of rule: whichever of them looks inside the flap names it, and the
other cannot. The judgement therefore asks what this runtime read. A collection
keeps the moments its reading of the carrier changed — a handful of marks, since
a signature holds for hours — and a carrier rebuild the owner made with no mark
of ours around it, in a stretch where we had read a carrier, is listed as one we
could not have seen rather than counted against the rule. A change we did read
around that moment stays a disagreement.

It recurs. By 2026-09-24 09:00Z four such flaps had been judged — 11:00:52Z,
15:43:05Z, 23:24:16Z and 08:54:02Z — every one of them the same ingress target
leaving the upstream tunnel for the physical interface, every one preceded in the
owner's log by about a minute of connection resets to that address, and every one
over before this runtime's next cycle.

What this gives up is stated in the spec: the rule is not proved for a carrier
change shorter than a cycle, and with authority this runtime would not rebuild
where the owner does, about four times a day. Change three's executor, which will hold the tunnel
rather than watch it, can read the routing socket instead of sampling, and that
is where the answer belongs.

## The comparison is a program, not a reading

The previous soak compared by hand, and this one decides whether a runtime is
given authority over the network. Its judgement lives in a package with tests:
given this runtime's decisions and Twilight's transitions, it pairs rebuilds by
cause inside a window, and reports agreements, disagreements in each direction,
and whether an agreement was induced. Induction is recorded by the operator when
it is done — a timestamp and a cause — because nothing in either store can tell a
killed process from a crashed one.
