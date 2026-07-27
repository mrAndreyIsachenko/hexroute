# Cloud Migration Runtime

`hexroute-ingest migrate` is a bounded App Platform `PRE_DEPLOY` job. It uses
the same immutable image as the API and worker, has no ingress, and receives
only the migrator database URL plus non-secret initial-principal metadata.

The job obtains a PostgreSQL transaction-scoped advisory lock, verifies the
embedded ordered migration manifest against `hexroute_schema_migrations`, and
applies pending expansion migrations before an API or worker rollout. A
reviewed legacy schema without the ledger is adopted only when all expected
tables, owners, roles, columns, indexes, and grants are present. Partial or
unknown schemas fail closed.

Migration 13 adds the provider-neutral transaction barrier documented in
[`cutover-write-freeze.md`](cutover-write-freeze.md). The migrator is the only
application role allowed to mutate cutover state. Provider commands and live
edge evidence are intentionally not part of this public runtime.

After migration, an empty `dashboard_principals` table receives one enabled
`operator` with a random UUID and WebAuthn user handle. Repeated deployments do
not replace the principal or its passkeys. The public API has no principal
creation endpoint or migrator credential.

Rollback uses the previous image digest and App spec. Expansion migrations and
the existing principal remain in place; migration down files are for
disposable compatibility tests, not automatic production rollback.
