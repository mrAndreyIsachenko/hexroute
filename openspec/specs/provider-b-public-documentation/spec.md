# Provider-B Public Documentation Specification

## Purpose

Define the single public architecture and operator view of the provider-B
building blocks, so available components are not mistaken for a production-ready
failover service.

## Requirements

### Requirement: Canonical provider-B architecture
The public repository SHALL provide one canonical architecture document that
connects the reusable infrastructure, bootstrap, runtime and functional-probe
components without including live provider identities, endpoints or secrets.

#### Scenario: Reader reviews provider B
- **WHEN** a reader follows the README or Terraform documentation
- **THEN** the canonical document explains topology and component ownership
- **AND** detailed module and probe contracts remain linked rather than duplicated

### Requirement: Explicit lifecycle and readiness boundary
The documentation MUST distinguish published, instantiated, provisioned,
qualified, inventory-admitted and failover-enabled states. It MUST identify the
current state without claiming production readiness, high availability or
automatic failover.

#### Scenario: Reusable components are published
- **WHEN** public modules and probes exist but no private workload consumes them
- **THEN** documentation reports the published state only
- **AND** Twilight remains the selected traffic and local recovery owner

#### Scenario: A later phase advances state
- **WHEN** private reviewed evidence satisfies a later lifecycle gate
- **THEN** only that gate can update the documented state
- **AND** failover remains disabled until a separate client-selection change

### Requirement: Independent signal and failure ownership
The operator view SHALL distinguish TCP reachability, public TLS fallback,
authenticated VLESS/Reality transport and signed instance heartbeat evidence.
Probe or provider failure MUST be documented as evidence-only with no cloud or
local mutation authority.

#### Scenario: Signals disagree
- **WHEN** one signal fails while another remains healthy
- **THEN** documentation directs qualification to preserve separate results
- **AND** no single signal authorizes restart, routing, DNS or failover mutation

### Requirement: Public documentation is regression checked
The repository SHALL statically verify canonical links, lifecycle gates,
ownership language and the absence of live account IDs, IPv4 endpoints and
credential-shaped values in the provider-B architecture document.

#### Scenario: Safety boundary is removed or private data appears
- **WHEN** a documentation edit drops required boundary language or introduces a private-shaped value
- **THEN** the repository documentation test fails before publication
