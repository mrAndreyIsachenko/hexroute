# Hexroute

A reliability control plane for explicitly scoped VPN, proxy and access-continuity paths on macOS, with a telemetry-only cloud side.

The hard part is not routing. It is changing privileged network policy on a machine that can lose power, sleep, have its clock moved, or be attacked through its own filesystem — and afterwards still being able to prove which policy was active and who approved it.

## What is here

**Crash-consistent policy generations.** Every state write follows one sequence: write a private temporary file, `fsync` it, atomically rename to a typed final name, `fsync` the directory. Renames are no-replace, implemented per platform ([`rename_noreplace_darwin.go`](internal/policystore/rename_noreplace_darwin.go), [`rename_noreplace_linux.go`](internal/policystore/rename_noreplace_linux.go)). A crash on either side of any sync, rename or directory-sync boundary leaves either the complete old record or the complete new one. Byte-identical retries are idempotent; conflicting retries fail rather than overwrite.

**Stores that resist path attacks.** Root and user policy stores are privilege-separated and owned by the exact daemon UID/GID at mode `0700`. Every path component is opened directory-relative with `O_NOFOLLOW`; an open store binds all directories to their device and inode and rejects replacement underneath it. Artifacts are created with `O_EXCL`, sealed to `0400`, and accepted only as single-link regular files. The API never accepts a caller-supplied path, and `HOME` cannot redirect the user store.

**Action leases that cannot be replayed.** A privileged action is authorized by a lease bound to boot ID and monotonic clock alongside the policy generations, target and plan digest. A lease cannot be replayed across a reboot or a wall-clock change, and each failure mode is a distinct typed error — replay, expiry, stale generation, binding mismatch, clock anomaly. See [`internal/actionlease`](internal/actionlease).

**Approval chains that verify end to end.** Activation is a two-phase commit: a durable prepare receipt, then a commit intent embedding a user-presence signed approval, then an active pointer that may only advance to a higher generation. Digests are over canonical JSON, and the review artifact is retained specifically because its digest is bound into the signed approval — later verification rechecks the whole chain without trusting compiler output. See [`docs/architecture/policy-storage.md`](docs/architecture/policy-storage.md).

**Regression evidence as a first-class output.** 24k lines of Go tests against 32k lines of implementation, 456 test functions, plus shell-level contract tests for containers, migrations, launchd units, Terraform modules and policy CLI boundaries under [`tests/`](tests).

## Status

Hexroute is deliberately pre-cutover. Twilight remains the production owner of the live data path.

The root runtime runs in an isolated observe-only mode: it inspects macOS state and emits route proposals with **no mutation authority**. Cloud services are telemetry-only. Provider-B ingress infrastructure is published, not deployed and not failover-enabled.

This gating is a rule, not a backlog. From [`docs/roadmap.md`](docs/roadmap.md):

> Each numbered item requires its own grill session and bounded OpenSpec change. No future item may be bundled into an earlier cutover merely because supporting code already exists.

Ownership moves from Twilight to Hexroute one transactional cutover at a time, each with its own criteria. Existing code is not treated as permission to proceed.

## Repository Boundary

This public repository contains generic application code, schemas, tests and synthetic fixtures. Live hostnames, addresses, credentials, deployment roots, Terraform state and raw operational evidence belong outside Git or in the private `hexroute-infra` repository.

## Documentation

| Area | Entry point |
|---|---|
| Roadmap and ordered cutover sequence | [`docs/roadmap.md`](docs/roadmap.md) |
| Immutable policy storage | [`docs/architecture/policy-storage.md`](docs/architecture/policy-storage.md) |
| Atomic policy model and OpenShell attribution | [`docs/architecture/atomic-policy-generations.md`](docs/architecture/atomic-policy-generations.md) |
| Provider-B ingress topology and gating | [`docs/architecture/provider-b-ingress.md`](docs/architecture/provider-b-ingress.md) |
| Root observe-only mode | [`docs/macos/root-observe.md`](docs/macos/root-observe.md) |
| Operator surface and redacted diagnostics | [`docs/macos/operator.md`](docs/macos/operator.md) |
| Policy signing | [`docs/macos/policy-signing.md`](docs/macos/policy-signing.md) |
| Policy compile, activation and rollback operations | [`docs/macos/policy-operations.md`](docs/macos/policy-operations.md) |
| Cloud API, worker and migrator runtimes | [`docs/cloud/api-runtime.md`](docs/cloud/api-runtime.md), [`docs/cloud/worker-runtime.md`](docs/cloud/worker-runtime.md), [`docs/cloud/migration-runtime.md`](docs/cloud/migration-runtime.md) |
| Container contract | [`docs/cloud/containers.md`](docs/cloud/containers.md) |
| Terraform modules | [`terraform/README.md`](terraform/README.md) |

Planned behavior is specified through repository-local OpenSpec changes under [`openspec/`](openspec) before implementation.

## License

Apache License 2.0. See [`LICENSE`](LICENSE).
