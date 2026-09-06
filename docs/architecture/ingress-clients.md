# Ingress Clients

This document records who reaches an ingress, and what for. It records no
identity, no credential, no address and no transport parameter: those live in
the private infrastructure repository, and naming them here would publish who
can reach what to anyone reading a public repository.

It exists because the other end of every path was unwritten. Each ingress host
has a recorded purpose, and its placement may not be judged without one. The
clients had nothing equivalent, and that gap was invisible until a second client
was proposed and there was no answer to what the first one is, what it may
reach, or what removing one would mean.

## Clients

| Client | Purpose | Reaches |
| --- | --- | --- |
| **Operator workstation** | The machine the operator works from. It is the reason the fleet exists, and the only client any part of this system was built for. | The fleet, through the locally managed transport |
| **Operator handset** | A phone carrying the operator's usual applications from networks that filter them, without a full profile or a third-party service installed on it. | One ingress, through a client application configured by hand |

A client is a thing that holds an identity, not a person and not a device
model. Two devices belonging to one operator are two clients.

## Identity

Every client holds an identity of its own. None is shared.

This is not tidiness. Identity is the unit of removal: a client is removed by
publishing a configuration version that does not carry it. Two clients sharing
one identity cannot be separated, so losing the smaller one would force the
larger one to be rotated as well — the phone in a taxi would cost the
workstation its keys.

## Removal

A client is removed by publishing a configuration version without it, and by
nothing else. Removal does not depend on recovering, deleting or overwriting
anything already given to that client: a profile that still exists somewhere
stops working because the server no longer answers it.

Removal is therefore subject to the same rules as any other version. It takes
effect when a host has applied it, and if that version does not prove within its
window the host returns to the retained one — which still carries the client.
A removal that did not prove is a removal that did not happen, and it is visible
as such rather than assumed.

## Profiles

A client's profile is derived from the configuration version that was published
and verified, never from the draft it was authored from. The draft may not have
been published; a later version may have been published instead; a host that
failed to prove a version has gone back to the one before. In each of those
cases a profile from the draft describes a server that will not answer, and the
client's failure looks like a network problem rather than a stale file.

Nothing publishes a profile for a client to fetch. A subscription endpoint would
make obtaining access depend on already having access, from exactly the networks
this exists to serve, and a third-party client cannot check an operator's
signature on what it downloads in any case. Profiles are carried to their client
by the operator, once.

## What is not recorded here

No identity, no credential, no endpoint, no transport parameter and no count of
devices beyond the purposes above. What each client may reach is stated as a
purpose, not as a rule.
