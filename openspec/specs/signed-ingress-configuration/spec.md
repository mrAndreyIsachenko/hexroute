# Signed Ingress Configuration Specification

## Purpose

Deliver a configuration to an ingress host so that the host can decide, on its
own, whether an operator meant it to run it — and so that a version which does
not work is left behind rather than lived with.

## Requirements

### Requirement: A configuration version is signed by a present operator

A configuration version SHALL be signed with the operator Ed25519 key held in
the user Keychain and SHALL require user presence. A daemon, a cloud component
and an automated build SHALL NOT possess that key or mint a version.

The signature SHALL cover the canonical version content and its digest
together, so that a signature cannot be moved to different bytes.

#### Scenario: An operator publishes a version

- **WHEN** an operator publishes a configuration version and user presence succeeds
- **THEN** the version carries a signature over its exact canonical content and digest
- **AND** the private key is not readable by any daemon or cloud component

#### Scenario: A component attempts to publish

- **WHEN** a daemon, worker or build attempts to produce a signed version
- **THEN** it is refused for want of the key rather than producing an unsigned or self-signed version

### Requirement: The node verifies before it applies

An ingress SHALL verify a configuration version before applying it: the
signature against a public key placed on the host when it was built, and the
digest against the bytes it received. It SHALL apply nothing that fails either
check, and SHALL keep serving its current configuration when it refuses.

Verification SHALL NOT depend on the transport that delivered the bytes. Reading
them from the expected place is not evidence of their authenticity.

#### Scenario: A version arrives intact

- **WHEN** the signature verifies against the placed public key and the digest matches the bytes
- **THEN** the ingress applies the version

#### Scenario: A version fails a check

- **WHEN** the signature, the signing key or the digest does not match
- **THEN** the ingress refuses it, keeps serving the configuration it already has, and records which check failed

#### Scenario: The delivery path is trusted instead of the content

- **WHEN** bytes arrive from the expected location with no valid signature
- **THEN** they are refused, and their location is not accepted as a substitute for authenticity

### Requirement: The cloud stores and records but does not instruct

The cloud SHALL hold configuration versions and their deployment history, and
SHALL NOT gain any interface through which it tells a node to change. An ingress
SHALL obtain a version by its own action.

The bytes SHALL live in private object storage and the version SHALL be recorded
in the configuration ledger, so that what exists and what is deployed are
answerable without reading the object.

#### Scenario: A version is published

- **WHEN** a signed version is published
- **THEN** its bytes are stored privately and a ledger row records its target, label, digest and signing key

#### Scenario: A node is offline while a version is published

- **WHEN** an ingress cannot reach the store
- **THEN** it continues on its current configuration and no instruction is queued against it
- **AND** it obtains the version on a later attempt of its own

### Requirement: A version is proven by the running generation, not by delivery

A configuration version SHALL become proven only when the ingress reports that
deployment generation healthy through its signed heartbeat. Successful delivery,
successful application and reachability SHALL NOT establish proof.

An ingress SHALL retain the version it was running and SHALL return to it when
the new version does not prove within its bounded window.

#### Scenario: The new generation reports healthy

- **WHEN** the signed heartbeat reports the new deployment generation healthy for its bounded window
- **THEN** the version is recorded proven

#### Scenario: The new generation does not report

- **WHEN** the window passes without a healthy heartbeat for that generation
- **THEN** the ingress returns to the retained version
- **AND** the version is recorded as not proven, with the reason it was not

#### Scenario: Delivery succeeded and the service did not

- **WHEN** the version applied cleanly but the heartbeat reports an unhealthy or older generation
- **THEN** it is not proven, and applying cleanly is not accepted as evidence that it works
