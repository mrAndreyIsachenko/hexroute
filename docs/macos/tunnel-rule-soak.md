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
runtime that was not running. A silence the runtime ended by deciding a wake gap
was a sleep and counts as observed; any other is a hole, and the judgement refuses
it. Collections made
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
upstream VPN off, or on. The cause names are `process_gone`, `wake_gap` and
`carrier_changed`; a wake gap is not induced, because the soak needs natural ones.

## Judge

```sh
sudo '/Library/Application Support/Hexroute/observe-root/bin/hexroute-soak-compare' --from '<soak start, RFC 3339>' judge
```

`PASSED`, `NOT PASSED` with what is missing, or `NOT JUDGEABLE` with the stretch
nobody observed. Every disagreement is listed with its time, in both directions:
a rebuild this runtime decided that the owner did not make, and one the owner made
that this runtime did not decide.
