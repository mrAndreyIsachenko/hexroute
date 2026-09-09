# Take Pritunl recovery, and grant the first production authority

## Why

Hexroute already decides when Pritunl should reconnect — its planner runs every
cycle in `hexroute-userd` and emits a verdict with a typed reason — and it can
do nothing about it. The two packages that would act are written, tested and in
no binary. Meanwhile the legacy watchdog acts, and passes the PIN and the
one-time code concatenated in a command-line argument, where any local process
can read them.

## What Changes

- Move the whole recovery path: observation, decision, one-time code, reconnect,
  and the request to restart a stale Pritunl service. The legacy watchdog is
  booted out **and disabled**, because a bootout does not survive a reboot and
  two watchdogs on one profile would race for a thirty-second code window.
- Grant `hexrouted` one production capability: restart one named system service,
  and only on a typed credential-free request it revalidates with its own
  probes. This is the first production authority in this system, and it is the
  smallest one available.
- Authorize that capability with a signed policy generation, checked through the
  mutation gate the operator dispatcher already consults on exactly this action.
  The gate answers whether this runtime may act at all; the capability answers
  whether it may perform this act, and until now nothing asked the second
  question. Rollback is a policy rollback.
- Submit the secret through the client's password-read path rather than an
  argument, so it never appears in the process table.
- Make inner health authoritative for the rescue request, and only for it. A
  profile that reports itself connected while carrying no traffic is the most
  recent real cause of a service restart, and nothing in the new path would
  otherwise see it.

## Capabilities

**New Capabilities**

- `pritunl-recovery-ownership` — who may recover a Pritunl session, how the
  request crosses the privilege boundary, and what authorizes the act.

**Modified Capabilities**

- `local-control-plane-foundation` — the observe-only prohibition is lifted for
  this one path, under a signed generation, and the separation between root and
  user authority is stated as what survives the lifting.

## Impact

`hexroute-userd` gains credential reading and the reconnect; `hexrouted` gains
one revalidated restart. Two packages leave the unwired census and the mutation
gate gains its first caller. The legacy watchdog stops. Nothing else on the host
changes: the root tunnel, its supervisor, AdGuard and both Codex paths are
untouched.

## Non-Goals

- **The root tunnel.** It is the next item, and this one exists partly to
  rehearse its transaction where being wrong costs a manual reconnect rather
  than the machine's only tunnel.
- **Moving the Keychain items.** They are read where they are. Renaming means
  reading a one-time-code seed out through a shell to change a string, and the
  cleanup item exists for exactly this kind of leftover.
- **Rotating the one-time-code seed.** Sound hygiene at a change of owner, and a
  manual re-enrolment that is not a condition of moving ownership.
- **Making inner health authoritative for reconnect.** What Pritunl says about
  its own session stays authoritative there; a path measurement is affected by
  the outer tunnel and the routes, and would reconnect Pritunl for faults that
  are not its.
- **Any second authorization mechanism.** The gate exists.

## Rollout

Two reversible steps in one operator transaction: disable the legacy watchdog,
then activate the generation that grants the capability. The gap between them is
bounded by the operator's own hand and costs nothing measurable — reconnects
happen a few times a day, not a few times a minute.

## Rollback

Roll the policy generation back and the capability is gone at once; enable and
bootstrap the legacy watchdog and it resumes. Neither step touches the Keychain,
which is why the items are read where they are: nothing in the recovery path
needs to be sound for the recovery of the recovery path.

## Ownership boundary

Public Hexroute owns the planner, the credential handling, the typed request,
the root handler and the authorizing capability. The private repository owns the
profile identity, the service label and the soak evidence. Twilight keeps the
root tunnel and its supervisor until the next item.
