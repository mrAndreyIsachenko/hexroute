# The local event archive

The host keeps every typed event twice, for two different reasons, and the
difference is the whole point of this.

## Why raising the spool bound is not retention

The upload spool holds events until telemetry acknowledges them. A record
leaves the moment the cloud says it arrived — so the spool's size bound is not
what removes records, and raising it changes nothing about how far back the
host can answer. A spool ten times larger empties at exactly the same rate.

This is worth stating plainly because the question comes back. "We are losing
history, make the spool bigger" is a reasonable sentence and a wrong one: it
proposes to change a bound that was never the constraint. Retention is a
function of time and disk, and the spool's is a function of upload success.

The archive is a second sink for the same records, retained by age and total
size only. Nothing acknowledges into it, and it exposes no way to remove a
record — one door in, three ways to look, and a test that fails if a fifth
exported method appears.

## What it retains, and until when

| Bound | Default | What it means |
| --- | --- | --- |
| age | 30 days | how far back a review can ask |
| size | 256 MiB | how much disk the answer may cost |

Age eviction ignores priority: a record outside the window goes, critical or
not, because a stale answer is not made truer by having been important. Size
eviction removes diagnostics first, then operational records, and never a
critical one — an append that could only be satisfied by dropping critical
evidence is refused instead.

Only records that decode under a registered event schema may enter. One that
does not is refused, and the refusal is counted and written into the archive
as a diagnostic — at the first refusal and at each doubling after it. Every
refusal would let a broken producer evict the evidence the archive exists to
keep; none would leave "no such record was ever offered" available as a
conclusion.

## What an overflow record means

Reaching a bound produces a durable record naming what went and where it sat.
Without one, a reader who finds records 100 and 140 cannot tell an idle hour
from an eviction.

| Reason | What happened |
| --- | --- |
| `age` | records fell outside the window and were removed regardless of priority |
| `size` | records were evicted to stay inside the size bound, cheapest evidence first |
| `refused` | an append was refused because the only records left to evict were critical |

A `refused` overflow names no sequence range, because nothing was dropped.
Something was not accepted, and the two are different answers to the same
bound.

## How the covered window is read

A read reports two windows. **Requested** is what was asked for; **Covered** is
what the archive could answer about. They disagree in exactly the case that
matters — an archive whose oldest record is newer than the request — and a
reader who cannot see the difference reads an eviction as a quiet week.

`Shortened` says so directly, because the three ways it happens all look the
same from outside: the window starts after the request, ends before it, or the
record limit cut the read short.

An empty answer says it is empty. A zeroed window would print as a period in
which nothing happened, which is the one reading an empty archive must never
receive.

## Crash safety

Every write is a staged file, a file sync, an atomic rename and a directory
sync. An interruption at any of those points leaves the archive readable, with
the record wholly present or wholly absent and never a partial one. All six
boundaries are exercised by fault injection rather than argued for.

A staged file found at startup is removed rather than read. It was never
published, so removing it cannot leave a hole: no reader ever saw it. The
sequence does not go backwards over an interruption either — a record that was
staged and lost still consumed its number.

## The weekly review

```
hexroute-archive-report --archive <root> --out <directory>
```

It writes one dated report for a window and nothing else. It holds the archive
through a handle that cannot write, and it does not create an archive it
cannot find: a missing archive and an empty one are different answers.

The report counts records by schema and by component, lists the transitions
that occurred, and ranks findings by rarity — a review is looking for what it
has not seen before, and the counts are what the common case is for. Every
ordering is on a recorded value, so two runs over one archive produce the same
bytes. The file on disk is the canonical encoding the digest was taken over.

The schedule owns a weekly interval rather than a calendar entry, which can
fire twice across a clock change, and runs through a wrapper that records the
attempt before making it and always exits zero. "No report this week" and "no
attempt this week" have different causes — one is the archive, the other is
the schedule — and handing the review's real exit code to launchd would turn a
week the archive could not be read into a job that looks like it crashes.

```
sudo scripts/macos/archive-review-launchd.sh install ./bin/hexroute-archive-report
sudo scripts/macos/archive-review-launchd.sh attempts
sudo scripts/macos/archive-review-launchd.sh reports
```

Uninstall removes the plist, the binary and the wrapper. Reports and the
attempt log stay: they are the record of the reviews that did run, and the
week someone uninstalls this is the week someone is reading them.

## The optional model pass

```
hexroute-archive-annotate --report <file> [--model <name>]
```

Off by default, and nothing schedules it. It may attach one note to a finding
the ranking already produced, and may do nothing else — not select, not order,
not remove, not add.

That is not caution about models. A report is worth having only if this week's
compares with last week's, and a ranking a model could influence changes when
the model changes: a difference in the model, reported as a difference in the
host. The calibration bench measured the instability directly — between two
runs of the same cases, what one run saw the next missed.

The rule is held by construction. Neither the archive nor the report command
may import `os/exec`, so the pass could not live in either; commentary sits
outside the digest; and the command recomputes the digest after attaching and
refuses to write a report whose digest moved.

| Outcome | What it says |
| --- | --- |
| `attached` | a note was added to at least one finding |
| `nothing_to_say` | the model answered and offered nothing, which is a good answer |
| `unavailable` | no local model could be reached |
| `timed_out` | the model was reached and did not finish |
| `unusable` | the answer could not be parsed, or was about findings this report does not contain |

Each is recorded on the report. One with no commentary and no record of a pass
cannot be told from one where no model was ever asked, and those are different
weeks.

## Rollback

Stop passing `--connectivity-event-archive` to the root daemon. Nothing is
mirrored, the read model runs the path it ran before, and what the archive
already holds stays on disk — rolling back is not deleting the evidence.

An archive that will not open is reported and the read model carries on. A
host that stopped watching its own network because a retention store would not
open would have traded the thing that matters for the thing that helps
afterwards.
