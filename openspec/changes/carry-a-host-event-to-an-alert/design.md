# Design

Every decision below was settled in the grill of 2026-10-10 and is recorded here
so it is not derived again. The facts each one rests on were measured first.

## The route is through the cloud, because both ends of it are already built

The alternative was the Mac posting to Telegram itself. It is much smaller, and
it was rejected on three measurements. `internal/alertdelivery` already holds the
night window, the transactional outbox with leases, the retry bounds, the
processor and the Telegram client, so a host-side sender is a second
implementation of a policy that already exists and would drift from it. It would
put the bot token on the operator's machine, where today it is only in the cloud
worker's configuration. And the objection that an alert about losing connectivity
travels over the connection that may be gone is answered by what is built rather
than by a second sender: the cloud infers a condition from **absence** as well as
from what arrives, so pushed events carry "something happened" and silence
carries "the host cannot say anything".

## The upload does not run in the observing cycle

The root cycle aims at a 60-second period and now owns the tunnel. A network call
inside it is a tunnel nobody supervises for as long as the call hangs, and
nothing else in that cycle leaves the machine. So the uploader is its own
scheduled privileged job, in the shape the repository already has: a launchd
agent calling a wrapper in `observe-root/bin`, as `archive-review` does weekly.

This also matches what the uploader was built for. It carries its own spool
cursor, applies acknowledgements, detects sequence gaps and repairs them — the
machinery of something that resumes on its own schedule, not of something driven
once per cycle.

## Two triggers, and the urgent one does not watch the spool

The spool gains a record every cycle, so watching the spool would start a process
every minute and send 1440 tiny batches a day for a stream nobody is waiting for.
Watching nothing and running on an interval would delay the one record that is
worth sending now.

So: an interval for the stream, and a marker file for the incident. The runtime
touches the marker from exactly one place — the journal door that writes an
incident — so the knowledge "this one is urgent" exists once. The job removes the
marker only after the acknowledgement, so a job that dies mid-upload is started
again rather than forgotten, and the interval collects whatever a missed trigger
left. The daemon touches a file; it makes no network call, which is what keeps
the previous decision intact.

## The incident travels, and the handback record stays

`RecordTunnelDecision`, `RecordTunnelExecution` and `RecordTunnelHandback` write
to the event archive only. They never reach the spool, and the archive says of
itself that it is not an upload source and that no acknowledgement reaches it —
"archiving a record is not a decision to send it", which the baseline already
states. So the handback itself cannot be what goes up.

What goes up is an incident. `tunnel.handback` stays as the local detail, and the
incident is the thing with a correlation key, a severity and an end.

## One door more on the journal, and it is as narrow as the first

`Journal.Append` takes a `connectivity.Fact` and nothing else. The incident needs
a different entry, and there were three ways to give it one.

A second spool was ruled out by measurement: the cloud's cursor is keyed
`(node_id, boot_session_id)`, one sequence stream per node per boot, so a second
spool would advance a second counter under the same key and the gap detector
would report permanent holes — and a hole is indistinguishable from a lost
record.

Writing the spool directly from the root daemon would put two writers on the one
counter the cloud's cursor depends on, and would duplicate the mirror by hand.

So the journal gets `AppendIncident(event.Incident)`: one closed type with its
own validation, beside `Append`'s one closed type with its own. The spool keeps
one writer, and the mirror into the archive comes for free. This adds no new
class of content to the spool, either — the spool already writes its own
`incident.lifecycle` entries when it overflows or quarantines, and
`journal.record` already returns a clean skip for an entry that is not a
connectivity fact. The readers were built to tolerate this.

## A condition, not an occurrence

The cloud correlates by node, category and component. The host's own incident
identity is kept as evidence instead.

This is not a preference. The spool records an overflow incident on every append
that meets its size bound, numbering each by its own sequence — and until an
uploader exists, meeting that bound is the spool's steady state, because records
leave it only by eviction. Correlating on what the host named would therefore
open a new critical incident every cycle, each requiring action, and an
actionable incident's delivery ignores the night window entirely. The first
upload would have been a flood at three in the morning.

Correlating on the condition collapses that to one incident that updates, and
the existing precedent is the same shape: `SignalFromSilentDecision` keys on
`"silent-node:" + NodeID`, per node and per kind, not per event.

## Silence is kept, with an expectation a laptop can meet

Registering the Mac and then not evaluating its silence would leave this path
with no failure detection: if the uploader stops, nothing arrives and nothing
says that nothing arrived. That is the one failure the pushed half cannot report.

But a silent node requires action, so its delivery bypasses the night window, and
a Mac registered to report every minute would alert every night when the lid
closes. The expectation is therefore 24 hours, which is the schema's ceiling
(`expected_heartbeat_seconds BETWEEN 10 AND 86400`) and the deadline is
`last_seen_at + interval × missed_heartbeats`. A nightly sleep does not reach it;
a day of hearing nothing does.

That is honest and it is temporary. The next change teaches the Mac to announce
its own sleep, and the expectation tightens to the job's own cadence, after which
silence means "went away without saying so" — and when the announcement fails to
upload, the resulting alert is still true.

## The key is made where someone can register it

`signing.GenerateFile` exists and is tested, and nothing in production calls it;
the ingress hosts get their keys by a private step. For this host the generation
goes in the root installer, which already runs as root, already places
root-owned files and already keeps a file it is pointed at rather than
overwriting it.

Installation is the moment the operator is present, which is what the public half
needs: it is printed once, and registering it is a step of the procedure rather
than something to remember. A key generated inside a scheduled job is a key
nobody registered, and its first uploads fail authentication in a way that looks
like silence.

The node identity and the key material are private and belong to
`hexroute-infra`. This repository carries the procedure.

## The reply path is held by what it can reach

The baseline already says the uploader "SHALL NOT ... expose the acknowledgement
to policy, reduction or action packages". Nothing holds it, because until now
there was no reply path on this machine at all.

`ApplyAcknowledgement` is already narrow: the reply must match the batch, the
node and the request, and its only local effect is `journal.Acknowledge`. A unit
test of that holds the handler that exists. A gate on reachability, in the shape
of `TestNoPackageCanBothHoldAProposalAndMutate`, holds the handler written next.
It closes a class and not every path — a reply could still influence something
indirectly through a file — which is why the spec keeps the scenario as well.

## A channel nothing claims is not planned

`Policy.Plan` writes a `local_macos` row for every actionable incident;
`ClaimDue` refuses any channel but `telegram` and `morning_digest`; retention
deletes only `delivered` and `suppressed`. The row can be neither served nor
expired.

Teaching retention to delete it would mean accumulating and then discarding rows
that never meant anything. Making it claimable now would pull the whole local
channel and the `ExternalState` wire into this change, and that wire has its own
unanswered question: how the local notification learns the cloud confirmed. So
the plan stops emitting it, and the third change adds it back together with its
claimer — in one change, so the plan and the claim cannot be out of step.

## What this leaves open, deliberately

The spool's own incidents get no end. `event.IncidentResolved` gains a producer
here for the tunnel, not for the spool, so an overflow condition will sit open.
Under this correlation it is one incident rather than thousands, but nothing
closes it — and there is a quiet joke in it: once this uploader runs, records
leave the spool on acknowledgement instead of by eviction, so the condition
genuinely ends and nobody observes the ending.

HEX-19 is the cost of that eviction, measured at 6.1 seconds for a gigabyte. It
should fall for the same reason. That is a prediction, and it is worth measuring
rather than announcing.

The job uploads regardless of power source. The batches are small and nothing
measured says otherwise.
