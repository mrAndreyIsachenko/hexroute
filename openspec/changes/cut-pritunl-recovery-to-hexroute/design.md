# Design

## Why this one is first

The roadmap had the root tunnel cutover before this. Three measurements moved
it: this path is built and held back rather than unfinished, its failure costs a
manual reconnect rather than the machine's only tunnel, and both are the same
transaction — boot out a legacy agent, hand ownership over, prove it, put the
old one back if it does not prove. Learning that where the cost is small is the
whole argument.

The order is not free of the other item, and the coupling argues for it. The
Pritunl service is a system daemon, so restarting it needs root the user daemon
does not have. This item therefore contains the first grant of production
authority no matter what — and it is one call against one named service, rather
than a supervisor with ten behaviours.

## What was already there

Almost all of it. The planner runs every cycle and already refuses to act for
reasons the legacy script also has — a closed lid, a dark wake, a settling wake,
an inactive session, an outer path that is not ready, a code window too short to
use — and for three the script does not have at all: backoff, an exhausted
recovery budget, and a verification window. The credential source reads the PIN
and the seed under scoped access that clears afterwards, and the type refuses to
print, log or marshal itself. The rescue request is typed and carries no
credential; the root handler checks the caller, checks the generation, then runs
its own probes before approving, and approves nothing else.

What is missing is small and specific: the root verifier has no implementation,
neither package is in a binary, and nothing performs a reconnect.

## The gate that has never been asked

`MutationAllowed()` is written and thoroughly tested — suspension, domain
mismatch, no active generation, a grandfathered non-compliant state — and it is
called only by its own tests. It is the sixth thing found here designed and
never given a producer.

That makes the authorization question answer itself. Building a flag, a build
tag or a settings file beside a tested gate would leave two mechanisms where one
is already correct, and the second would be the one nobody signed. So the
capability is a policy capability, the authority arrives as a signed generation
under user presence, and revoking it is the rollback that already exists.

## Why the gap and not the overlap

Both watchdogs run short loops — the legacy one every fifteen seconds — and a
one-time code is valid for thirty. Two of them on one profile is not redundancy;
it is one stopping what the other started, burning a code each round, with
neither able to see the other. A gap costs the opposite: reconnects happen a few
times a day, so the chance of needing one inside a hand-operated window is
negligible, and the next cycle picks it up regardless.

A bootout alone would not hold. For a user agent it does not survive a reboot,
so the overlap this avoids would reappear unattended, at night, in the middle of
the soak. The step is disable, and the rollback is enable.

## Why the two halves are proven differently

They fire at completely different rates, and the same evidence rule would be
wrong for one of them.

Reconnects are ordinary: 796 of them across 48 days, with only six days seeing
none. Waiting is a real sample, and inducing them would replace a natural
distribution with a chosen one.

Service rescues are not: 108 across the same period, of which 90 fell in two
days. That is one incident, not a rate. Waiting for the next one is not evidence
gathering, it is waiting for an outage while holding an untested grant of root
authority.

So the rescue is induced — and by creating the precondition rather than the
request. A request written by hand proves the handler and skips the detection,
and detection is the half this change moves.

## The detector that would have been lost

The planner marks inner state diagnostic-only, deliberately: what Pritunl says
about its own session is authoritative for reconnecting, and a path measurement
is affected by the outer tunnel and the routes as much as by Pritunl.

But the legacy script has a check the planner does not — a session that reports
itself connected, with a client address, carrying no traffic — and that is the
most recent real cause of a service restart. Moving ownership while dropping the
only detector for "everything looks healthy and nothing works" would make the
system worse and call it done.

The resolution keeps both properties by splitting on the decision rather than on
the signal. Inner health does not reconnect anything; it may cause a request to
restart a stale service, which root then revalidates before acting. Pritunl
stays the authority on its own session; the measurement stays the authority on
whether the path carries traffic.

## The secret

The legacy path concatenates the PIN and the code into a command-line argument,
where the process table exposes them to every local process for the life of the
call. The credential package was written against exactly this, with a test that
keeps secrets out of arguments and formatting.

The client supports reading the password instead of taking it as a flag, so the
value goes to the child's input and never becomes an argument. This is the one
place where the move is not ownership-neutral: the new path is safer than the
one it replaces, and that difference is the point rather than a side effect.

## What this leaves owed

The Keychain items keep their legacy names, and Hexroute will depend on them on
a critical path. Renaming means reading a seed out through a shell to change a
string, which is a real exposure for a cosmetic gain, and the cleanup item
exists for leftovers of this kind. It is named there rather than discovered.
