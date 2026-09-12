# Hexroute Roadmap

Status date: 2026-09-04.

## Current Baseline

- Twilight remains the production owner of sing-box, scoped routes and
  Keychain-backed Pritunl recovery.
- Hexroute root and user daemons are installed beside Twilight under disjoint
  labels, paths, sockets and stores. They remain pre-cutover and have no
  production data-plane mutation authority.
- The atomic policy compiler, user-presence signer, immutable root/user stores
  and typed cross-domain activation are implemented. A deny-only initial bundle
  and a higher-deny successor have passed a live local activation and rollback
  safety check while both Codex paths, Twilight and AdGuard remained available;
  private artifacts and evidence remain outside public Git.
- Atomic policy generations and the generation-bound network reconciler are
  synced into baseline specs. They remain pre-cutover and grant no production
  mutation authority. The reconciler is reachable from `hexrouted` only far
  enough to answer what its shadow store holds; it compares nothing against the
  host and cannot, because its dependencies forbid every package that could
  reach one.
- The observable connectivity read model is qualified and synced into the
  baseline specs. Its shadow evidence chain is complete: 260,921 eligible
  seconds against the 259,200 required, two sleep/wake cycles, one reboot and
  all thirteen mandatory injected faults, with zero divergences and nothing
  left unbound. It runs on the host behind an off-by-default gate. Root adapts its existing network, default-path, scoped-route,
  transport and relay observations into facts; the user daemon publishes what
  only it can see over authenticated IPC; the aggregate reduces both into one
  snapshot and checkpoints it on effective change. DNS has no observer, so that
  component reports `unknown`. Nothing in this path can mutate anything.
- Deterministic lifecycle policy, typed peer-authenticated IPC, bounded local
  journals, signed ingestion and redacted diagnostics are implemented and
  covered by synthetic tests.
- The telemetry-only cloud API, worker and migrator implement PostgreSQL-backed
  ingestion, incidents, retention, Telegram alerts, the redacted connectivity
  projection and a passkey-protected read-only dashboard.
- SLO availability is calculated hourly per node from stored evidence and
  upserted, so the dashboard's SLO section reports measured availability. A
  window whose opening state cannot be established, or whose failed time no
  incident explains, is left uncomputed rather than filled in.
- Incident-bundle creation and expiry are scheduled in the maintenance worker.
  A closed incident with linked evidence is bundled once; an incident with
  nothing linked is never selected, and one whose bundle was removed at its
  recorded expiry is not bundled again, so retention is not undone by the pass
  that would restore it. Where bundle storage is unconfigured the pass records
  `cloud_incident_bundle_unconfigured` on every interval, so a deployment that
  was never finished reads differently from one with nothing to bundle. The
  object store it writes through implements two methods and cannot read; its
  SigV4 signing is checked against fourteen of the provider's published test
  vectors at all three stages, not against itself alone.
- Durable local event retention keeps typed events by age and size rather than
  by upload state, with a scheduled review and an annotator that refuses to
  write when attaching commentary would move the digest.
- Public reusable Terraform modules exist; live roots and deployment evidence
  remain in the private `hexroute-infra` repository.
- The first cloud foundation is live behind `status.hexroute.app` with external
  black-box monitoring. The control plane, PostgreSQL and private object
  storage are isolated in a dedicated DigitalOcean Team with canonical,
  disjoint production and provider-neutral edge states.
- The first measured logical PostgreSQL and private-object restore drill,
  RPO-zero cutover, 72-hour soak and old shared-Team decommission are complete.
- The cloud foundation is not yet a second transport provider.
- Provider-B reusable Lightsail infrastructure, secret-free bootstrap and four
  independent functional probes are published and documented, but no public
  fact claims a workload is deployed, qualified, inventory-admitted or
  failover-enabled. See
  [`docs/architecture/provider-b-ingress.md`](architecture/provider-b-ingress.md).
- Repository checks run in CI on every pull request and on `main`: `make check`
  on macOS, the Go gate on Linux, and `make postgres-test`. A package that no
  binary contains fails `make check`, so code cannot be written, marked done
  and left reachable from nothing without saying so. Four packages are currently
  recorded as unwired.
- The operational acceptance drill in
  [`docs/testing/operational-acceptance.md`](testing/operational-acceptance.md)
  is complete and archived.

## Active Changes

`decide-what-a-tunnel-owner-would-do` is open. It is the second of four changes
the grill of item 8 settled, and the first in this repository: the Codex
fallback was lifted out of the supervisor in `twilight` first, because an escape
hatch must not belong to the experiment it exists to escape.

The grill settled that the build and the switch are separate changes in that
order. The alternative makes booting the supervisor out the first occasion on
which the decision rule was ever applied to a live machine, and if it is wrong
about a wake or a carrier change, that is learned without a network.

Six causes, measured from the supervisor's own log over sixty-one days rather
than derived from its source: nineteen carrier changes, eleven wake gaps, two
payload failures, the routes reapplied every tick, and two that did not occur at
all — the process exiting and the link returning. Both are kept. The process
exiting is the only thing between this machine and no network.

This runtime already observed five of the six. The sixth needed a probe that
fails when traffic does not traverse, because a completed connection proves only
that something accepted a socket — and that same distinction is what item 8
requires as the evidence completing the switch.

Nothing executes. The safety allowlist already names `restart sing_box`, and no
policy grants it: the capability that would is the change after this one.

What follows is what the recent ones changed and what they left standing, kept
because the reasons are worth more than the record of having done them.

`pay-for-eviction-not-for-being-full` closed on 2026-09-13. The root daemon came
up at 17:32:58 on 2026-09-12, wrote its last heartbeat at 17:35:54, and four
hours later was still running with a core busy in `jsoncanonicalizer` under
`spool.scanStable`, reached from `spool.Append`. The `user` journal's spool stood
at 104,858,244 bytes against a bound of 104,857,600 — over it — holding 84,067
records, and every append decoded all of them to learn one fact per record that
eviction needs and the directory cannot report: the priority class. Nothing
recorded the stall, because the append that would have recorded it was the one
that hung.

The class now sits beside the kept listing. A record this process published
carries it from the append that wrote it; a record already on disk is opened
once, by a reader that decodes the envelope and stops. The listing outlives the
append, so the reading is paid per record rather than per append.

This is the defect the entry below called left standing on 2026-09-12, and that
entry was wrong in one way that mattered: it called the spool's accounting a size
question. It is a cost question, and what made deferring it look safe that
morning was that the spool was not yet full. Nothing drains it, so being full was
not a risk but a schedule, and the schedule came due the same day.

Measured after installing, with the spool held at its bound so that every append
evicts: four samples two minutes apart held 84,063 files and 104,857,291 bytes
against a bound of 104,857,600, sequences climbing about seven a minute, nothing
set aside, and the heartbeat at 62 to 64 seconds for forty minutes without a
pause. A `status` round trip took 45 to 81 milliseconds.

Two things the measurement corrected rather than confirmed. The first reading of
it was taken with `hexroutectl` under `sudo`, and the daemon authorises the
operator uid rather than root: `available: false` and the 24-millisecond replies
were `unauthorized_peer`, not a dead cycle. The second is that these round trips
are not comparable to the 9.8 and 1.21 seconds recorded for `take-the-probes-
together`, which timed a different command; the heartbeat cadence is the
comparable evidence and it is the one quoted here.

One cost stands unexplained and unaddressed. The daemon's process appears three
minutes before it logs `daemon_started` — 23:33:36 to 23:36:41 on this install,
and 17:32:58 to 17:35:52 on the one before it, so the three minutes predate this
change and are not a regression. Opening two spools and an archive over 130,000
files is the obvious suspect and it has not been measured. Nothing observes
during those three minutes.

`bound-the-archive-by-what-the-disk-charges` closed on 2026-09-12. Records
average 1.25 kilobytes and take a four-kilobyte block, so a bound counting
contents promised about a third of the room taken: an archive bounded at 256
megabytes held 96.7 and occupied 315. It now counts what the filesystem
charges, reports the number it counts, and its window is seven days rather than
the thirty that never applied.

Measured after installing: 318 megabytes occupied became 167, 81,330 files
became 42,805, and the depth became exactly seven days. 38,525 records went,
against a prediction of fifteen thousand — the prediction assumed the byte bound
would bite and it was the age window that did, harder. Thirty-five overflow
records stand in the archive naming what went.

The spool and the journals carry the same accounting defect and are not
corrected. Their accounting was left alone deliberately, and restating every
size assertion in their tests is mechanical work that was not safe to do
quickly. They hold 463 megabytes of disk for 141 of records. What that deferral
did not weigh is that the spool's byte bound decides when the expensive path is
taken — which is the stall `pay-for-eviction-not-for-being-full` answered, above.
The accounting itself is still logical and still uncorrected.

What follows is what the recent ones changed and what they left standing, kept
because the reasons are worth more than the record of having done them.

`take-the-probes-together` closed on 2026-09-11. The cycle starts its endpoint
probes together and waits once; only the waiting changed, because every answer
lands at its configured index and the fold walks them in that order. Which
failure is recorded when more than one fails is asserted rather than assumed.

Measured on the machine the same way the pause has been measured throughout,
twenty-five minutes and 24 samples: median 1.21 seconds, mean 1.37, one sample
of 24 above two seconds. About 1.3 seconds returned against a prediction of
about 1.4.

The arc, each step measured the same way: **9.8 seconds** typical before any of
it, **2.5** after the archive and the spool stopped listing their directories,
**1.21** now — against a fifteen-second deadline on the socket that was being
missed. What is left agrees with the parts: the slowest probe at about 0.7
seconds, the rest of the observation at 0.08, the durable writes at about 0.2,
the listings at about 0.2.

One thing is recorded rather than explained: a single sample of 24 reached 4.3
seconds. One outlier is not a pattern, and chasing it would mean profiling for a
shape that may not recur.

The retention window still stands untouched: 456MB across 176,000 files, which
is a decision about how much history is wanted rather than about cost.

What follows is what the recent ones changed and what they left standing, kept
because the reasons are worth more than the record of having done them.

`keep-the-spool-index-in-memory` closed on 2026-09-11, and with it the pause in
root's operator loop is accounted for end to end.

It was the same defect as the archive's, in the store beside it: the spool lists
its directory on every append, it lives inside the connectivity state directory
holding 107,600 files, and the journal mirrors every published fact into both.
The profile taken for the archive already named it — read as a call tree rather
than a leaf histogram, `spool.scanIndex` stands beside `eventarchive`'s frames.

Measured on the machine with both fixes installed, the same way the pause was
first measured:

| | before | after |
|---|---|---|
| pause | 6.6 to 13.1s | 2.4 to 4.6s |
| typical | about 9.8s | about 2.5s |
| period | about 70s | about 63s |

What remains is the observation itself — 2.2 seconds measured separately, of
which 2.09 is three endpoint probes taken one after another. The residual and
that number agree, so nothing is left unexplained.

Two things stand, neither a leftover of this. The probes are sequential and
taking them together would return about 1.4 seconds; that is worth proposing on
its own terms, and the loop now answers inside its fifteen-second deadline with
eleven to spare. And the stores hold 456MB across 176,000 files: the retention
window admits that, and narrowing it is a decision about how much history is
wanted rather than about cost.

What follows is what the recent ones changed and what they left standing, kept
because the reasons are worth more than the record of having done them.

`keep-the-index-in-memory` closed on 2026-09-11. The root runtime's operator
loop was stopping for six to thirteen seconds once a minute, against a
fifteen-second deadline on the socket it was failing to answer. A profile of the
live process put the time in `os.ReadDir` and the sort inside it.

The archive listed its directory on every append and a cycle appends eleven
records, so it listed 69,000 entries eleven times.
`bound-archive-append-cost` removed the decode from that listing in September
and left the listing; the requirement it wrote down said the cost still grows
with how many records are stored, which is true per append and misses that a
cycle makes many. The archive now learns its directory once and keeps it
current, dropping it whenever a write may have half happened.

Measured: eleven appends against 20,000 stored records cost 767ms listing each
time and 109ms listing once. At the 69,000 the live archive holds that returns
roughly 2.7 seconds per cycle.

Two things are left standing, both stated in the change rather than implied.
Observation accounts for 2.2 seconds of the pause and this for about 2.7, so
between one and eight seconds are still unexplained and want another profile
rather than another guess. And the stores hold 456MB across 176,000 files: the
retention window is what admits that, and narrowing it is a decision about how
much history is wanted, not about cost.

What follows is what the recent ones changed and what they left standing, kept
because the reasons are worth more than the record of having done them.

`make-a-failed-assertion-say-so` closed on 2026-09-10. Thirty-three assertions
in ten gates were written as bare conditionals, which the bash macOS ships does
not fail on, and `make check` — the only job that runs the shell gates — runs on
macOS. Each now says what to do when it fails and names the condition, and a
gate refuses the shape so it cannot come back by being copied from a neighbour.

Converting them found exactly one that was false, and it was a day old: the gate
added with the installer guard asserted that each installer asks its question
before replacing the binary, and compared against the first `install` anywhere
in the file rather than the one that replaces the binary. The installers were
right; the measurement was not. That is the argument for the whole exercise —
the shape hides the assertion that matters among the ones that happen to hold.

Two forms are left alone on purpose. A conditional heading an `&&` list is inert
too, and the four here are `break`, `continue` or a flag. A conditional that
ends a function is that function's return value, and a caller does fail on it.

What follows is what the recent ones changed and what they left standing, kept
because the reasons are worth more than the record of having done them.

`refuse-an-install-that-drops-authority` closed on 2026-09-10. The offline
check takes the installed configuration as a second input and refuses a
candidate that drops any of its settings; the installers ask before they touch
the binary or the plist and keep what they replaced; and a daemon whose store
holds an authority its configuration cannot read says so. That last guard is
the only one that works however the configuration arrived, including by hand.

It found something wider than itself. macOS ships bash 3.2, where `set -e` does
not fire on a failing `[[ ]]`, so an assertion written as a bare conditional
evaluates, reports false, and lets the script continue to its success message.
Thirty-three such assertions stand in ten gates under `tests/`, and on the macOS
runner — the only one that runs them — they check nothing. A first count of
sixty-seven in seventeen included conditionals inside `if`, `while` and
intentional `&&` lists, which are control flow rather than claims. The gate added here says what to do when each
assertion fails; the rest do not, and that has no change yet.

What follows is what the recent ones changed and what they left standing, kept
because the reasons are worth more than the record of having done them.

`name-the-refusal-nobody-wrote-down` closed on 2026-09-10. Two layers answered
above the rescuer and neither could write: the dispatcher's mutation gate and
the broker's answer when no reader takes the envelope. Both now report, and the
reporter is a required constructor argument rather than an optional one, so a
dispatcher or broker that can refuse and leave nothing behind is unbuildable.
The asking runtime stopped reporting the other side's internal failure as the
other side's refusal.

Only one of the two was reproduced on a machine. A spare user daemon with a
deliberately slow cycle recorded `request_not_taken` at the moment the
request's deadline expired. The gate refusal cannot be induced here at all: the
gate closes only on a suspended authorization or a store with no active
generation, both domains carry an active generation 4, and every route to the
closed state changes production policy state. That is the third condition in
this capability worth observing and impossible to produce on demand.

Installing the two daemons to prove it cost this machine its policy state and
found a hazard the procedure does not mention. The documented install command
names `private/<domain>-observe.json` and the installer copies it over the live
configuration without comparing them. Both checkout copies were stale, so the
install replaced runtimes holding a signed generation 4 with runtimes holding
no policy, and root lost the named service that is its whole rescue capability.
Both were rebuilt from the active manifest and the signer's public key, matched
by digest rather than by path. It was survivable only because the policy store
is a different file from the configuration, and it will happen again to anyone
who follows the documented command with a stale working copy.

`name-what-a-reconnect-could-not-do` closed on 2026-09-10. A reconnect now says
which step it stopped at. Proving it on this machine did not work as planned:
the only fault that can be induced by hand is a blackholed tunnel, and the
planner answers that with a rescue rather than a reconnect, so the named
failures remain unexercised on live hardware. The induction found the silent
refusal above instead.

It also left one thing measured and undiagnosed: root's observation cycle stops
answering its own operator socket for 6.6 to 13.1 seconds once every ~70
seconds. Over 400 requests the pause held that band without a trend — a first
short sample looked like growth and a longer one does not support it. What
matters is the ceiling rather than the direction: the I/O deadline on that
socket is 15 seconds, and a pause that reaches it stops producing a late answer
and starts producing a caller that gives up, which reads as a failure rather
than as the slow answer it is. That is not a refusal problem and has no change
yet.

`cut-pritunl-recovery-to-hexroute` closed on 2026-09-09 with its behaviour built
and specified, and with one thing it does not have: no act has ever been
performed under this authority.

Neither half can be induced, which was measured rather than assumed. A service
launchd keeps alive reports `spawn scheduled` and then `running` after a kill,
never the `not running` that staleness means. A session stopped three times, with
autostart on and off, came back in 8, 16 and 24 seconds — the Pritunl service
restores an active profile whatever that flag says. Both components repair
themselves faster than this runtime observes, which is their design and not a
fault in it.

So the approval path has never run outside tests, and the evidence for it can
only come from a real fault. That is not hopeless: the user runtime's own log
holds 349 reconnect proposals across 27 days, which are the occasions Pritunl's
supervision did not repair quickly. What changed today is that the next one will
be noticed within a cycle and its outcome named — neither of which was true this
morning. It moved the whole Pritunl
recovery path — observation, decision, one-time code, reconnect and the request
to restart a stale service — and grants the first production authority in this
system: one named service restart, on a typed credential-free request that root
revalidates itself. The authority arrives as a signed policy generation and is
checked through the mutation gate the operator dispatcher already consults on
exactly this action.

What remains is the transaction itself and its evidence, which are an operator
action rather than a commit: no generation grants the capability yet, so both
daemons still propose and neither acts. The procedure and its rollback are
written down in advance in
[`docs/macos/pritunl-recovery-cutover.md`](macos/pritunl-recovery-cutover.md),
because a rollback discovered during an incident is not a rollback. It is item
7 below.

`bound-archive-append-cost` closed on 2026-09-09. It is the same defect
as `bound-spool-operation-cost`, in the store that change did not touch.
`Archive.scan` reads and decodes every stored record, and `Append` calls it on
every record — twice when anything is evicted — while the connectivity journal
mirrors every fact into the archive synchronously. Measured on this repository's
records: 18ms per scan at 1,000 stored records, 189ms at 10,000, and 898ms at the
41,492 the live root archive had reached.

That is what stopped the root daemon answering. It burned 23.3 CPU-seconds in
every 30 of wall time, a CPU profile put the burn in this package, and every
connectivity publication was refused for about a day because the publisher bounds
a publication at a third of its cycle and root could not answer inside it.
Nothing about the network, the socket or the exchange was wrong; the diagnosis
took a day because neither side's log named its own fault, which
`name-what-the-ipc-refused` has since fixed.

Appending now costs 123ms at those 41,492 records against 898ms, and 15ms at a
thousand. The cost still grows with the record count — the total size can only
come from the filesystem — so the specification says that rather than the
stronger thing first written, exactly as `bound-spool-operation-cost` had to
after measuring. Installed on the live host the same day: the root daemon's
CPU fell from 27.2 seconds in every 30 of wall time to a mean of 6.2, the
connectivity stream advanced in every observed window, and the publication
deadline went back to `interval/3` from the twelve-second binary that had been
put there to establish that root was the slow side. No publication has been
refused since.

The archive is harder than the spool in one place: the spool has no age bound, so
it never needed a timestamp from inside a record. Records are named by a
monotonic sequence and appended in order, so the oldest retained record is the
lowest retained sequence — one read says whether anything expired, and further
records are read only while they are themselves expired. Retention is
deliberately unchanged: the same bounds, the same priority-ordered eviction, the
same overflow accounting. An entry-count bound is a question about what the
archive should keep rather than about what an append should cost, and it is asked
separately.

`bound-spool-operation-cost` closed on 2026-09-07. `Spool.Append` called
`scanStable`, which read and fully decoded every stored record — from all six
operations that called it, so asking the spool its own size re-proved every
record it held. Appending now takes the highest sequence from the filenames and
the total size from file metadata, and opens a stored record only when it must
evict, which below the bound it never must. Measured at the scale the machine
reached: 191ms per append at 60,000 records against roughly 1.7s.

Nothing had overflowed — both spools sat inside their hundred-megabyte bound —
so the missing statement was about cost rather than size, and the specification
says what was measured rather than the stronger thing first written: the cost
still grows with the record count, bounded by the byte bound. Two things
travelled with it. A damaged record now stops its own use and nothing else,
because refusing to append meant losing every future observation to recover one
already beyond saving; it is set aside rather than deleted and reported by
writing an incident into the spool, the way overflow already was. And the user
daemon's publish gained a deadline derived from its observation interval instead
of a fifteen-second default against a fifteen-second cycle — the coupling that
carried a fault in root into a symptom in user.

That unblocked the activation the change below was stuck on, and the root daemon
answers `policy status` in 44ms where it had not answered at all.

`permit-two-sided-action-capabilities` closed on 2026-09-07, and item 7 now
waits only on a date. Generation 4 — the first generation in this system's
history to expand authorization rather than only withhold it — is compiled,
diffed and replayed, and the signer application was rebuilt so the compiler
carrying the repair is the one that produced it. Its diff carries exactly two
entries, one newly allowed action per domain, and nothing else.

The ceremony is held until 2026-09-09 on purpose. Generation 3 expires
2026-09-16, so the seven-day announcement crosses on the ninth, and that
crossing is the last open task of the expiry change: only a real one proves the
incident reaches a person rather than merely being raised. Activating generation
4 first would supersede generation 3 and move the crossing three weeks out.
Waiting costs nothing operationally — the Pritunl transaction begins by
disabling the legacy watchdog, so until it runs, nothing changes.


Four places agreed with each other and disagreed with the detector — the runbook,
both runtimes' calls with the target written as a constant in root's, and the
safety envelope, which lists the capability under both domains and explains why
root names `pritunl` rather than folding it into `runtime`. The detector's only
test uses a credential selector, which is the case it was written for: one key
has one owner. It was then applied to every selector kind alike.

The line it drew is what an overlap contradicts. A credential, a route
and an endpoint are singular on the host, so two domains claiming one denies a
fact. An action capability is not singular: the envelope assigns capability and
target per domain, may assign the same pair to both, and has already agreed by
the time conflicts are looked for. Differing effects across domains compile too,
because root allowing while user denies is what a partial rollback leaves.

Splitting the capability in two was rejected: it needs no rule change and gives
each name one owner, but two capabilities can be revoked one at a time, leaving
root able to restart the service after the user runtime has lost the right to
reconnect — the drift a single grant exists to prevent.

Nothing caught this because no test ever compiled a generation granting the
capability. It appears in one test file, which evaluates authorization against a
payload written by hand and never reaches the compiler. Every part was proven
and the path between them was not.

`recover-policy-generations-after-expiry` closed on 2026-09-09, and item 7 is no
longer blocked by it. Generation 3 went active in both domains on 2026-09-07
expiring 2026-09-16, and that window was nine days rather than thirty so the
seven-day announcement could be observed on the real thing rather than only
under an injected clock.

Observing it is what closed the change, and it earned its place. The crossing at
2026-09-09T07:56:38Z was announced seventeen seconds later and went unread —
because Notification Center already held five identical announcements for the
same generation from 2026-09-07, one for each of that day's restarts. The
suppression the specification requires lived in a map built empty at start, so
it lasted the life of the process rather than the life of the generation. The
delivery record now outlives the process, and only for identities that do: a
policy generation is the same number in the next process, a runtime's state
generation is not. The active policy generation had expired
on 2026-08-22 and this machine was locked out of its own policy control plane: the installer refuses to place the
successor generation because it cannot revalidate the expired predecessor, and
the predecessor cannot be revalidated because time passed. Every other cause of
that refusal has an operator action that clears it; expiry's only cure is the
next generation, which is the thing refused.

Measured rather than reasoned: inside its window the expired pointer passes
every check — signature, digests, immutable artifacts, trusted compiler. Two
comparisons stand in the way, and both ask about the present rather than about
the artifact. So the defect is that the installer asks a historical question
through the function that answers an operational one, and lineage is separated
from authority: what proves the chain keeps every cryptographic check, and drops
the validity window and the installed static digest. The relaxation returns a
record that carries no manifest, payload or approval, so authorizing from a
lapsed generation is unwritable rather than merely discouraged.

Expiry also stops being reported as a `clock_anomaly` on a machine whose clock is
correct, and stops raising a suspension that contributed nothing — mutations were
already refused for the absence of an active generation. Removing that overlay
removes the only loud signal, so the announcement is in the same change rather
than a later one: `expires_at` becomes visible, and a bounded incident arrives at
seven days, at forty-eight hours, and once a generation has lapsed unreplaced.
Nothing announced expiry before, which is why nobody saw this one.

`admit-a-second-ingress-client` closed on 2026-09-06. This system now records
who reaches an ingress and what for — which nothing had ever done — and an
operator can verify a published configuration version and read back the exact
bytes the host will run, so that a client profile describes the server that will
answer rather than the draft it was built from. Delivery stays blind to what it
delivers, and that is now a requirement rather than an accident of how it was
written.

`deliver-signed-ingress-configuration` closed the same day. An ingress now
fetches a version by its own action, verifies the signature against the key
placed on it when it was built and the digest against the bytes it received,
and keeps serving what it has when either fails. It retains the version it was
running, so returning needs no network, and it returns when the applied version
has not reported itself healthy through the signed heartbeat before its window
passes. `config_versions` and `deployments` have their first producer, and the
worker that records what became of a version still cannot bring one into
existence. It was expected to unblock item 6 below; the grill of that item
found the item itself was wrong, and took it apart instead.

Previously closed: `record-ingress-fleet-purpose` closed on 2026-09-05, recording what each
ingress host is for, correcting the provider-B lifecycle state and rewriting
item 4 below. The three before it closed on 2026-09-03 and 2026-09-04:
`add-observable-connectivity-state-machine`, `add-local-event-archive` and
`add-private-incident-bundles`, with `observe-sentinel-recovery` alongside them.

## Ordered Changes

1. ~~Complete and run the operational acceptance drill for the current working
   path, including baseline and recovery evidence.~~ Done. See the archived
   `add-operational-acceptance-drill` change and
   [`docs/testing/operational-acceptance.md`](testing/operational-acceptance.md).
2. ~~Qualify the observable connectivity state machine without enabling
   production mutations.~~ Done. The evidence chain closed on 2026-09-03 with
   260,921 eligible seconds against the 259,200 required, two sleep/wake
   cycles, one reboot and all thirteen mandatory faults injected. Evidence is
   retained privately.
3. ~~Complete root observe-only soak and resolve every materially divergent
   proposed action without enabling mutations.~~ Done, and closed by that same
   evidence: divergences were zero and nothing was left unbound, so there was
   no divergent proposed action to resolve. The sentinel observes recovery and
   records the plan it would run, with no restarter attached.
4. ~~Run an evidence-based provider-B bake-off and deploy an independent
   VLESS/Reality ingress in a different provider and ASN.~~ The deployment half
   was delivered before this item was read: the provider-B ingress exists, its
   provider and ASN are distinct, and a private workload consumes it. The
   bake-off half cannot be run and was ruled out on evidence — the word appears
   nowhere in this repository except in this line, no criterion or threshold is
   written anywhere, and exactly one provider serves ingress, so there is no
   second candidate to compare against. Standing one up purely to have a
   comparison would pay for an answer nobody needs. What remains is recorded by
   `record-ingress-fleet-purpose`: the fleet's purposes, the corrected lifecycle
   state, and the failure domain that two configured entries share.
5. ~~Add signed configuration delivery to an ingress, with verification on the
   host and return to the previous version when the running generation does not
   prove healthy.~~ Done. See the archived
   `deliver-signed-ingress-configuration` change. It was moved in front of the
   Telegram item, which was numbered 5 and could not be started without it: the
   provider-B unit refuses to start without a runtime configuration file that
   Terraform deliberately does not write, so the only way that file changed was
   a person opening bounded operator access. `proven` needed no new notion of
   success, only a connection — but the connection needed one thing the fleet
   did not have, because the observer reported the generation it was built with
   and every version would have proved the moment the host was reachable. The
   host now records the version it applied and reports that instead.
6. ~~Deploy and qualify two-provider Telegram ingress using native MTG, SNI
   pass-through and functional MTProto health evidence.~~ Taken apart rather
   than done. Its grill repositioned the purpose: the proxy looked like the
   thing keeping the alert channel alive, and is in fact about using Telegram
   from a phone without installing a VPN client. Under that purpose a client
   already on the phone carries everything rather than one application, over the
   strongest protocol on the fleet, and costs the server nothing — while MTProto
   would put a protocol that is fingerprinted and blocked at scale beside
   Reality on one address, where a probe that flags the weaker one has found the
   stronger one too. MTG, the Nginx split, the second provider and a client
   application of our own are all ruled out with their reasons in
   `admit-a-second-ingress-client`, which carries the public remainder: record
   who the clients are, and derive a profile from the version that was
   published rather than the draft it came from. The rest is private work.

   Two things that grill established still stand. The provider-B host cannot
   take a second public port — but not because its module specifies one. The
   module accepts up to eight rules and allows only ports 22 and 443, requires
   exactly one global 443 rule, and permits 22 only from `/32` networks, so any
   other port fails validation before apply. And SNI is secret material by this
   repository's own contract test, so a demultiplexer's configuration could
   never have lived in Terraform.
7. Cut user Pritunl recovery ownership from the legacy OTP watchdog to
   `hexroute-userd` transactionally. Moved in front of the root tunnel cutover,
   which was numbered 7, on three measurements. This one is nearly built and
   held back rather than unfinished — `credentials` is 442 lines against 234 of
   tests and `pritunlrescue` 308 against 202 — while the root cutover has no
   executor at all and a guard that refuses to name a production thing.
   Failure here means Pritunl does not reconnect by itself; failure there means
   the machine has no tunnel. And both are the same transaction — boot out a
   Twilight agent, hand ownership to a Hexroute daemon, prove it, put the old
   one back if it does not — which is cheaper to learn where being wrong costs
   a manual reconnect.

   It is not free of the other item: the Pritunl service is a system daemon, so
   restarting it needs root that `hexroute-userd` does not have. That makes
   this the smallest possible first grant of production authority — one
   `launchctl kickstart` of one named service — rather than an argument against
   the order.

   The ceremony ran on 2026-09-09. Generation 4 is active in both domains, and
   both `newly_allowed` entries of the signed diff are live — one root, one user.
   That is the first grant of production authority in this system, and it is the
   two-sided capability the compiler had to be rebuilt for.

   What remains are the two proofs that wait for something real rather than for
   a command: the user half by a reconnect actually occurring, the root half by
   inducing a stale service rather than by writing the request by hand. It waits for the whole of that change rather than its first
   part: task 4.1 proves the user half by waiting, the soak and the validity
   bound are the same order of magnitude, and starting a multi-week wait whose
   sample can end silently — before the thing that makes it non-silent exists —
   would set up the experiment that just failed.
8. Cut root tunnel ownership from Twilight to Hexroute transactionally. Its
   grill was run before the reorder and settled its shape, recorded here so it
   is not derived again.

   The unit is the whole supervisor, not the tunnel. `com.twilight.supervisor`
   runs 999 lines in which the tunnel is one job among several, and the monitor
   loop interleaves route enforcement, ingress failover, the reserve probe,
   automatic restore and the Codex and Pritunl paths. Measured against the live
   configuration the set is about ten behaviours rather than thirty-two: the
   OTP watchdog supervision, XRay auto-recovery and health-driven restart are
   all switched off.

   sing-box stays, and so do its bytes. The configuration moves into a signed
   configuration version — the format from item 5, which never looks inside
   what it carries — with the first version byte-identical to the file running
   today, so that the only thing the cutover changes is who owns it. The Mac
   does not pull it over the network: a host needs its tunnel configuration
   exactly when it has no network.

   The transaction is held by an operator command in the foreground, not by a
   daemon. Handing a Hexroute daemon the authority to start Twilight's launchd
   label would outlive the forty seconds it is needed for, and a written
   procedure is not a transaction at all. A durable session envelope covers the
   terminal dying mid-switch. Handing over through Twilight's own lock is not
   available: it defends only against another `twilight-up.sh` and breaks any
   other holder's claim.

   Completing the switch requires evidence that traffic traversed the tunnel,
   not that an interface came up. This repository has already written down that
   a reachable socket is not qualification, and has already made the weaker
   mistake once.

   One thing is owed before it: the Codex fallback watchdog is a child of the
   supervisor that loops while its parent lives, so booting the supervisor out
   takes the fallback with it. It is lifted into its own agent first — no
   roadmap item moves it otherwise, and an escape hatch must not belong to the
   experiment it exists to escape.
9. Complete public qualification, supply-chain evidence and legacy cleanup.
   Two leftovers are named here rather than left to be found. Hexroute now
   depends on Keychain items under legacy names, on a critical path: they were
   read where they are because moving one means reading a one-time-code seed
   out through a shell to change a string, which is a real exposure for a
   cosmetic gain. And the ingress fleet's hosts are still provisioned outside
   Terraform and adopted by nothing.

Item 4 also ruled out two placements that were proposed and withdrawn during its
grill session. Moving the named-country host nearer the operator was recommended
before its purpose was known and would have cost latency to serve a need already
met. Holding a permanent host for the occasional heavily filtered country was
dropped because the need is rare, is met by buying a service locally, and could
not be verified from any vantage point that exists.

Each numbered item requires its own grill session and bounded OpenSpec change.
No future item may be bundled into an earlier cutover merely because
supporting code already exists.

## Owed

Findings from grills that no change has taken. They are not unwired code, so the
census below does not see them, and they are recorded here rather than left to
be found again.

**The external monitor has one alert contact, and it is Telegram.** Every check
in `terraform/modules/uptime-checks` delivers through a single integration.
UptimeRobot supports mail, SMS and push; the module exposes none of them. All
external monitoring therefore depends on one channel, and it is the channel
whose reachability was the subject of the item-6 grill. Cheap to fix, and not
fixed. Found 2026-09-06.

**A version is called proven when two sockets accept a connection.** The signed
heartbeat's transport health is two TCP dials in `internal/ingressobserver`,
which is what the whole prove-or-return path rests on. The provider-B document
calls that insufficient in its own words — "a reachable socket is not
qualification" — and it is right. Found 2026-09-06.

**The Telegram availability calculation has no producer, and now has no
subject.** `CalculateTelegram` and `CalculateTelegramFailover` are called only
by their own tests: nothing has ever produced the state they consume. That made
it the fifth thing here found designed and never given a producer. After item 6
was taken apart it is worse than unused — it computes an objective for a service
that will not be built, and should be removed or redefined rather than left to
look like a commitment. Found 2026-09-06.

## Debt

Code that exists and no binary contains. Each entry is a claim this repository
has made and not yet kept; the list is enforced by `make check`, so it cannot
grow in silence.

- `resumeexecutor` — operator resume enforcement.
- `policyadvisor` — redacted policy observability.

`credentials` and `pritunlrescue` left this list on 2026-09-06, when the user
cutover gave them the binaries they were held back from: the user daemon holds
the credential and asks, and the root daemon verifies and restarts.

`incidentbundle` and `objectstore` left this list on 2026-09-04, when the
maintenance worker began calling bundle creation and expiry: the census fell
from six packages and 1800 unrun lines to four and 638. `reconciler` and `slo`
left it on 2026-08-30, the first when its shadow status became answerable and
the second when its calculation was scheduled. It still compares nothing: correlating its planner output with the
read model's proposals is part of item 2.
