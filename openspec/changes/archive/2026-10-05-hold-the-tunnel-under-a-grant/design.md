# Design

## What is taken, and what is left

The runtime that holds the tunnel today does six things. It runs the tunnel
process and rebuilds it; it rescues the Pritunl service; it keeps an OTP watchdog
alive; it probes reserves and picks a failover ingress; it holds a fallback path
for Codex; and it holds the machine awake with `caffeinate -i -s`. This change
takes the first and leaves the other five where they are.

That is why the other runtime keeps running after the handover, and why this
design reads its behaviour closely without changing a line of it. Under a claim
it stands down from exactly the parts this one takes: measured in its own loop,
a claim sends it to `HANDED_OVER`, and while it is held it watches neither the
process, nor the carrier, nor the tick gap, while its Pritunl rescue and reserve
probes go on as before.

The specification says "whatever other runtime still holds a tunnel" rather than
naming that one. When the last of its five duties moves here and it is switched
off, the behaviour written down degenerates by itself: a runtime that gives up
the tunnel with nobody to give it to stops and says so.

## Acting where the decision is already made

The cycle already decides, and since the soak it decides early — the tunnel's own
observations first, the decision recorded before the probes, so that a machine
that sleeps mid-cycle still leaves the record. The executor acts immediately
after that record, in the same cycle, and every gate it passes is read in that
cycle: the claim, the grant, the rate bound, the machine's own power state.

This is what "authorized in the cycle it was answered in" means concretely. The
authorization is asked with the plan's digest and the control-state generation
this runtime reads for itself, and the act that follows is the one that was asked
about. Nothing is carried to the next cycle: a cycle that could not act decides
again from what it then observes.

## The route snapshot

A rebuild changes the tunnel's interface. Measured in the owning runtime's own
log, one ingress address has been carried by `utun4`, `utun5`, `utun6`, `utun7`,
`utun13`, `utun15` and `utun16` across restarts, so nothing may assume the name.

What must come back is what pointed at the old interface. On this machine that is
seven host routes; two more on the same interface are the tunnel's own subnet and
gateway, which the tunnel recreates. Thirty-seven host routes point at another
runtime's tunnel and three at the upstream VPN — none of them ours to touch.

So the executor reads the routing table before it stops anything, keeps the host
routes whose interface is the tunnel's, and creates exactly those on the new
interface. It does not consult its own route planner, which has proposed the same
two operations every cycle for days while traffic traversed; what those two are
is measured in this change and applied in none of it.

## The start goes through the binary an operator runs

Raising the tunnel means verifying a signed configuration version, and a gate in
this repository keeps the always-running daemons off the delivery path: neither
daemon, the operator command, the policy installer nor the sentinel may depend on
it. The point is that a configuration which arrives from elsewhere can reach a
binary someone runs and never a process that is always running.

The executor needs exactly that verification, so the daemon asks the handover
binary for it — `start-tunnel`, which refuses unless the claim is already this
runtime's, verifies the version the way `begin` does and reports the process it
started. Stopping needs none of that and stays in the daemon: it signals a
process it is already watching and waits for it to be gone.

The gate was not moved. It was the third option, and the cheapest; a boundary
that is relaxed the first time it is inconvenient is not one.

## Done means traffic

The rule's causes are about the tunnel's shape — a process, a gap, a carrier —
and none of them says the tunnel works. The runtime being reproduced waits for a
startup probe before calling itself healthy, measured at 17 seconds from stop to
probe on 2026-09-23, and this one waits for its own payload path the same way.

A rebuild that starts a process and carries nothing is a failure, recorded as
one. It costs the cycle up to half a minute, but only in a cycle that rebuilt,
and a cycle that rebuilt has already changed the machine more than waiting does.

## Why a suspended cycle does nothing

A machine dozing on battery wakes for seconds every quarter of an hour: 21 such
wakes in the night of 2026-09-20, and the rule as proved would have rebuilt
fourteen times against the owning runtime's six. A rebuild does not fit in one of
those wakes, and the tunnel it would rebuild is carrying nothing for anyone.

The cycle already records whether the machine was suspended, from its own power
observation — a ground added during the soak, for the judgement rather than for
the rule. The executor reads it. Nothing is lost: the gap keeps accruing while
the machine sleeps, so the first waking cycle names the wake and rebuilds then,
which is when a working tunnel is worth having.

## Giving the tunnel back

Three conditions end this runtime's ownership, and all three run the release
transaction that already exists: the rate bound reached, the grant lapsed, and a
tunnel that carries nothing while the outer path is reachable.

The first is the loop the bound exists for. The second is the whole meaning of a
grant with an expiry: a runtime that kept acting after it lapsed would make the
expiry decorative. The third is the failure this rule cannot repair — it does not
rebuild on the payload, because it cannot change ingress and would restart into
the same broken one, so it hands the tunnel to a runtime that can.

Taking the tunnel back is not symmetric. A handover interrupts traffic for as
long as the exchange takes, and doing that at a moment nobody chose is worse than
waiting for the operator. So `resume` clears what stopped the runtime and takes
nothing; the handover stays a command.

The bound's counters therefore live on disk. The case they exist for is a runtime
that rebuilds, dies, comes back and rebuilds again; counters in memory would be
cleared by exactly the fault they are meant to catch.

## The generation an authorization is judged against

The evaluator compares the caller's control-state generation with a copy of the
caller's control-state generation. It has done so on every action path, and the
only thing the check has ever required is a non-zero number.

The handler will read the generation itself, from the store that holds it, so the
state side of the comparison is the runtime's. A read that fails is a refusal:
the alternative is an authorization that proceeds precisely when it cannot
establish what it is authorizing against.

This is the one part of the change that makes existing paths stricter. A caller
that sent a stale generation was authorized before and will be refused now, which
is the point; the paths affected are Pritunl rescue, operator resume and the
tunnel question.

## The second handover

The first one taught what this one has to do differently. The other runtime's
supervisor restarts its tunnel inside its own process, so it kept running the
script it had loaded hours before a newer one was installed — measured again on
2026-09-25, eleven days on bytes that are no longer on disk.

So the handover restarts that supervisor as a process, and the preflight refuses
while the running one started before its script was written. The comparison is
made from outside it: when the process started, against when the script was
written. An install that preserved timestamps would fool it, and that is written
down rather than hidden.

A restart must also not change how it behaves. Its settings come from a file it
reads at start, not from its launch definition — the launch definition carries
only `HOME` and `PATH` — so a restart through its own launcher keeps them. The
preflight confirms the one that matters from what the restarted supervisor
announces: a health interval of sixty seconds, where the script's own default is
ten.

## What this change still does not prove

The rule is performed as proved, so what the soak left open is now performed too:
a carrier change shorter than a cycle passes unnoticed, about four times a day,
and a dozing machine is left alone by choice. The first could be closed by
reading the routing socket instead of sampling once a cycle, which this runtime
can do once it holds the tunnel; that is a change of the rule and would need its
own proof, and is not attempted here.
