# Pritunl Recovery Cutover

This is the transaction that moves Pritunl recovery from the legacy watchdog to
Hexroute, the rollback for it, and what has to be true before it can be called
done. It records no profile identity, no service label and no Keychain item
name: those live in the private infrastructure repository, and the commands
below take them as values the operator supplies.

It is written before the cutover is performed, because a rollback discovered
during an incident is not a rollback.

## What changes hands

The user runtime holds the credential, derives the one-time code and reconnects
the session. The root runtime restarts the service when the user runtime asks
and its own probes agree. The legacy watchdog stops.

Nothing else moves. The root tunnel and its supervisor stay where they are, and
so do AdGuard and both Codex paths.

## Before

Three things, and the order matters only in that all three precede the
transaction:

1. **The generation exists and is signed.** It grants the Pritunl recovery
   capability to the user domain on the `pritunl` target and to the root domain
   on the same, and grants nothing else. It is signed with the operator key
   under user presence, like any other generation.
2. **Both runtimes are configured to act.** The user daemon has its recovery
   section — client path, profile, mode, and the Keychain item names — and the
   root daemon has the service label. Without these a granted generation
   authorizes an act neither can perform, which is reported as degraded rather
   than hidden.
3. **A baseline is taken.** The claim afterwards is "no worse than before", and
   that needs a before: the legacy watchdog's log, counted, with the period it
   covers.

## The transaction

Two steps. Disable first, activate second.

```bash
sudo launchctl bootout "gui/$(id -u)/<legacy watchdog label>" 2>/dev/null || true
sudo launchctl disable "gui/$(id -u)/<legacy watchdog label>"
```

`disable` is the load-bearing half. A bootout alone does not survive a reboot,
so the agent would return at the next login and two watchdogs would run against
one profile — one stopping what the other started, burning a one-time code each
round, with neither able to see the other.

Then activate the generation that grants the capability, by the ordinary policy
path. From that moment the user daemon's next cycle acts on what it decides
instead of proposing it.

The gap between the two steps is the only window in which nothing watches the
session. Reconnects run at a few a day, so the chance of needing one inside a
hand-operated window is negligible, and the next cycle picks it up regardless.

## The rollback

Two steps, in the reverse order, and neither touches the Keychain — which is why
the items are read where they are. Nothing in the recovery path has to be sound
for the recovery of the recovery path.

Roll the policy generation back by the ordinary policy path. The capability is
gone at once: the next cycle asks, is refused, and reports a proposal exactly as
it did before the cutover.

```bash
sudo launchctl enable "gui/$(id -u)/<legacy watchdog label>"
sudo launchctl bootstrap "gui/$(id -u)" "<legacy watchdog plist>"
```

The legacy watchdog resumes from its own state. It reads the same Keychain
items, which is why nothing about them changed.

## Proving it

The two halves fire at completely different rates, so one rule would be wrong
for one of them.

**The reconnect half is proven by waiting.** It runs at a few a day with only a
handful of empty days in a month and a half, so a soak of the usual length is a
real sample rather than a hope, and the short-code-window and backoff paths turn
up in it on their own.

**The restart half is induced.** It fires perhaps weekly and in bursts — the
largest run in the measured period was one incident over two days — so waiting
for it means holding an untested grant of root authority until an outage.

Induce the **precondition**, not the request. A request file written by hand
would prove the handler and skip the detection, and detection is the half this
moved.

**Booting the service out is the wrong precondition, and this was written before
that was known.** It unloads the job entirely, `launchctl print` then exits
non-zero, and the root verifier answers "not stale" deliberately — a service
that is not loaded is not something this runtime restarts. Stale means the job
is still loaded and reports `state = not running`.

Run on 2026-09-09, that induction produced a correct refusal and proved
everything up to it: the user runtime noticed the missing service within one
cycle, degraded across the whole outage, requested the restart, and root looked
and disagreed. It cannot prove the link after that.

**No induction of an absent service can work here, and that was measured.**
Sampling a synthetic `KeepAlive` job every 100ms for twelve seconds after
SIGKILL: `spawn scheduled` for about 8.2 seconds, then `running`. `not running`
never appeared. Staleness is exactly `not running` on purpose — a service that
is starting says so, and restarting one mid-start interrupts the recovery
already under way — so for a service launchd keeps alive, launchd's own restart
is the recovery and this runtime declines to interfere with it.

The root half's real trigger is the one this document should describe instead: a
session reporting itself connected while carrying no traffic. That is the
condition the capability exists for, and inducing it is a different problem than
stopping a service.

## What closes it

Both halves observed, the fault paths seen at least once each — a refusal on
authority, a refusal on the service's state, and a reconnect that failed — and
no regression against the baseline.

Evidence stays private. The logs carry a live profile identity and a service
label, and neither belongs in this repository.
