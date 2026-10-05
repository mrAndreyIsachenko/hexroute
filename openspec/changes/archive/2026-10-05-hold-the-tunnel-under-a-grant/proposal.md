## Why

The decision rule was proved against the runtime that owns the tunnel and
performs nothing: seven days, no disagreement, three natural wake gaps, two
carrier agreements, two induced process losses. A rule nobody acts on keeps the
machine dependent on a runtime that is being retired, and every further claim
about this one — that it can hold a tunnel, that it gives it up when it should —
is untested. This change gives the rule an executor under a grant that can be
withdrawn, and takes the tunnel a second time.

This belongs in public Hexroute. The runtime being taken from is Twilight's, and
its behaviour is read here rather than changed: nothing in this change edits that
repository.

## What Changes

- The three soaked causes produce a rebuild that is performed: the tunnel process
  is stopped, started from the signed configuration version, and the host routes
  that pointed at the old tunnel interface are restored on the new one.
- A rebuild counts as done only when the payload path passes through it, within a
  bound. A start nothing traverses is a failed rebuild, and is recorded as one.
- A cycle that runs while the machine is suspended decides and records as before
  and performs nothing. The first waking cycle names the gap and acts then.
- A loop guard bounds rebuilds — two in five minutes, six in an hour, fifteen in a
  day — counted on disk, so a daemon that restarts into the same fault is still
  bounded.
- The runtime gives the tunnel back rather than sitting on a broken one. Three
  conditions do it: the guard tripping, the grant lapsing, and a tunnel that
  carries nothing for three consecutive cycles while the outer path is reachable.
  Each hands the tunnel to whatever other runtime still holds one, alerts, and
  leaves this one observing until `resume`.
- Taking the tunnel stays an operator act. `resume` clears the guard and nothing
  else; nothing in this runtime takes a tunnel on its own.
- **BREAKING** for the authorization path: an action authorization compares the
  caller's control-state generation with the one the runtime reads for itself,
  not with the caller's own. Every action path — Pritunl rescue, operator resume,
  the tunnel — is refused where it was silently accepted before (HEX-13).
- The handover refuses to begin unless the supervisor that holds the tunnel is
  younger than the script installed for it, so a supervisor running a script that
  no longer exists on disk is caught before the exchange rather than after
  (HEX-15).
- The ceremony grants root-only `tunnel_ownership` in a new generation, with the
  lease bounded by that generation, and the expiry warnings the ceremony needs.

## Capabilities

### New Capabilities

None. The executor is the tunnel supervision capability finally performing what
it has been deciding.

### Modified Capabilities

- `tunnel-supervision`: the decision is performed rather than only recorded; a
  rebuild's success is the payload passing through it; routes are snapshotted and
  restored around a rebuild; a suspended cycle performs nothing; a bounded rate
  and the conditions that end ownership.
- `tunnel-ownership-handover`: the second handover, its refusal when the running
  supervisor predates its installed script, and giving the tunnel back as an act
  of the runtime rather than only of the operator.
- `local-control-plane-foundation`: an action authorization compares the
  runtime's own control-state generation, and a runtime that cannot read it
  refuses rather than authorizes.

## Impact

- `internal/rootdaemon`: the cycle performs after recording, under the guard and
  the grant.
- `internal/tunnelplan`: unchanged in what it decides — this change does not
  touch the rule the soak proved.
- `internal/tunnelstart`, `internal/tunnelhandover`, `internal/tunnelclaim`: the
  second handover, and the release used as a runtime act.
- `internal/policycontrol`, `internal/policy`: the control-state generation the
  evaluator compares against.
- `internal/routeplan`: a snapshot and restore beside the planner, which
  continues to propose and is still not applied.
- `internal/alertdelivery`, `internal/notification`: the events that stop or
  return the tunnel.
- Operator documentation: the ceremony, the second handover, and what to do when
  the tunnel comes back.

## Non-goals

- The rule is not changed. What the soak proved is what is performed, including
  what it left unproved: a carrier change shorter than a cycle, and a dozing
  machine.
- The routing socket is not read. Sampling stays as soaked; the debt stays
  recorded.
- Routes are not continuously reconciled. The planner's standing proposal
  (HEX-11) is measured here and applied nowhere.
- Twilight's other five duties stay with Twilight: Pritunl rescue, the OTP
  watchdog, reserve probes and failover selection, the Codex fallback path, and
  holding the machine awake. This change takes the tunnel and nothing else.

## Rollout

1. Publish the grant in a ceremony: a new generation with root-only
   `tunnel_ownership`, verified against the installed policy before anything is
   performed.
2. Install the executor while the tunnel is still the other runtime's. Nothing is
   performed without a claim, so the installed executor is inert until the
   handover.
3. Restart the supervisor that holds the tunnel as a process, and confirm the
   refusal path works by running the preflight before and after.
4. Take the tunnel with the handover transaction.
5. Induce one rebuild per cause and read the outcome of each.
6. Hold the tunnel for seven days with no guard trip and no rebuild the payload
   did not pass.

## Rollback

`release` gives the tunnel back in one command, and is the same transaction the
runtime uses on its own when the guard trips. It does not depend on the executor:
it stops this runtime's tunnel, removes the claim, and the other runtime raises
its own within about seventy seconds, measured 2026-09-14. Withdrawing the grant
is the second lever and needs no code: a generation without `tunnel_ownership`
leaves the executor unable to act while the daemon keeps observing.

## Production ownership boundary

Twilight owns the tunnel until step 4 of the rollout and remains the lodger
afterwards, holding everything this change does not take. Live endpoints,
credentials and deployment evidence stay out of this repository; what is measured
from the machine enters as prose and counts.
