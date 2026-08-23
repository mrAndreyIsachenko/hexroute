## Why

The reusable Lightsail module, runtime bootstrap and functional probes are
implemented, but their topology and lifecycle boundaries are scattered across
three implementation changes. A single public architecture and operator view
is needed before private infrastructure consumes them, so readers cannot
mistake available building blocks for a production-ready failover service.

## What Changes

- Add one provider-B architecture document covering component topology,
  privilege/secret ownership, independent health signals and lifecycle gates.
- Document the operator distinction between publishing, instantiating,
  provisioning, qualifying, inventory admission and future failover enablement.
- Link the architecture from the README, Terraform documentation, probe guide
  and roadmap.
- Update current status to reflect implemented reusable components while
  explicitly retaining Twilight ownership and disabled client failover.
- Add static documentation checks for required safety language and absence of
  live provider identifiers, endpoints or credentials.
- Keep Terraform roots, live deployment, secret provisioning, monitoring and
  client selection out of this documentation-only change.

## Capabilities

### New Capabilities

- `provider-b-public-documentation`: Public architecture and operator contract
  for reusable provider-B ingress components and their gated lifecycle.

### Modified Capabilities

- None.

## Impact

This public Hexroute change affects documentation, static documentation tests
and repository-local OpenSpec artifacts only. Private `hexroute-infra` remains
the owner of live AWS identity, Terraform roots, values and evidence; Twilight
remains the current traffic owner. Rollout publishes corrected documentation.
Rollback reverts only the documentation commit and cannot alter any runtime,
route, provider resource or monitoring state.
