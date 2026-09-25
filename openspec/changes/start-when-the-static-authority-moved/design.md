# Design

## What the crash loop cost

Both daemons refused to start, and the refusal took more than authority with it.
The root daemon's observation cycle stopped, so nothing was written to the
archive; the operator socket was gone, so `hexroutectl` could not ask either
domain anything; and the user daemon's own publication stopped with it. The one
act that resolves a static mismatch — installing the successor and activating it
— runs through those sockets.

So the refusal is not a safe failure. It is a failure that removes the
instruments and the remedy at once, and leaves a rollback as the only way out.

## The state already exists

Nothing here invents a vocabulary. `restart_required` is a policy state and
`static_mismatch` is a reason, and the pair is already produced when a candidate
is prepared against the wrong static authority. This change has a daemon reach
the same conclusion about its own store at startup.

What the status must carry is fixed by the status validation that is already
there: a bundle generation, a domain generation, a manifest digest, no
activation time. All four are in the store's lineage.

## Why the lineage, and not the active record

`RecoverActive` is the operational question — may this generation govern now —
and it is what fails. `RecoverLineage` is the historical one, and it says in its
own comment that it does not compare the predecessor's static digest, because an
expired generation and one compiled against an envelope that has since gained a
capability are both still the generation that came before.

That is exactly the reading a daemon needs here: it cannot run what the store
holds, and it must still say what the store holds. Taking the generation from
anywhere else would mean reporting a number nobody verified.

## What does not change

A candidate whose static digest differs from the installed one is refused as
before. The remedy is unchanged and is now reachable: install the configuration
the candidate needs, restart, install the candidate, activate it.

The other startup failures keep their answers. Corruption, an invalid signature,
a clock anomaly and an unreadable store are different questions, and the change
is written so that making one of them legible cannot make the rest so — each is
tested for the answer it had before.

## Authority is off without being switched off

A handler in this state has no active generation, and mutation authority already
requires one: `mutationAllowedLocked` refuses while nothing is active. So the
state authorizes nothing by construction rather than by a second switch that
could drift from the first.
