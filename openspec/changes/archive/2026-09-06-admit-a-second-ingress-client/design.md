# Design

## What the grill overturned

Roadmap item 6 asked for a Telegram proxy on two providers, using MTG and an
Nginx SNI split, with functional MTProto health evidence. The change before this
one recorded it as the reason signed configuration delivery had to come first.
The grill of item 6 found the item itself was wrong, and the reason is worth
writing down so nobody derives it again.

The proxy's justification looked like an availability objective. The SLO model
already computes Telegram availability and a sixty-second failover between
proxies, and the only alert contact configured on the external monitor is
Telegram, so a proxy looked like the thing keeping the alert channel alive. Put
to the operator, the purpose turned out to be simpler and different: using
Telegram from a phone without installing a VPN client.

Under that purpose the comparison inverts. An MTProto proxy carries one
application and requires a second runtime, a demultiplexer on the shared port,
a health probe that speaks a protocol nothing here speaks, and a producer for an
SLO that has never had one. A client already installed on the phone carries
everything, speaks the transport the fleet already runs, and costs the server
nothing at all. The proxy's one genuine advantage — that it needs no software on
the phone — disappears the moment any software is acceptable, and it was.

There is a second reason, and it is not about cost. MTProto with FakeTLS is
fingerprinted and blocked at scale where this fleet is used, and Reality exists
precisely to be indistinguishable from a real session to a real site. Putting
them on one address means an active probe that flags the weaker one has found
the stronger one too.

## Why the client record comes first

The fleet's hosts each have a recorded purpose, and the specification refuses to
judge a host's placement without one. Nothing equivalent exists for the other
end. That gap only became visible when a second client was proposed: there was
no answer to what the first one is, what it may reach, or what removing one
would mean.

So this change records clients before it admits one. It is the smaller half of
the work and the half that makes the larger half reviewable.

## Why the profile is derived from the published version

A client profile and the server configuration describe the same thing from two
sides, and they can disagree. The obvious source for the profile is the draft
the operator authored, and it is the wrong one: the draft may not have been
published, a different version may have been published after it, and a host that
failed to prove a version has gone back to the one before. In each case the
draft describes a server that is not answering.

The published version does not have that problem. It is the exact bytes the host
verified and applied, addressed by a digest, signed by the operator. A profile
derived from it cannot describe a server that will not answer.

## Why the emission stays opaque

Deriving the profile requires reading the transport's configuration — its
inbound structure, its client list, its parameters. This repository does not
know any of that, deliberately: the contract test on the ingress module fails on
the transport's own vocabulary appearing in a template, and `configversion`
handles content as bytes with no opinion about it. That is why the delivery
works for any runtime rather than for one.

So the boundary runs between verifying and parsing. This repository verifies a
version — signature against the pinned operator key, digest against the bytes —
and emits exactly what it verified. The private repository parses those bytes
and produces the profile. Both halves get the property they need and neither
learns the other's business.

The alternative was a shared library, and it is not available: the private
repository is shell and Terraform, and Go's internal packages do not cross a
module boundary anyway. Bytes were going to be the interface regardless; this
makes that a decision rather than an accident.

## Why nothing is published for a client to fetch

A subscription endpoint would be a place the phone must reach from a filtered
network in order to obtain the means of reaching things from a filtered network.
It also has to be hosted, secured and rotated, and a third-party client cannot
check an operator signature on what it fetches, so the "signed" in signed client
delivery would stop at the last hop regardless.

A profile carried over once by hand has none of those properties to defend.
Removal does not depend on it either: a client is removed by publishing a
configuration version without it, and that works whether or not an old profile
still exists somewhere.

## What this leaves owed

Three things surfaced during the grill that this change does not fix, recorded
so they are not lost:

The external monitor has exactly one alert contact, and it is Telegram. Every
check delivers through a single channel, and it is the channel whose reachability
was the subject of the grill.

The signed heartbeat's transport health is two TCP dials. A version is called
proven when two sockets accept a connection, which the provider-B document
itself calls insufficient — "a reachable socket is not qualification".

The Telegram SLO calculation has no producer and, after this change, describes a
service that will not be built. It is the fifth thing in this system found
designed and never given one.
