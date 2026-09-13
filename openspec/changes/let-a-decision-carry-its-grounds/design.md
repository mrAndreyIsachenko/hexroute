# Design

## One ground per cause, and the cycle's own honesty

Six causes can hold, and each is reached from something:

| cause | ground |
|---|---|
| `process_gone` | whether the process was running |
| `wake_gap` | how long the machine slept |
| `carrier_changed` | a digest of what carries the destinations, and how many it covers |
| `link_returned` | how many outer endpoints were configured and how many answered, what the rule believed, how many failures stood behind that belief |
| `payload_failed` | what the probe said and how many failures stood behind it |
| `routes_drifted` | how many route operations were planned |

Plus whether the cycle saw everything, because three of the six are comparisons
that an incomplete cycle does not make and a reader cannot otherwise tell a
cause that did not hold from one that was not asked.

## Decision: the planner returns the grounds

`Decide` already has every one of these in front of it. Returning them beside
the plan makes the record a projection of what the decision used rather than a
second gathering that could drift from it — the shape that lets a record say
something the decision did not.

## Decision: a digest and a count, never the destinations

The carrier signature is made of addresses. This repository does not let those
out: the relay mapper beside it records how many endpoints are configured and
how many answered and says in its own comment that which endpoint is which never
leaves it.

A digest keeps what the reader needs — whether two cycles saw the same carrier —
and drops what they do not. A count keeps the other thing that mattered live: a
signature of twenty entries where sixteen were expected was how a defect showed
itself once already.

The digest is truncated rather than whole. It is compared, never resolved, and a
short one cannot be walked back to a list of addresses by anyone who guesses the
input space.

## Decision: absent rather than zero for what was not observed

A cycle that stopped early has no carrier, no endpoint counts and no route plan.
Recording zeros would say it saw none where it saw nothing, and those are
different claims — the same distinction the timing records already keep.

## What this does not do

It does not align this vocabulary with Twilight's. Twilight records state
transitions with reasons of its own, and matching the two is the cutover's
problem. This change makes each record answerable on its own, which is the part
that has to be true before any comparison is worth making.
