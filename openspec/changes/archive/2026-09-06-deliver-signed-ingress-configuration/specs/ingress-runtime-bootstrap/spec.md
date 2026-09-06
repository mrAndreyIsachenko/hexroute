## MODIFIED Requirements

### Requirement: Loopback-only signed heartbeat
The observer SHALL bind only a configured literal loopback address, load an
existing mode-private Hexroute Ed25519 key, and return a bounded signed
heartbeat containing exact node, generation, observation time and transport
health fields. It MUST NOT expose secret material or dependency error text.

The generation SHALL be the configuration version the host has applied, when
the host is receiving versions. A host that has applied one and reports the
generation it was built with would prove a version it is not running, so an
observer that cannot read the applied version SHALL return no heartbeat rather
than the one it was built with.

#### Scenario: Runtime is healthy
- **WHEN** the configured local XRay listener and outbound dependency pass bounded probes
- **THEN** the observer returns a fresh signed healthy heartbeat

#### Scenario: Runtime or configuration is unhealthy
- **WHEN** a dependency fails or configuration, key ownership or key permissions are invalid
- **THEN** the observer fails closed or returns a signed unhealthy heartbeat
- **AND** no runtime value or raw dependency error is emitted

#### Scenario: The host has applied a configuration version
- **WHEN** the host records an applied configuration version and the observer is configured to read it
- **THEN** the heartbeat reports that version as its generation

#### Scenario: The applied version cannot be read
- **WHEN** the recorded version is absent or is not a generation reference
- **THEN** the observer returns no heartbeat, rather than the generation it was built with

#### Scenario: The host receives no versions
- **WHEN** no applied-version record is configured
- **THEN** the heartbeat reports the generation the host was built with
