# Hexroute

Hexroute is a reliability control plane for explicitly scoped VPN, proxy and
access-continuity paths. It is being developed beside the existing Twilight
runtime and does not own production routes or processes until guarded cutover
criteria are satisfied.

The initial implementation focuses on deterministic local recovery, strict
privilege separation, offline-safe operation and regression evidence. Cloud
services remain telemetry-only.

## Repository Boundary

This public repository contains generic application code, schemas, tests and
synthetic fixtures. Live hostnames, addresses, credentials, deployment roots,
Terraform state and raw operational evidence belong outside Git or in the
private `hexroute-infra` repository.

## Current Status

The repository is pre-cutover. Twilight remains the production owner. The
root runtime currently supports an isolated observe-only mode that can inspect
macOS state and emit route proposals without mutation authority. See
`docs/macos/root-observe.md`. A local typed operator surface provides bounded
status, redacted diagnostics and generation-guarded safe-mode resume without
exposing arbitrary commands; see `docs/macos/operator.md`.
Actionable user incidents use fixed, redacted macOS notification templates;
see `docs/macos/notifications.md`.
The cloud API and worker share a pinned, non-root `scratch` image contract with
a read-only root filesystem; see `docs/cloud/containers.md`.
The signed-ingest, readiness and passkey/dashboard HTTP surface is assembled by
the explicit `hexroute-ingest api` mode; see `docs/cloud/api-runtime.md`.

## License

Apache License 2.0. See `LICENSE`.
