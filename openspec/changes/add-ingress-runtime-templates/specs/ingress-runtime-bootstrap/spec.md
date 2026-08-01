## ADDED Requirements

### Requirement: Exact runtime artifact pins
The module SHALL accept runtime bootstrap only as exact XRay and observer
versions, HTTPS artifact URLs without query credentials, and lowercase SHA-256
digests. It MUST NOT accept floating versions or arbitrary user data.

#### Scenario: Valid artifacts are supplied
- **WHEN** both artifacts have exact versions, bounded HTTPS URLs and SHA-256 digests
- **THEN** Terraform renders deterministic non-secret cloud-init
- **AND** the plan exposes only its digest as bootstrap evidence

#### Scenario: Artifact pin is incomplete or unsafe
- **WHEN** a version floats, a URL is not bounded HTTPS, or a digest is malformed
- **THEN** variable validation fails before apply

### Requirement: Verified bootstrap installation
Cloud-init SHALL download each artifact to a private temporary path, verify its
SHA-256 digest before extraction, install only the verified executable and
record exact non-secret versions.

#### Scenario: Artifact digest does not match
- **WHEN** downloaded content fails SHA-256 verification
- **THEN** bootstrap exits non-zero before installing or starting that artifact

### Requirement: Hardened service ownership
XRay and the observer SHALL run as a dedicated non-login service identity under
separate systemd units. XRay MUST receive only the bind-service capability and
the observer MUST receive no Linux capability.

#### Scenario: Unit contracts are inspected
- **WHEN** systemd templates are rendered
- **THEN** both units use non-root identity and restart boundaries
- **AND** filesystem, privilege, device and address-family restrictions are explicit

### Requirement: Runtime-only secrets
Transport and heartbeat secrets MUST be absent from Terraform inputs, rendered
cloud-init, Git, tests and outputs. Service startup SHALL depend on root-owned
runtime files provisioned after infrastructure apply.

#### Scenario: Instance boots before runtime provisioning
- **WHEN** runtime configuration files do not exist
- **THEN** systemd skips XRay and observer startup without exposing a fallback secret

#### Scenario: Repository content is scanned
- **WHEN** secret and template contract tests inspect the worktree
- **THEN** no VLESS identity, Reality private key, SNI, signing secret or credential is present

### Requirement: Existing production paths coexist unchanged
Publishing or rendering the bootstrap MUST NOT mutate current cloud resources,
local networking, Twilight, AdGuard or Pritunl and MUST NOT claim production
readiness or automatic failover.

#### Scenario: Bootstrap revision is published
- **WHEN** public checks and mock plans pass
- **THEN** no live instance or local process changes
- **AND** current production and recovery ownership remains unchanged
