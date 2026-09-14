# Hand the tunnel back

Linear: HEX-14

## Why

Since the handover of 2026-09-14 nothing restarts the tunnel. The supervisor
stands down while the claim is held, the sing-box this runtime started has no
parent but launchd, and this runtime has no executor. Twilight rebuilt the
tunnel 35 times in the 63 days of its log — 13 of them after the machine woke —
and none of those now happens.

The grill of item 9 settled that the executor is proved by a soak against
Twilight acting, which needs the tunnel back with Twilight. There is no way to
give it back. `hexroute-handover abort` undoes a transaction in flight, and a
completed handover clears its session, so after one `abort` answers "no handover
was in flight" and does nothing.

Releasing the claim by hand is not a way either. Twilight finds a stale sing-box
by the path of its own configuration, and the one this runtime started runs from
another. The supervisor would start a second tunnel beside it, fail on the taken
interface, and be restarted by launchd for as long as nobody noticed — the loop
the handover produced once already, and this time with no claim to end it.

The same measurement exposed a defect in the handover itself. This runtime takes
the first process named `sing-box` as the tunnel, and an ingress probe runs
sing-box too. The handover stopped the right process on 2026-09-14 because the
tunnel's pid happened to be lower.

## What

- `hexroute-handover release`: a transaction that gives the tunnel back. It stops
  the sing-box this runtime runs, releases the claim, and completes on two
  consecutive proofs that traffic traversed the tunnel Twilight raised. If
  Twilight does not raise one inside the deadline, it starts this runtime's
  tunnel again from the signed version and places the claim again.
- The tunnel process is identified by the configuration its owner runs, never by
  name: in the handover's possession step, in `release`, and in the daemon's
  `process_gone`. The claim records the path of the configuration its holder
  runs.
- Twilight takes a new carrier baseline when the claim goes away, instead of
  comparing against the one it froze when the claim appeared.

## Impact

- Affected specs: `tunnel-ownership-handover`
- Affected code: `internal/tunnelhandover`, `internal/tunnelclaim`,
  `internal/tunnelstart`, `internal/observe`, `internal/rootdaemon`,
  `internal/handovercli`; in `twilight`, the supervisor's carrier watchdog.
- Not affected: `abort`, which stays for a transaction in flight.

## Closing measurement

`release` run on the machine: Twilight's tunnel proven by two traversals, exactly
one tunnel sing-box afterwards, and no carrier rebuild recorded by Twilight that
the machine did not cause.
