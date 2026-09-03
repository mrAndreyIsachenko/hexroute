# Declare private incident evidence bundles

## Why

The bundle exists in the tree and answers to nothing. `internal/incidentbundle`
holds 787 lines of creation and expiry, migration `000009_incident_bundle_expiry`
is applied and exercised, 520 lines of integration tests run in the PostgreSQL
gate, and `docs/cloud/incident-bundles.md` states the storage contract it
expects. The string `incident bundle` appears in `openspec/` zero times.

Nothing therefore tracks it as unfinished. The worker that the documentation
names as its creator runs, is reachable from `cmd/`, and never calls it — and
because no requirement says a bundle should ever be created, no gate has
anything to fail. Six weeks passed with the package green, ticked and
unreachable, and the census that reports it as unwired was the only thing
saying so.

This change writes the requirement the work was built against, so the missing
connection becomes a visible gap rather than an absence of opinion.

## What Changes

- Declare the durable private incident bundle: what may enter it, what bounds
  it, how its identity is derived and what expiry means.
- Declare the storage contract as a precondition rather than an assumption: no
  public URL, idempotent write on identical key and content, and a lifecycle
  ceiling equal to the recorded expiry. Configuring that storage stays outside
  this repository, and the requirement says so instead of leaving it implied.
- State that a bundle grants no authority: it is redacted evidence a person
  reads, and nothing in it can be replayed into a host, a policy generation or
  a mutation.
- Connect the maintenance worker to creation and expiry, behind explicit
  configuration, so that an unconfigured deployment is unchanged rather than
  broken.

No behaviour of the local control plane changes. Nothing here can reach a host.

## Capabilities

**New Capabilities:** none.

**Modified Capabilities:**

- `cloud-telemetry` — adds the bundle requirement beside the durable incident
  and retention model it extends.

## Impact

- `internal/incidentbundle` — reachable for the first time; no change to its
  creation or expiry logic is anticipated.
- `internal/cloudruntime` — the worker gains a bundle pass, disabled unless
  storage is configured.
- `internal/database/migrations/postgres/000009_incident_bundle_expiry` —
  already applied; this change describes what it was for.
- `docs/cloud/incident-bundles.md` — becomes the operator-facing half of a
  requirement rather than a standalone note.
- `tests/package_reachability_test.sh` — `incidentbundle` leaves the unwired
  list when the worker calls it.

Object storage credentials, bucket names and lifecycle configuration remain in
`hexroute-infra`. Nothing in this change adds a live endpoint to public Git.
