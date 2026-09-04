# Record what the ingress fleet is for

## Why

Roadmap item 4 read "run an evidence-based provider-B bake-off and deploy an
independent VLESS/Reality ingress in a different provider and ASN." Grilling it
before writing any change found that it described work already done, and that
the reason the fleet exists at all is written down nowhere.

The provider-B ingress is deployed: a Lightsail instance with a static address,
managed by a reviewed private root, monitored externally, and consumed by
Twilight as its third outbound. Provider and ASN are genuinely distinct from the
DigitalOcean side. The half of item 4 that said "deploy" was finished before the
item was read.

The half that said "bake-off" cannot be run. The word appears exactly once in
this repository — in the roadmap line itself. No criterion, no threshold and no
second candidate provider exists anywhere in the public repository or the
private infrastructure, which declares only AWS, Cloudflare, DigitalOcean and
UptimeRobot.

What the grill did surface is a set of requirements that drive every placement
decision and are recorded in no specification:

- One host exists to serve the operator's home network, where circumvention is
  needed most.
- One host exists to present a United States address. Its latency is the price
  of that purpose, not a defect to be optimised away — a reading this repository
  currently invites, because the purpose is unwritten.
- The operator works periodically from a second jurisdiction with its own
  filtering, so an ingress inside that jurisdiction would be useless.
- Occasional access from a heavily filtered country is handled by buying a
  commercial service locally, not by this fleet.

Because none of that is written, the fleet's shape reads as arbitrary, and the
roadmap asked for work that was either finished or impossible.

Two claims this repository makes are also false as stated. The public
provider-B documentation reports the lifecycle state as `published`, hedged as
"no public repository fact proves" a workload is instantiated — accurate about
the public record, and untrue about the world, while the requirement it answers
says documentation MUST identify the current state. And the roadmap's item 4
describes work already delivered.

## What Changes

- Record the ingress fleet's purposes as a capability: what each host is for,
  and that a host's placement is answerable to its purpose rather than to
  latency alone.
- Correct the documented provider-B lifecycle state to what is true, keeping the
  distinction between what the public record proves and what is deployed.
- Rewrite roadmap item 4 to name the work that remains, and record what the
  grill ruled out: no bake-off is possible without a second candidate, and no
  provider can be selected on evidence for a country nobody can measure from.
- Record that no Hexroute-side evidence currently attributes ingress
  availability to a provider. The measurement that exists lives in Twilight,
  probes one target through the tunnel and two directly, and is therefore not
  admissible as provider comparison until that is fixed.

## Non-Goals

- Provisioning any host. The fleet's third machine belongs to Twilight
  production, which `policy/twilight-deny-inventory.json` states Hexroute
  migration plans must never manage.
- Changing selection, failover or any runtime behaviour.
- Advancing the provider-B lifecycle past what evidence supports. Correcting a
  documented state is not qualification, and inventory admission still requires
  its own reviewed evidence.
- Fixing the Twilight ingress monitor. Naming the defect is in scope; the code
  is not in this repository.

## Impact

Public documentation and the roadmap change. One capability is added that
records purpose. No binary, no daemon and no cloud component changes behaviour,
so there is nothing to roll out.

## Rollback

Revert the commit. Nothing is installed, activated or deployed by this change,
and no runtime reads any of it.

## Ownership boundary

This change belongs in public Hexroute. Twilight remains the production owner of
the ingress fleet and of every host in it; this repository records what the
fleet is for and what it may claim, and provisions nothing.
