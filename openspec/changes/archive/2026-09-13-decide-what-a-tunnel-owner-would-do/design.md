## Context

The cycle already takes every observation the decision needs:

| cause | what the cycle already has |
|---|---|
| the process exited | `processes.SingBox` |
| a wake gap | `Snapshot.LastTick` against the current tick |
| the carrier changed | `network.Route` per configured destination |
| connectivity returned | the endpoint probes |
| the payload path failed | nothing — this is the one thing built here |
| the routes drifted | `routeplan` already plans this |

Three of the six need memory of the previous cycle: the carrier's signature,
whether connectivity was present, and how many consecutive payload failures
have been seen. The snapshot carries `LastTick` and nothing else of this kind.

## Decisions

**The planner is pure.** It takes observations and the previous cycle's state
and returns a decision and the next state. No clock, no filesystem, no
processes. Everything about it is decidable in a test, which is what makes a
rule that will later hold a machine's network worth trusting.

**The carrier's signature is the destination-to-interface map.** That is what
the supervisor compares, expressed in what this runtime already observes, so
the two runtimes can disagree only about the facts and not about the question.

**A decision names every cause that held.** The comparison is against another
runtime's account of itself, and that runtime reports one reason. Recording
only the first cause would make an honest disagreement look like a wrong
decision.

**The previous cycle's state is durable and separate.** It is not policy, not
an observation and not part of the operator snapshot; folding it into any of
those would make an unrelated schema change whenever a cause is added. It gets
its own small file, written the way the other state files here are written.

**The payload probe proves the response, not the connection.** A probe that
completes a handshake proves a socket answered. The one built here fails when
the payload does not traverse — which is the sixth cause, and the evidence the
switch itself will be judged on.

**Nothing executes, and nothing can.** The safety allowlist already names
`restart sing_box`, and no policy grants it: `AuthorizePritunlRecovery` is the
only authorization this runtime has, and it is for a different target. The
inability to act is a property of the policy, not a flag in this change that
could be flipped by accident.

## Risks

Two of the six causes have not occurred in sixty-one days, so the soak cannot
confirm them by waiting. Both are inducible in seconds — kill the process, drop
and restore the link — and the grill settled that each is induced once rather
than left unproven. That is weaker than a natural occurrence and stronger than
a regression alone, and the record says which it was.

The payload probe is new code on a path that will later decide whether the
machine's network is rebuilt. While this change holds, a wrong probe costs a
wrong line in a comparison log and nothing else.
