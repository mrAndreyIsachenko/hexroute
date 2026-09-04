# Provider-B Ingress Architecture

## Current State

Hexroute has published reusable provider-B components. A provider-B workload is
**provisioned**: it exists, it carries traffic for a private workload, and its
provider and ASN are distinct from the other ingress. It is not qualified, not
inventory-admitted and not failover-enabled by Hexroute.

No public repository fact proves any of that, and none is added here. The
evidence lives in the private infrastructure repository, as does every address,
provider identity and region. This document previously reported the state as
`published` on the strength of that silence — accurate about the public record,
untrue about the world, and the wrong answer to a requirement that asks for the
current state rather than the last state the public record can prove.

Twilight remains the selected production traffic owner and the owner of local
sing-box, scoped routes and Pritunl recovery. Provider-B code and probes have no
authority to mutate Twilight, AdGuard, DNS, routes or client selection.

## Topology

The public capability has three layers:

1. `terraform/modules/lightsail-ingress` defines one bounded AWS Lightsail
   instance, one static address attachment and explicit public-port policy.
2. Structured bootstrap renders verified XRay and observer installation plus
   hardened systemd units. Runtime files are deliberately absent from
   Terraform and service startup is gated on their later presence.
3. `hexroute-ingress-probe` produces separate TCP, public TLS fallback,
   authenticated VLESS/Reality and signed heartbeat results.

The private infrastructure repository composes those layers. It owns the live
Terraform root, provider identity, region and image selection, artifact pins,
secret references, operator access, external monitor, qualification evidence
and rollback plans. None of those deployment values belongs in this public
document.

At runtime, XRay and the observer are separate restricted processes. XRay owns
the public transport listener and reads only its root-provisioned runtime
configuration. The observer has no provider mutation credential; it reports a
fresh generation-bound signed heartbeat from a loopback-only HTTP listener.
Private operator automation retrieves authenticated-probe material over stdin,
starts a temporary loopback-only sing-box SOCKS process and reads the observer
through that authenticated transport. External monitoring remains outside the
AWS failure domain and tests public reachability without receiving AWS
credentials.

## Ownership

| Surface | Owner | Authority |
| --- | --- | --- |
| Reusable module, bootstrap and probe schemas | Public Hexroute | Define and test provider-neutral contracts |
| Live AWS root, account/region binding and saved plans | Private infrastructure | Create or destroy only reviewed provider-B resources |
| Transport and heartbeat secret values | Provider secret service plus operator recovery store | Supply runtime values outside Terraform and Git |
| XRay process and transport listener | Restricted ingress service identity | Serve authenticated transport only |
| Observer process | Restricted ingress service identity | Emit signed health evidence only |
| External reachability monitor | Provider-neutral edge state | Observe public TCP/TLS fallback and notify |
| Authenticated Mac canary | Private user-side automation | Create temporary loopback SOCKS and report evidence |
| Current routes, Pritunl recovery and selected ingress | Twilight | Continue production behavior until a separate transactional cutover |

Cloud loss or a failed provider-B activation can remove evidence, but it cannot
request local mutation. Existing local recovery remains available because the
provider-B module, runtime and probes have no local daemon IPC or route/DNS
authority.

## Lifecycle Gates

Provider-B state advances only in this order:

| State | Required evidence | Traffic effect |
| --- | --- | --- |
| **Published** | Public modules, bootstrap, probes and tests are available | None; passed |
| **Instantiated** | Exact private saved plan creates only the reviewed provider-B foundation and the next plan is no-op | None; passed |
| **Provisioned** | Pinned runtime is installed, secrets arrive out of band, services validate and temporary operator access is removed | None; **this is the current state** |
| **Qualified** | Instance status, external reachability, authenticated transport, signed heartbeat and replacement recovery pass one generation-bound soak | None |
| **Inventory-admitted** | Independent provider/ASN identity, cost and qualification evidence are reviewed and committed | Still none; selection remains disabled |
| **Failover-enabled** | A separate signed client-delivery and transactional selection change passes its own rollout and rollback gates | Outside the current provider-B change |

A private workload consuming this ingress does not advance its state either: it
is traffic, not evidence, and Hexroute neither selected it nor measured it.

Publishing code is not deployment. A reachable socket is not qualification.
Inventory admission is not client activation. No state can be skipped or
inferred from a later-looking but incomplete signal.

## Independent Signals

Qualification preserves four functional results rather than collapsing them
into one health flag:

- **TCP reachability** proves only that a listener accepted a connection.
- **Public TLS fallback** proves normal certificate-verified fallback
  negotiation, not authenticated Reality transport.
- **Authenticated transport** proves a bounded HTTPS canary traversed the
  expected VLESS/Reality path through temporary loopback SOCKS.
- **Signed heartbeat** proves the expected node/key emitted a fresh healthy
  deployment generation, not that an outside client can reach it.

Private qualification also correlates provider instance status, static-address
attachment and external monitor continuity. A missing, stale, mismatched or
failed signal keeps provider B unqualified. No probe result can restart XRay,
change a route, alter DNS or enable failover.

## Failure And Rollback

Before inventory admission, rollback removes pending private consumers and then
destroys only allowlisted provider-B resources through a reviewed saved plan.
After admission but before client activation, inventory and monitoring
references are removed first, followed by the reviewed provider destroy plan.

Because the current state has no client selection authority, both rollback paths
leave DigitalOcean, Twilight, AdGuard, Pritunl and current traffic unchanged.
A later failover-enabled state requires its own local rollback and is not
defined by this capability.

## Operator Rules

- Never place live endpoint, SNI, VLESS/Reality material or heartbeat private
  keys in Git, Terraform inputs, process arguments or logs.
- Never infer authenticated transport from TCP or fallback TLS alone.
- Never begin qualification while temporary operator access remains open.
- Never admit an ingress without independent provider/ASN and replacement
  recovery evidence.
- Never represent provider B as production-ready or highly available while the
  lifecycle remains before failover-enabled.

Detailed reusable contracts:

- [Terraform module and bootstrap](../../terraform/README.md)
- [Functional probes](../cloud/ingress-functional-probes.md)
- [Product sequencing](../roadmap.md)
