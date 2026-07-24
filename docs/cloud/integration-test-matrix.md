# Cloud Integration Test Matrix

Hexroute exercises the cloud data plane against a disposable PostgreSQL 17
container. The suite does not contact or mutate live Mac, VPS, provider, AdGuard
or Pritunl state.

Run the complete local and PostgreSQL gates:

```sh
make check
make postgres-test
```

## Required Scenarios

| Guarantee | Test |
| --- | --- |
| Cloud loss cannot block local recovery, and evidence drains only after an acknowledgement | `TestCloudLossRetainsEvidenceWhileLocalRecoveryContinuesAndDrainsOnReturn` |
| Retried event UUIDs produce one logical event while the retry is acknowledged | `TestPostgresStorePersistsDeduplicatesAndTracksSequenceGaps` |
| Missing and stale worker heartbeats fail readiness, while a new heartbeat restores it | `TestPostgresHeartbeatDrivesReadiness` |
| Explicit sleep suppresses silence, duplicate projections are idempotent, and unmatched wake evidence cannot suppress silence | `TestPostgresSleepProjectionSuppressesOnlyExplicitSleep` |
| Incident transitions and immutable alert snapshots commit together, drain once and preserve transition-time state | `TestPostgresIncidentOutboxQueuesSnapshotExactlyOnce` |
| The maintenance worker attests its role, writes heartbeat freshness and exits within its shutdown bound | `TestPostgresWorkerRuntimeHeartbeatsAndShutsDown` |
| A passkey assertion establishes a bounded session and advances credential state through only the auth role | `TestPostgresPasskeyLoginAuthorizesSessionAndAdvancesCounter` |
| Migrator, ingest, dashboard, dashboard-auth and maintenance grants allow only their documented operations | `tests/postgres_migrations_test.sh` role probes |

The PostgreSQL harness applies every migration in order, runs the scenario
tests through restricted login roles, rolls every migration back in reverse
order and requires the public schema to be empty afterward.
