# Design

## What the grill ruled out

Recording what a change decided against is worth as much as recording what it
does, because the next reader would otherwise re-derive it. Four things were
considered and rejected on evidence during the session that produced this
change.

**A provider bake-off cannot be run.** The word appears once in the repository,
in the roadmap line itself. No criterion or threshold is written anywhere, and
the private infrastructure declares four providers of which exactly one serves
ingress. A comparison needs two candidates; standing up a second purely to have
something to compare against would be paying for an answer nobody needs.

**Moving the United States host to Asia was proposed and withdrawn.** It was
recommended before its purpose was known, on the reasoning that an ingress far
from the operator is badly placed. The purpose turned out to be the address
itself. Measured from the operator's network, every Asian region is slower than
the current placement, so the move would have cost latency to serve a need that
was already met elsewhere.

**A permanent host for the heavily filtered country was proposed and
withdrawn.** Access from there is occasional and is met by buying a commercial
service locally. Holding infrastructure for it permanently would pay every month
for a need that arises rarely, and could not be verified: no vantage point
inside that country exists, so no placement decision for it can be evidence
based.

**A provider whose peering into that country is best was considered.** Its
operational risk is contractual rather than legal — such a provider may suspend
an instance found running a proxy — and it offers nothing for the two
geographies that matter routinely. It was set aside for the permanent fleet
rather than rejected outright.

## Why purpose is a capability rather than documentation

Placement decisions kept being re-litigated in this session because each one was
judged against latency, the only recorded number. A host presenting a United
States address measured worst of three and was twice called misplaced — once by
this repository's own roadmap, which asked for it to be replaced.

Recording purpose as a requirement rather than as prose means the next
comparison has something to be answerable to. It also makes a gap visible: a
purpose with no host is owed work, where today it is simply absent.

## Why comparable measurement is a requirement

The measurement that exists probes one target through the tunnel and two
directly. Read without that distinction it says one provider is less available
than another, and that reading survived several exchanges in this session before
the route field was examined.

The requirement is deliberately narrow. It does not demand a particular
instrument or vantage point; it demands that a comparison rest on comparable
paths, and that the absence of one be recorded rather than filled with the
measurement at hand.

## Boundary

Nothing here provisions, selects or mutates. The fleet's hosts belong to
Twilight production, which `policy/twilight-deny-inventory.json` places outside
what Hexroute plans may manage, with guards that forbid the protected project,
the resource types and runtime mutation from the infrastructure repository.

This change records what the fleet is for and what may be claimed about it. The
work of adding a host is Twilight's, and is not carried by this change or by the
roadmap item it corrects.
