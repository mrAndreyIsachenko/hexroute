# Carry a host event to an alert

## Why

Item 9 closed on 2026-10-05 owing one thing, and said so: "Telegram is still
owed rather than delivered: there is no path from the root runtime to an alert at
all." The runtime that gives the tunnel up leaves word in a file the operator's
own session reads. If the operator is not at the machine, nothing reaches them,
and the state that word describes is the machine having no supervised tunnel
until someone runs `resume`.

Both ends of the path exist and are tested. `internal/alertdelivery` has a policy
with a night window, a transactional outbox with leases and retries, a processor
and a Telegram client, and `cloudruntime` wires all four.
`cloudincident` correlates, opens and clears durable incidents.
`internal/telemetry` has an uploader that signs batches, applies
acknowledgements, detects sequence gaps and repairs them. The cloud's silent-node
evaluator even carries a branch for a Mac that is asleep.

What does not exist is the leg between them. Measured 2026-10-09:

- The host has never uploaded. `telemetry.NewUploader` and
  `cloudingest.NewHTTPTransport` are both recorded in
  `tests/unbuilt_capability_test.sh` as waiting, with the reason "no host binary
  uploads; the local archive covers retention".
- The cloud can therefore only infer an incident from **absence**:
  `cloudincident.SignalFromSilentDecision` is the only constructor there is.
  Nothing turns an event a host pushed into a signal.
- `notification.ExternalPending` and `ExternalDelivered` are set only by tests;
  all three production call sites pass `ExternalNotRequired`.
- `alertdelivery.Policy.Plan` writes a `local_macos` row for every actionable
  incident, and `ClaimDue` refuses every channel but `telegram` and
  `morning_digest`. Retention deletes only `delivered` and `suppressed`. The row
  is created, can never be claimed and can never expire.

This change is the first of three. It gives the host a way to say something and
the cloud a way to turn that into an alert. The second teaches the Mac to
announce its own sleep, which is what makes silence mean something. The third
gives the local channel a claimer.

## What Changes

- A scheduled root agent drains the upload spool through the existing uploader
  and the existing signed-ingestion transport. It runs on a relaxed interval for
  the ordinary stream, and immediately when the runtime writes an incident,
  because that is the only record anybody is waiting for.
- The runtime records an incident where the upload queue can carry it. The
  connectivity journal gains a second door as narrow as its first,
  `AppendIncident(event.Incident)`, so the spool keeps one writer and one
  sequence counter — the counter the cloud's cursor and gap model is built on.
- Handing the tunnel back opens an availability incident, and taking the tunnel
  again clears it. `event.IncidentResolved` and `cloudincident.ConditionCleared`
  get their first producers.
- The cloud turns any uploaded `incident.lifecycle` into a signal, correlated by
  node, category and component rather than by the host's per-event identity, so
  a repeating condition is one incident that updates instead of thousands.
- The root installer generates the node key if it is absent, takes the node
  identity as input and prints the public half once, while the operator is
  present. Key material stays private.
- The Mac is registered as a reporting node with a 24-hour expected heartbeat.
  Silence then means "said nothing for a day", which nightly sleep does not trip
  — and it is what detects this path failing, because an uploader that stops
  being heard from is the one failure the pushed half cannot report.
- A channel nothing can claim is no longer planned, with the reason recorded.
- The baseline already forbids an acknowledgement reaching policy, reduction or
  action packages. Nothing holds it. A reachability gate now does, because this
  change creates the first cloud-to-host reply path on this machine.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `cloud-telemetry` — a host reports its own events; a pushed incident becomes a
  signal and how it correlates; a channel is not planned without a claimer; the
  acknowledgement prohibition is held mechanically.
- `tunnel-supervision` — handing the tunnel back opens an incident that can leave
  the machine, and taking it again clears it. The file the operator's session
  reads stays, because the reason it exists has not changed: the root runtime
  still cannot speak to that session.

## Impact

- A new root binary and launchd agent, their wrapper and their installation.
- `internal/connectivityjournal` gains one typed write path.
- `internal/cloudincident` gains one signal constructor.
- `internal/alertdelivery` stops planning an unclaimable channel.
- `internal/rootdaemon` records an incident at handback and at resume.
- The node identity, its key and its registration are private and belong to
  `hexroute-infra`. This repository carries the procedure.
- Nothing in Twilight is read or changed. AdGuard is untouched. Both Codex paths
  are untouched.
