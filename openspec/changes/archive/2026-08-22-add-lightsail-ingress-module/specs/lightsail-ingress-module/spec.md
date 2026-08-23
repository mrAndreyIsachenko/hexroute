## ADDED Requirements

### Requirement: Bounded Lightsail resource topology
The public module SHALL declare exactly one Lightsail instance, one static IPv4,
one static-IP attachment and one authoritative public-port policy, and SHALL
NOT declare a provider, backend or unrelated cloud resource.

#### Scenario: Synthetic module plan is generated
- **WHEN** a private-compatible fixture supplies valid instance metadata
- **THEN** the plan contains the four bounded Lightsail resources
- **AND** no account, region, DNS, IAM, secret, monitoring or client-routing resource is created

### Requirement: Provider-neutral live inputs
The module MUST require caller-supplied resource name, availability zone,
blueprint and bundle identifiers without embedding a live account identifier,
region, hostname, address or provider credential.

#### Scenario: Public repository is scanned
- **WHEN** public-boundary checks inspect the module and fixtures
- **THEN** no live identity, endpoint, credential, SNI or transport secret is present
- **AND** provider and backend configuration remain owned by the private root

### Requirement: Restrictive IPv4 firewall
The module SHALL expose global IPv4 TCP 443 and SHALL permit TCP 22 only from
explicit IPv4 `/32` networks. It MUST reject all other ports, port ranges,
protocols, IPv6 networks and public SSH.

#### Scenario: Default firewall is planned
- **WHEN** the caller does not request temporary operator access
- **THEN** the authoritative public-port policy contains only TCP 443 from `0.0.0.0/0`

#### Scenario: Bounded operator access is planned
- **WHEN** the caller adds TCP 22 from one or more valid IPv4 `/32` networks
- **THEN** the module accepts the rules without broadening transport TCP 443

#### Scenario: Broad ingress is requested
- **WHEN** a caller requests public SSH, IPv6, UDP, another port or a port range
- **THEN** Terraform variable validation fails before apply

### Requirement: Secret-free instance contract
The initial module SHALL NOT accept user-data, credentials, generic secret
payloads, VLESS/Reality material or SNI values and SHALL expose only non-secret
resource identities and computed network addresses.

#### Scenario: Module interface is reviewed
- **WHEN** inputs and outputs are inspected
- **THEN** no input can carry bootstrap or transport secret content
- **AND** output values cannot reveal a credential or transport secret

### Requirement: Production ownership remains external
The module MUST NOT claim production readiness, automatic failover or local
mutation authority, and its resources MUST remain removable by an independently
reviewed private rollback plan.

#### Scenario: Module is published before private adoption
- **WHEN** public code and tests pass
- **THEN** no AWS resource or local runtime changes
- **AND** Twilight and existing Hexroute paths remain the production baseline
