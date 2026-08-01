## Context

The private provider-B change has established an isolated AWS account and HCP
state but has not created compute. The public repository already publishes
reusable Terraform modules while private roots own provider configuration,
account checks, live values and evidence. This module must define the smallest
Lightsail network topology needed by the later private root without becoming a
deployment root or a secret-delivery mechanism.

## Goals / Non-Goals

**Goals:**

- model one Linux Lightsail instance, one static IPv4 attachment and one
  authoritative public-port policy;
- fail validation for broad protocols, unexpected ports, public SSH or malformed
  provider inputs;
- keep the module reusable across AWS accounts, regions and private roots;
- expose non-secret identities and computed addresses for private inventory;
- verify behavior with mock-provider plans and static public-boundary tests.

**Non-Goals:**

- define an AWS provider, backend, account ID, region, live hostname or address;
- accept cloud-init, credentials, VLESS/Reality values, SNI or generic secret
  payloads in this phase;
- create Secrets Manager, IAM, DNS, monitoring, client routing or failover;
- change Twilight, AdGuard, Pritunl or any active local path.

## Decisions

### Four-resource topology

The module owns `aws_lightsail_instance`, `aws_lightsail_static_ip`,
`aws_lightsail_static_ip_attachment` and
`aws_lightsail_instance_public_ports`. The public-ports resource is
authoritative: omitted ports are closed by AWS. EC2/VPC resources and implicit
default firewall behavior are rejected because they enlarge or obscure the
state boundary.

### Explicit bounded firewall input

The default and required transport rule is global IPv4 TCP 443. Callers may add
TCP 22 only for explicit IPv4 `/32` sources. Other protocols, ranges, IPv6,
additional ports and public SSH fail variable validation. Expiry and approval of
an operator `/32` remain private-root policy because they are live facts.

### No bootstrap payload in the initial contract

The instance module does not accept `user_data` yet. Non-secret cloud-init and
systemd templates are a separate parent-change task, while runtime secrets are
always provisioned out of band. This prevents an apparently generic input from
becoming an accidental Terraform-state secret channel.

### Caller-owned provider and live policy

The module declares only a compatible HashiCorp AWS provider constraint. The
private root selects and pins the exact provider/module revision, configures the
region, verifies the account and supplies discovered availability-zone,
blueprint and bundle identifiers. Module outputs are resource identities and
network addresses only; no credentials or secret references are produced.

### Replacement remains possible

The public module does not use `prevent_destroy`. Exact destroy/replacement
approval belongs to the private saved-plan policy, and reproducible replacement
is part of the provider-B recovery design. This keeps rollback executable while
preventing the reusable module from claiming production authority.

## Risks / Trade-offs

- [Caller supplies a valid but unsuitable blueprint or bundle] -> Private
  account/region/cost policy pins discovered live values before apply.
- [A static IP is briefly unattached during replacement] -> Private saved-plan
  policy and recovery rehearsal bound replacement order and identity.
- [Terraform state contains computed public/private addresses] -> These are
  non-secret live values and remain only in private remote state; no concrete
  address is committed publicly.
- [SSH `/32` can remain open] -> The later private root requires owner/expiry
  metadata and removes SSH before qualification.
- [One Lightsail instance is not highly available] -> The module makes no HA or
  failover claim; current production paths remain unchanged.

## Migration Plan

1. Publish the unused module with mock-provider and boundary tests.
2. Pin its commit from the private provider-B root and review an exact saved
   plan before creating any workload resource.
3. Roll back before adoption by reverting the public module commit; after
   adoption, rollback is an independently reviewed private AWS destroy plan.

Cloud loss or failed activation cannot remove local recovery because this
change has no local process, route, credential or mutation authority.

## Open Questions

None block the module contract. Exact availability zone, blueprint, bundle,
tags and temporary SSH `/32` are intentionally deferred to private policy.
