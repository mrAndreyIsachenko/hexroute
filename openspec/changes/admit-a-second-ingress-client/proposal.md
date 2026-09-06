# Admit a second client to the ingress, and record who its clients are

## Why

The system has never recorded who its clients are. It records what each ingress
host is for and refuses to judge a host's placement without that, but the other
end of every path is unwritten: no document, spec or test names a client, and
the operator's phone was named as a consumer for the first time during the grill
that produced this change. A second client cannot be admitted safely to
something whose first client is unrecorded, because nothing says what admitting
one means, what it may reach, or how it is taken back.

## What Changes

- Record the client population the way the fleet's hosts are recorded: each
  client has an identity of its own and a stated purpose, and a client is
  removed by publishing a configuration version without it.
- Add an operator step that verifies a published configuration version and emits
  the exact bytes the host will run, so that a client profile can be derived
  from what will actually answer rather than from the draft it was built from.
- State that delivery does not interpret what it delivers. `configversion`
  treats content as opaque bytes today by construction; this makes it a
  requirement, so that the first code to look inside a runtime configuration has
  to argue for itself.
- **Removes nothing and grants nothing.** No new listener, runtime, port,
  endpoint or credential appears anywhere.

## Capabilities

**New Capabilities**

- `ingress-client-admission` — who may reach an ingress, how a client is
  admitted, and how it is taken back.

**Modified Capabilities**

- `signed-ingress-configuration` — delivery does not interpret what it
  delivers, and a published version's content is recoverable by an operator
  under the same verification a host performs.

## Impact

One offline subcommand on the operator's signing binary, one new public
document, and two spec files. No daemon, no cloud component, no database, no
Terraform module and no ingress host changes. The private repository gains the
profile generator; this repository never learns the transport's schema.

## Non-Goals

- **Adding MTProto.** Roadmap item 6 asked for a Telegram proxy on two
  providers. Its grill repositioned the purpose from an alert channel to using
  Telegram from a phone, and at that point an existing client on the phone gives
  more for less: all traffic rather than one application, over the strongest
  protocol on the fleet, with nothing new on the server. MTProto would put a
  protocol that is fingerprinted and blocked at scale beside Reality on one
  address, where a probe that flags the first reaches the second.
- **Nginx SNI pass-through.** Sharing 443 needed a demultiplexer, and a
  distribution package would put an unpinned listener on the public port, which
  contradicts the specification that defines that host.
- **A client application of our own.** It costs a paid developer account, a
  network-extension entitlement and a distribution channel, and produces a
  re-skinned build of a client that already exists and already speaks the
  transport.
- **Publishing a profile to be fetched.** A subscription would make obtaining
  access depend on having access, from the network it exists to serve.
- **Deriving the profile here.** That would require the transport's schema in a
  repository whose boundary excludes it.
- **A second provider.** It costs a host to adopt after the cutover, for a
  purpose no measurement has established.

## Rollout

There is nothing to roll out. The subcommand runs on the operator's machine; the
documents are records. A client is admitted by the configuration-version path
that already exists, and is admitted only when a version carrying it proves.

## Rollback

Revert the commit. The subcommand disappears, the records disappear, and no host
or client is affected, because neither ever depended on them: a client already
admitted keeps working, and removing one is a configuration version either way.

## Ownership boundary

Public Hexroute owns the client record, the verification and the emission of
verified bytes. The private infrastructure repository owns the transport
parameters, the profile generator and the identity of every client. Twilight is
untouched and is not a consumer.
