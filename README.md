# Hexroute

Hexroute is a reliability control plane for explicitly scoped VPN, proxy and
access-continuity paths. It is being developed beside the existing Twilight
runtime and does not own production routes or processes until guarded cutover
criteria are satisfied.

The initial implementation focuses on deterministic local recovery, strict
privilege separation, offline-safe operation and regression evidence. Cloud
services remain telemetry-only.

Current capabilities and the ordered migration sequence are tracked in
`docs/roadmap.md`. Planned behavior is specified through repository-local
OpenSpec changes rather than the archived cross-repository umbrella proposal.

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
The cloud API, worker and pre-deploy migrator share a pinned, non-root
`scratch` image contract with
a read-only root filesystem; see `docs/cloud/containers.md`.
The signed-ingest, readiness and passkey/dashboard HTTP surface is assembled by
the explicit `hexroute-ingest api` mode; see `docs/cloud/api-runtime.md`.
The explicit `hexroute-ingest worker` mode runs bounded heartbeat,
sleep/incident reconciliation, transactional alert delivery and retention
jobs; see `docs/cloud/worker-runtime.md`.
The explicit `hexroute-ingest migrate` mode applies checksum-verified schema
changes and idempotently seeds the first dashboard principal before rollout;
see `docs/cloud/migration-runtime.md`.
Reusable Terraform modules define the App Platform, managed PostgreSQL,
private Spaces, DNS, uptime-check and provider-neutral ingress contracts
without embedding live infrastructure; see `terraform/README.md`. The
provider-B topology, ownership and gated lifecycle are documented in
`docs/architecture/provider-b-ingress.md`; its current public state is
published, not deployed or failover-enabled.

## License

Apache License 2.0. See `LICENSE`.
