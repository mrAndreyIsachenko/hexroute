# Ingress Egress Identity Specification

## Purpose

Define how an authenticated ingress probe asserts that a bounded identity
endpoint observed the expected egress address, without exposing the response
body or the live address.

## Requirements

### Requirement: Authenticated canary can verify bounded response identity
The authenticated ingress probe SHALL accept an optional expected lowercase
SHA-256 digest and compare it with the exact response body received through the
temporary loopback SOCKS transport. The body used for comparison MUST be no
larger than 4096 bytes.

#### Scenario: Expected identity response matches
- **WHEN** authenticated transport returns an allowed status and the bounded response body matches the expected digest
- **THEN** the probe reports the existing passing result
- **AND** no response content or digest appears in output

#### Scenario: Expected identity response differs
- **WHEN** authenticated transport returns a body with a different digest or more than 4096 bytes
- **THEN** the probe reports an authenticated-transport failure
- **AND** the child process and private temporary configuration are removed

#### Scenario: No identity assertion is requested
- **WHEN** the optional digest is absent and the HTTPS status is allowed
- **THEN** the existing status-only authenticated canary behavior remains unchanged

### Requirement: Identity assertion remains secret-safe and observation-only
The response identity assertion MUST NOT place request values, response bytes or
calculated digests in process arguments, output or logs, and failure MUST NOT
mutate routes, DNS, TUN interfaces, provider resources or production services.

#### Scenario: Digest input is invalid
- **WHEN** the optional digest is not exactly 64 lowercase hexadecimal characters
- **THEN** input is rejected before `sing-box` starts
- **AND** output contains only the stable redacted failure contract

#### Scenario: Identity assertion fails
- **WHEN** the response identity does not match
- **THEN** the only effects are a structured failure result, child termination and temporary-file cleanup
- **AND** Twilight, AdGuard, Pritunl and current traffic remain unchanged
