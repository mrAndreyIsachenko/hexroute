## ADDED Requirements

### Requirement: Bounded provider-neutral result contract
The probe CLI SHALL read one strict size-bounded JSON request from standard
input and emit one versioned result containing only the probe kind, pass/fail
state, stable failure category and elapsed milliseconds. It MUST NOT emit an
endpoint, SNI, VLESS identity, Reality material, heartbeat key or target URL.

#### Scenario: Probe succeeds
- **WHEN** a supported probe completes within its deadline
- **THEN** the CLI exits zero with a passing versioned JSON result
- **AND** no request value is copied into output or process arguments

#### Scenario: Input or probe fails
- **WHEN** input is invalid or a probe fails or times out
- **THEN** the CLI exits non-zero with a failing stable category
- **AND** standard error remains generic and secret-free

### Requirement: Distinct TCP and TLS fallback evidence
The system SHALL probe TCP reachability independently from standards-compliant
public TLS negotiation with certificate and server-name verification. Every
network operation MUST have an explicit bounded timeout.

#### Scenario: Listener is reachable but TLS is invalid
- **WHEN** TCP connection succeeds but the fallback presents an invalid TLS identity
- **THEN** TCP reports pass and TLS fallback reports a TLS-category failure

#### Scenario: Endpoint is unavailable
- **WHEN** the connection cannot complete before its deadline
- **THEN** the corresponding probe reports timeout or reachability failure without retrying indefinitely

### Requirement: Authenticated transport uses an isolated proven engine
The authenticated probe SHALL delegate VLESS/Reality to an installed
`sing-box`, expose only a temporary loopback SOCKS listener and fetch one
bounded HTTPS canary through it. It MUST NOT create a TUN, alter routes or DNS,
or place transport material in arguments, logs or persistent files.

#### Scenario: Authenticated canary succeeds
- **WHEN** sing-box establishes the configured VLESS/Reality transport and the HTTPS canary returns an allowed status
- **THEN** the authenticated probe reports pass
- **AND** the child process and temporary configuration are removed

#### Scenario: Engine, transport or canary fails
- **WHEN** sing-box is absent, cannot become ready, loses transport or receives a disallowed canary response
- **THEN** the probe reports dependency or authenticated-transport failure within its deadline
- **AND** cleanup still terminates the child and removes the private temporary directory

### Requirement: Signed heartbeat proves expected runtime generation
The heartbeat probe SHALL accept only a bounded HTTPS response containing a
strict heartbeat body and existing Hexroute signed envelope. It MUST validate
the expected active Ed25519 node/key, signature, envelope and body freshness,
exact deployment generation and healthy runtime state.

#### Scenario: Expected instance is fresh and healthy
- **WHEN** the response is authentic, fresh, generation-matched and runtime-healthy
- **THEN** the heartbeat probe reports pass

#### Scenario: Heartbeat trust or state is invalid
- **WHEN** the signature, node/key, timestamp, generation or runtime state is invalid
- **THEN** the probe reports the corresponding authenticity, freshness, generation or health category
- **AND** it does not accept TLS reachability as a substitute for signed identity

### Requirement: Existing production ownership remains unchanged
Publishing or invoking these probes SHALL be observation-only and MUST NOT
mutate cloud resources, local daemons, routes, DNS, Twilight, AdGuard or
Pritunl. Probe failure MUST NOT trigger an XRay or local-service restart.

#### Scenario: Any probe fails
- **WHEN** a probe returns any failure category
- **THEN** the only effect is a structured result and process cleanup
- **AND** current production and recovery paths remain unchanged
