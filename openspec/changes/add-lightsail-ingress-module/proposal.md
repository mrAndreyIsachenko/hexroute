## Why

Provider B needs a reusable AWS Lightsail building block before private
infrastructure can create and qualify an independent ingress. Defining the
contract publicly now keeps live account identity and deployment state private
while making the infrastructure behavior reviewable and testable.

## What Changes

- Add a provider-neutral Terraform module for one bounded Lightsail Linux
  ingress, its static IP attachment and an explicit ingress firewall policy.
- Validate names, blueprint, bundle, availability zone, tags and allowed ports
  without embedding account IDs, regions, addresses, credentials or secrets.
- Expose only non-secret resource identities and computed network outputs needed
  by a private root.
- Add synthetic Terraform tests and public-boundary checks for the module.
- Keep live instantiation, workload bootstrap, VLESS/Reality configuration,
  qualification, monitoring and failover out of this change.

## Capabilities

### New Capabilities

- `lightsail-ingress-module`: Provider-neutral contract for a single bounded
  AWS Lightsail ingress and its network attachment.

### Modified Capabilities

- None.

## Impact

This change belongs to the public Hexroute repository and affects only
`terraform/modules`, synthetic fixtures, tests and public module documentation.
The private `hexroute-infra` repository remains the sole owner of the live AWS
root, HCP workspace, provider configuration and deployment evidence. Twilight
remains the production transport owner; rollout is module publication only and
rollback is removal of the unused module before private adoption.
