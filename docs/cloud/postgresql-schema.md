# PostgreSQL Schema

Hexroute stores cloud telemetry in PostgreSQL. The schema is split into
ordered, append-only expansion migrations:

1. node identity, signing keys and idempotent event ingestion;
2. current operational state, sleep intervals, incidents and deployments;
3. passkey public credentials, alert delivery state and SLO aggregates.

The schema stores public verification keys and passkey public credentials. It
does not model VPN credentials, private signing keys, OTP material, packet
captures or unrestricted raw logs.

## Migration contract

SQL files under `internal/database/migrations/postgres` are embedded in the
application through `migrations.PostgreSQL()`. Every version has a matching
`up` and `down` file and a SHA-256 checksum over the exact expansion SQL.

Production deployments apply each `up` migration in a transaction and record
the version and checksum through the deployment migrator. An application
rollback never runs a `down` file. Schema evolution uses expand/contract
changes compatible with the current and immediately previous application
version; a destructive contraction requires a later, separately approved
migration after that compatibility window closes.

The `down` files exist only to prove dependency ordering against a disposable
database. Run that check with:

```sh
make postgres-test
```

The integration test copies the SQL into an unexposed PostgreSQL 17 container,
applies every expansion in order, verifies the required tables, then rolls the
disposable schema back in reverse order. It does not require Docker Desktop to
share the repository directory with containers.

## Data boundaries

- Event UUIDs provide logical deduplication for at-least-once delivery.
- `(node_id, boot_session_id, sequence)` prevents conflicting sequence reuse.
- Sequence gaps remain explicit until resolved.
- Incident evidence links survive as structured relationships; raw adapter
  output is not a fallback evidence format.
- Sleep intervals are first-class inputs to silent-node and SLO calculations.
- Alert channels have separate delivery rows, so a local acknowledgement cannot
  clear a pending Telegram delivery.
- SLO aggregates link back to incidents that explain failures and exclusions.

Database grants are deliberately not part of these schema migrations. The next
migration stage creates and tests separate migrator, ingest, dashboard and
maintenance privileges.
