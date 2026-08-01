## Why

The bounded Lightsail module intentionally has no arbitrary bootstrap channel,
but provider B still needs reproducible XRay and observer service scaffolding.
Version- and digest-pinned public templates provide that baseline without
placing runtime transport credentials in Terraform or Git.

## What Changes

- Add cloud-init and hardened systemd templates for XRay and the ingress
  observer under the reusable Lightsail module.
- Add a structured, nullable runtime-artifact input containing only exact
  versions, HTTPS artifact URLs and SHA-256 digests.
- Render user data internally and expose only its SHA-256 digest, never an
  arbitrary caller-supplied user-data string.
- Install verified binaries and enable service units whose startup remains
  gated on root-provisioned runtime configuration files.
- Add the missing `hexroute-ingress-observer` Linux command and a
  deterministic release archive consumed by the existing bootstrap contract.
- Keep the observer on loopback and extend the heartbeat probe to retrieve its
  signed response through an explicitly supplied loopback SOCKS proxy.
- Add rendering, privilege-boundary, pinning and secret-redaction tests.
- Keep runtime secret creation/provisioning, live AWS instantiation, monitoring,
  qualification and client failover out of this change.

## Capabilities

### New Capabilities

- `ingress-runtime-bootstrap`: Secret-free, version-pinned bootstrap and systemd
  scaffolding for XRay and the bounded ingress observer.

### Modified Capabilities

- None.

## Impact

This change belongs to public Hexroute and affects the reusable
`lightsail-ingress` module, the observer and probe commands, deterministic
release tooling, synthetic fixtures, tests and public documentation.
Private `hexroute-infra` continues to own the live provider root, discovered
artifact coordinates and runtime secret provisioning. Rollout publishes an
unused module revision; rollback before private adoption reverts that revision.
Twilight, AdGuard, Pritunl and all current traffic remain unchanged.
