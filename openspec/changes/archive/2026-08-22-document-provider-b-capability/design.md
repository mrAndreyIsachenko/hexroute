## Context

Public Hexroute now contains three provider-B building blocks: a bounded
Lightsail module, secret-free runtime bootstrap and observation-only functional
probes. Their implementation documents are accurate but change-scoped, while a
new operator or reviewer needs one stable view of how the pieces compose, who
owns live values and which gates separate code availability from traffic use.

## Goals / Non-Goals

**Goals:**

- provide one canonical public topology and ownership document;
- make lifecycle states and promotion gates explicit and ordered;
- distinguish external fallback, authenticated transport and signed instance
  evidence;
- link reusable module, runtime and probe references from obvious entrypoints;
- enforce language and public/private boundaries with a static test.

**Non-Goals:**

- document live AWS account, endpoint, SNI, secret reference or evidence;
- add deployment steps that bypass private saved-plan and approval policy;
- claim high availability, production readiness or automatic client failover;
- change Terraform, Go code, runtime state, monitoring or current traffic.

## Decisions

### One canonical architecture document

`docs/architecture/provider-b-ingress.md` is the stable public entrypoint. It
describes component edges, ownership, signal independence, lifecycle state and
rollback, then links to the detailed Terraform and probe contracts. Duplicating
full deployment instructions in README or every component guide was rejected
because those copies would drift and could expose private operational facts.

### Lifecycle is a monotonic gated sequence

The document names six states: published, instantiated, provisioned, qualified,
inventory-admitted and failover-enabled. Public code currently proves only the
first. Every later state belongs to a private reviewed change, and failover is
explicitly outside the current provider-B deployment. This prevents a module or
green probe result from being represented as production availability.

### Ownership is described by boundary, not live identity

Public Hexroute owns reusable code and schemas; private infrastructure owns
provider identity, roots, secret references, monitors and evidence; the ingress
host owns restricted XRay/observer processes; Twilight owns selected client
traffic and local recovery. Public examples use roles and logical labels only.

### Documentation requirements are executable

A shell test checks canonical links, the full lifecycle sequence, all four
independent signals, explicit no-failover/current-owner language and absence of
account IDs, IPv4 literals and credential-shaped data in the architecture
document. Manual review alone was rejected because future edits could silently
erase the safety boundary.

## Risks / Trade-offs

- [Documentation becomes stale after later phases] -> lifecycle state remains
  explicit and roadmap status is updated only when private evidence exists.
- [Static checks overfit prose] -> test stable headings and key contracts rather
  than paragraph formatting.
- [Public topology helps scanners] -> omit all live hostnames, addresses, SNI,
  account IDs and secret references.
- [Cloud loss is mistaken for local outage authority] -> document probes as
  evidence-only and retain Twilight/local recovery ownership.

## Migration Plan

1. Publish the canonical document, links and static test with no runtime change.
2. Pin the public documentation revision in private progress and close parent
   task 4.5.
3. Later private phases update status only after their own plan/evidence gates.
4. Rollback reverts the documentation commit; no infrastructure action exists.

## Open Questions

None. Live deployment values and qualification thresholds remain deliberately
private and unresolved in this change.
