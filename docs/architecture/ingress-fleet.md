# Ingress Fleet Purpose

This document records what each ingress host is for. It records no address, no
provider identity and no region: those live in the private infrastructure
repository, and naming them here would publish the fleet's shape to anyone
reading a public repository.

Purpose is recorded because placement was repeatedly judged against latency, the
only number written down. A host that is slow on purpose is indistinguishable
from a host that is slow by mistake when its purpose is unrecorded, and this
repository's own roadmap once asked for such a host to be replaced.

## Purposes

| Purpose | Served | Note |
| --- | --- | --- |
| **Primary network** | Yes, by one host | The operator's usual location, where circumvention matters most. This host is the fastest measured and is the one a latency-based client selects by default. |
| **Named-country address** | Yes, by one host | Presents an address in a specific country because some destinations are reachable only from it. Its latency is the price of that purpose. It is measured slowest of the fleet and is correct where it is. |
| **Independent failure domain for the primary leg** | **No** | Two configured entries reach the primary host. A client selecting between them cannot tell, and an outage of that one machine removes both at once. This is owed work. |
| **Secondary working location** | Partially | The operator works periodically from a second jurisdiction that filters access. No host is placed to serve it specifically; the named-country host is the slowest option from there and the primary host the least bad. |
| **Occasional heavily filtered country** | Not by this fleet | Met by buying a commercial service locally. No host is held for it, because the need is rare and no vantage point exists there to verify any placement would help. A host inside such a jurisdiction would sit on the wrong side of the filter and is excluded by design. |

## Redundancy

Redundancy is counted in independent hosts, never in configured entries. The
fleet currently presents three entries and holds two hosts. Documentation,
review and any claim about failover use the host count.

## What is not claimed

No comparison between providers rests on the measurement that exists. It probes
one target through the tunnel and the others directly, so its results are not
comparable across providers, and the absence of a comparable measurement is
recorded rather than resolved by using the one at hand.

## Adoption

The fleet's hosts are provisioned outside Terraform and are named in the private
deny inventory, which forbids Hexroute migration plans from managing the
compute-instance and reserved-address resource types. That guard is scoped to the
migration period, not permanent: ownership transfers at the root tunnel cutover.

Adopting the hosts into managed infrastructure afterwards is not named by any
roadmap item. The mechanism exists — the provider-B root already adopts
pre-existing resources through Terraform `import` blocks — but each host
stood up by hand until then is one more to adopt later, and that cost is
recorded here rather than discovered at cleanup.
