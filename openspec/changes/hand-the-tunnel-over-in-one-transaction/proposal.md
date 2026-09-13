# Hand the tunnel over in one transaction

## Why

Item 8, and the first change in which this runtime performs anything. It decides
what a tunnel owner would do, records what it decided from, and asks whether it
would be permitted — and owns nothing.

## What the machine actually does today

Measured from the live supervisor rather than from its source. Twelve behaviours
are switched on, and four that a reader of the script would expect are off:
XRay auto-recovery, OTP watchdog supervision, restart on health failure, and
restart when the outer path is restored.

That last one matters beyond its own scope. `link_returned` is one of the six
causes this runtime decides on, and the runtime that owns the tunnel is
configured never to act on it. It was recorded as a cause that had not occurred
in sixty-one days; it could not have.

## What Changes

- Six behaviours move: starting sing-box and restarting it when the process is
  gone, the carrier changing, the wake gap, the payload path failing, the scoped
  routes, and holding the machine awake.
- Six stay with the supervisor, which is not booted out: selecting the ingress
  with its quarantine and automatic restore, the Codex fallback's health, the
  startup probe, the reserve probe tick, and the observing health probe.
- Ownership passes through a file the supervisor reads on every tick and before
  every start of sing-box. Only the operator's transaction writes it, and abort
  removes it.
- The configuration sing-box runs becomes a signed version, the first byte for
  byte what runs today, verified at every start. A version that does not verify
  is not started and ownership goes back.
- The transaction completes on two consecutive payload probes that prove traffic
  traversed, within 120 seconds, and aborts to the supervisor otherwise.
- It runs first as a rehearsal that performs every phase except the handover.

## Why the supervisor is not booted out

The grill of item 8 settled that the unit is the whole supervisor. Measurement
revised it: the supervisor has no reduced mode — `TWILIGHT_SUPERVISOR_MODE` is
written by its installer and read by nothing — and its ingress selection changed
91 times in 61 days, which is more often than anything about the tunnel. Booting
it out to take the tunnel would drop a behaviour that fires half again a day to
gain one that fires every other week.

## Impact

- Affected specs: `tunnel-ownership-handover` (new), `tunnel-supervision`
- Affected code: `internal/tunnelhandover` (new), `internal/rootdaemon`,
  `internal/configversion`, `cmd/hexroutectl`; and `twilight`, which gains the
  ownership file and stops starting sing-box when it is held.
- Depends on: a generation granting `tunnel_ownership`, which is a ceremony with
  an operator's password and is not performed by this change.

## What must be proved before it runs

That this runtime's targets cover what the supervisor's two route scripts apply.
It plans routes for ingress, corporate, GitLab HTTPS and the Codex fallback
across both the physical link and the tunnel — the same ground `twilight-routes`
and `twilight-direct-routes` cover — but the same ground is not the same list,
and a destination only the supervisor knows would simply stop being routed.
