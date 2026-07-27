## Why

The production Team migration cannot safely switch PostgreSQL-backed API and
worker instances while the source remains writable: writes accepted after the
final copy can be lost or diverge. Hexroute needs an application-level,
transactionally enforced write freeze before the private infrastructure
repository may execute the DNS cutover.

## What Changes

- Add a singleton PostgreSQL cutover-control record owned by the migrator and
  readable by runtime roles.
- Require every application write transaction to acquire the shared cutover
  lock and reject writes while the source is frozen.
- Return `503 write_frozen` with `Retry-After` from ingest and passkey
  registration while keeping authenticated reads available.
- Keep passkey login available without persisting counters or last-auth times
  during a freeze; logout remains available.
- Keep the worker process alive but suppress heartbeat, reconciliation, alert,
  and retention writes while frozen.
- Make `/readyz` freeze-aware: validate PostgreSQL and the freeze deadline,
  bypass worker-heartbeat freshness, and report `write_frozen=true`.
- Fail closed after a freeze deadline. An explicit abort may thaw the source
  only while the public edge still targets that source.
- Keep provider-specific freeze/thaw orchestration, edge verification, and
  evidence in private `hexroute-infra`; this public change exposes only the
  provider-neutral database and runtime contract.
- Non-goals: changing Twilight ownership, AdGuard, local VPN paths, provider
  credentials, DNS, or adding cloud authority over local recovery.

## Capabilities

### New Capabilities

- `cutover-write-freeze`: Transactional, deadline-bounded write exclusion and
  runtime behavior required for a lossless database cutover.

### Modified Capabilities

- `cloud-telemetry`: Freeze-aware ingestion, worker, readiness, and passkey
  behavior during an infrastructure cutover.

## Impact

The change affects PostgreSQL migrations and grants, API/worker runtime
composition, ingestion persistence, dashboard authentication persistence,
readiness responses, maintenance jobs, and integration tests. Rollout uses one
immutable image digest deployed to old and green environments in normal mode;
freeze is enabled only after both pass regression checks. Rollback keeps the
previous digest available and explicitly thaws the old source only after the
private edge guard proves public traffic still targets it. Production
ownership remains with the existing Twilight/local runtime and the old cloud
environment until the private cutover gate completes.
