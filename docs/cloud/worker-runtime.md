# Cloud Worker Runtime

`hexroute-ingest worker` is the long-running telemetry-only maintenance mode.
It validates its complete configuration, opens one bounded PostgreSQL pool and
proves that the login belongs exclusively to `hexroute_maintenance` before
starting any job.

## Required Configuration

| Variable | Purpose |
| --- | --- |
| `HEXROUTE_MAINTENANCE_DATABASE_URL` | Login exclusively in `hexroute_maintenance` |
| `HEXROUTE_TELEGRAM_BOT_TOKEN` | Telegram Bot API token supplied at runtime |
| `HEXROUTE_TELEGRAM_CHAT_ID` | Destination numeric chat ID or channel handle |
| `HEXROUTE_WORKER_NAME` | Heartbeat identity; defaults to `primary` |
| `HEXROUTE_TIMEZONE` | Alert quiet-hours location; defaults to `Europe/Moscow` |

Secrets and database URLs are validated but never included in logs. Startup
fails closed if a required value, database connection or role attestation is
invalid.

## Scheduled Jobs

Each job runs immediately at startup and then on its own fixed ticker. A job
cannot overlap its previous invocation, every invocation has a deadline and
failures are recorded with an allowlisted event name before the next retry.

| Job | Default interval | Work |
| --- | --- | --- |
| heartbeat | 30 seconds | Replace the named worker freshness record |
| reconcile | 30 seconds | Project up to 100 sleep events, evaluate nodes and reconcile incidents |
| alert queue | 10 seconds | Drain up to 50 incident snapshots into delivery rows |
| alert delivery | 10 seconds | Claim and send up to 50 Telegram or digest deliveries |
| retention | 1 hour | Delete at most 500 eligible rows from each retention class |

The intervals can be overridden with
`HEXROUTE_HEARTBEAT_INTERVAL`, `HEXROUTE_RECONCILE_INTERVAL`,
`HEXROUTE_ALERT_QUEUE_INTERVAL`, `HEXROUTE_DELIVERY_INTERVAL` and
`HEXROUTE_RETENTION_INTERVAL`. `HEXROUTE_JOB_TIMEOUT` and
`HEXROUTE_SHUTDOWN_TIMEOUT` bound individual work and process shutdown. Every
override is range checked.

An incident transition and its immutable alert snapshot are committed in the
same PostgreSQL transaction. Workers claim snapshots with expiring leases and
`FOR UPDATE SKIP LOCKED`; delivery insertion is idempotent by incident
generation and channel. A crash can delay an alert but cannot leave an
unobservable transition-to-alert gap.

Heartbeat failure stops the worker so `/readyz` becomes stale instead of
claiming health. Other job failures remain isolated and retry on their next
tick. On `SIGTERM`, all jobs receive cancellation and the process waits only
for the configured shutdown bound.
