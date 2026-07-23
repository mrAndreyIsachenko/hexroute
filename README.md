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

The repository is pre-cutover. Twilight remains the production owner.

## License

Apache License 2.0. See `LICENSE`.
