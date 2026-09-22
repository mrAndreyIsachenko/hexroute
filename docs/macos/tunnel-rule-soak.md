# Soaking the Tunnel Decision Rule

The rule this runtime decides tunnel rebuilds by is proved against the runtime
that owns the tunnel before anything is allowed to act on it. The soak is at
least seven days with no disagreement, and it needs at least three natural
wake-gap agreements, two carrier agreements and one induced process loss.

The judgement is a program, `hexroute-soak-compare`, and it judges a ledger the
soak is collected into as it goes. It cannot read the archive at the end: the
archive answers for about a week and evicts by age regardless of priority, so the
first hours of a seven-day soak are gone by then.

## Collect every day

```sh
sudo '/Library/Application Support/Hexroute/observe-root/bin/hexroute-soak-compare' collect
```

The first collection takes `--from` with the soak's start; later ones continue
from the newest record the last one covered. A collection missed for longer than
the archive keeps operational records loses records for good, and the judgement
then refuses the soak rather than counting the loss as a quiet stretch.

That is not the archive's age. Measured on 2026-09-14 at 14:19Z the archive held
six days and nineteen hours; measured at 17:49Z its oldest operational record was
three days and five hours old, because the records it writes about its own
evictions are never evicted for size and push the others out. Until the archive
stops doing that, collect in the morning and in the evening.

A collection records the silences inside what it read. The runtime writes several
records every cycle, so a silence longer than three cycles is a machine asleep or a
runtime that was not running. A silence the runtime accounted for in a wake it
decided was a sleep and counts as observed — either the wake came within three
cycles of the silence ending, or the gap it was decided on covers the silence,
which is what a night of dark wakes looks like. A silence a cycle ran inside
counts too: a decision names the cycle before it, so a cycle whose own record
the sleeping machine swallowed is still visible. Any other is a hole, and the
judgement refuses it.

A stretch the machine dozed through — waking for seconds every quarter of an
hour, as it does on battery with the lid closed — is not judged at all. Both
runtimes decide in such a stretch, in wakes the other slept through, and pairing
those is meaningless. Wake gaps for the soak therefore come from deliberate
sleeps: close the lid on battery, then open it, and both runtimes name the same
wake within seconds. A deliberate sleep is not a doze, and the two are told apart
by how often the machine suspended: a night leaves about a dozen suspended cycles
over hours, a lid closed for five minutes leaves one, and three is the line. Collections made
before silences were recorded are not evidence either way; collecting once with
`--from` the soak's start covers them again:

```sh
sudo '/Library/Application Support/Hexroute/observe-root/bin/hexroute-soak-compare' --from '<soak start, RFC 3339>' collect
```

`--from` always wins over where the ledger reached.

A collection can fail with a record that is not found. The daemon evicts records
while the collection reads, and one removed between the listing and the read is
reported rather than skipped. Run it again; nothing was written.

## Before inducing anything, say so

An induced event is counted apart from a natural one, and neither runtime's record
can tell a killed process from a crashed one, so the operator records it:

```sh
sudo '/Library/Application Support/Hexroute/observe-root/bin/hexroute-soak-compare' --cause process_gone note
```

Then induce it. A process loss is stopping the tunnel process the owning runtime
started; it restarts it within about a minute. A carrier change is switching the
upstream VPN off, or on — and on this machine the upstream VPN is AdGuard VPN, not
Pritunl. Twilight's carrier is whichever interface carries the probe address, and
in its `upstream-vpn` mode that is AdGuard VPN's tunnel; Pritunl runs inside
Twilight's own tunnel and toggling it changes no route that counts, as two attempts
on 2026-09-17 showed. Switching AdGuard off breaks this repository's rule never to
stop it, so it needs the operator's explicit permission each time. Note before
switching it off and note again before switching it on: Twilight rebuilds on its
next tick after each, and one note matches only a rebuild within two minutes. The cause names are `process_gone`, `wake_gap` and
`carrier_changed`; a wake gap is not induced, because the soak needs natural ones.

## The machine will not sleep on its own

The owning runtime holds `caffeinate -i -s` for as long as its supervisor runs, so
the machine does not sleep while idle and the soak would wait for wake gaps that
never come. Measured 2026-09-16: that assertion had been held for two days and one
hour, and the last sleep in the power log was twelve minutes before it started.

Neither flag covers the lid. `-i` refuses an idle sleep and `-s` refuses one on
mains power, so closing the lid on battery sleeps the machine — while on mains it
reaches dark wake instead, which is not a gap. Close the lid on battery for five
minutes or more, three times over the soak; a night is enough.

That is not an induction and is not noted: nothing is told to either runtime, both
see the same gap, and neither is given a hint the other lacks. What is checked
after one is that this runtime decided a wake gap and the owning runtime recorded
`wake_gap_detected` for the same moment.

## Judge

```sh
sudo '/Library/Application Support/Hexroute/observe-root/bin/hexroute-soak-compare' --from '<soak start, RFC 3339>' judge
```

`PASSED`, `NOT PASSED` with what is missing, or `NOT JUDGEABLE` with the stretch
nobody observed. Every disagreement is listed with its time, in both directions:
a rebuild this runtime decided that the owner did not make, and one the owner made
that this runtime did not decide. A process loss this runtime decided beside a
restart the owner made for another reason is listed as explained by the owner's
own restart, and is not a disagreement: watching from outside, both replace the
process.
