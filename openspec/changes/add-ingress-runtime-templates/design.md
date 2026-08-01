## Context

The public Lightsail module creates only infrastructure and deliberately rejects
arbitrary user data. The private provider-B design requires non-secret baseline
hardening and service scaffolding in cloud-init, while VLESS/Reality and
heartbeat signing secrets must arrive later through an operator-controlled
runtime channel. Template rendering therefore has to be structured, pinned and
testable without becoming a generic secret-bearing input.

## Goals / Non-Goals

**Goals:**

- render cloud-init internally from exact XRay and observer versions, HTTPS
  artifact URLs and SHA-256 digests;
- verify downloads before installation and record only non-secret versions;
- run XRay and the observer under one dedicated non-login service identity with
  distinct systemd sandbox and writable-path boundaries;
- gate service startup on root-provisioned runtime files that are absent from
  Terraform and Git;
- expose only a bootstrap digest for plan/evidence correlation.

**Non-Goals:**

- accept an arbitrary `user_data` string or any runtime credential;
- define XRay routing, Reality keys, VLESS identities, SNI, heartbeat keys or
  observer destinations;
- start services before runtime files exist;
- create or mutate live AWS, local routes, Twilight, AdGuard or Pritunl.

## Decisions

### Structured artifact contract

The module accepts a nullable object with one XRay and one observer artifact.
Each requires an exact semantic version, an HTTPS URL without query/user-info
and a lowercase SHA-256 digest. A nullable object preserves infrastructure-only
use; an arbitrary user-data input remains forbidden. Alternative generic cloud
init was rejected because Terraform state could silently become a secret store.

### Internal deterministic rendering

Checked-in `.tftpl` files render the installer, both units and cloud-init.
Terraform passes the rendered cloud-init directly to the Lightsail instance and
outputs only `sha256(user_data)`. Template files contain no environment-specific
addresses or secrets, so the same inputs produce the same bootstrap digest.

### Verify before install

The root cloud-init installer downloads to private temporary files with bounded
curl settings, validates SHA-256 before extraction, installs binaries atomically
and records exact versions. It enables units but does not start them. This keeps
bootstrap failure visible and prevents an unverified binary from replacing a
working path.

### Runtime secret and process ownership

Root later writes `/etc/hexroute/runtime/xray.json` and `observer.env` over the
private provisioning channel. Both services run as `hexroute-ingress`, not root.
XRay receives only `CAP_NET_BIND_SERVICE`; the observer receives no capability.
Systemd limits filesystem, home, namespace, device and address-family access,
and each unit restarts only its own process. Neither process can request local
Mac recovery or cloud control-plane mutation.

### Activation remains a private operation

Units use `ConditionPathExists` and remain inactive until private secret
provisioning validates runtime files and explicitly starts them. Cloud loss or a
failed unit leaves Twilight and local recovery untouched because the module has
no client, route or local process authority.

## Risks / Trade-offs

- [Artifact host is unavailable during replacement] -> Private policy pins and
  validates URLs before apply and retains rollback/recovery evidence.
- [A valid digest pins a vulnerable release] -> Version upgrades require a new
  reviewed module input and bootstrap hash.
- [User data is retained in Terraform state] -> It contains only public units,
  versions, URLs and digests; runtime secrets are structurally absent.
- [Environment-file secrets are readable by the service user] -> Root owns the
  directory, only the dedicated group can read required files, and systemd
  isolates the process from unrelated paths.
- [Enabled units fail at boot before provisioning] -> `ConditionPathExists`
  skips startup cleanly until runtime activation.

## Migration Plan

1. Publish templates and mock-render tests without changing live roots.
2. Pin the new public commit in private policy.
3. A later private saved plan supplies reviewed artifact coordinates and
   compares the rendered bootstrap digest before apply.
4. Before adoption, rollback reverts this public revision. After adoption,
   replacement or destroy remains controlled by private plan policy.

## Open Questions

The exact XRay and observer releases, artifact URLs and digests are live
deployment facts and remain intentionally unresolved until private task 5.2.
