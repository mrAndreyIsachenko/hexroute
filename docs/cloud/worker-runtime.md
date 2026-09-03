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

Incident bundle storage is optional and is either wholly present or wholly
absent. A deployment given three of these five is refused at startup rather
than started, because a half-configured deployment would otherwise be
indistinguishable from one that was never given storage at all.

| Variable | Purpose |
| --- | --- |
| `HEXROUTE_BUNDLE_ENDPOINT` | HTTPS endpoint of the private object store |
| `HEXROUTE_BUNDLE_REGION` | Region the endpoint serves |
| `HEXROUTE_BUNDLE_BUCKET` | Bucket that holds bundles, with no public access |
| `HEXROUTE_BUNDLE_ACCESS_KEY_ID` | Access key identifier |
| `HEXROUTE_BUNDLE_SECRET_KEY` | Secret used to sign each request |

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
| connectivity projection | 30 seconds | Fold uploaded connectivity records into the stored read model |
| SLO | 30 seconds | Measure availability from evidence already stored |
| retention | 1 hour | Delete at most 500 eligible rows from each retention class |
| incident bundle | 1 hour | Assemble evidence for up to 16 closed incidents never bundled, then remove up to 16 bundles that reached their expiry |

The connectivity projection and SLO jobs had been running unlisted here; they
are named now rather than left for the next reader to discover from the source.

A closed incident is bundled once. An incident with nothing linked to it is
never selected, and an incident whose bundle was removed at its recorded expiry
is not bundled again — assembling one would restore what retention took away,
and the two passes would undo each other on every interval. Where bundle
storage is not configured the job still runs, creates nothing, and records
`cloud_incident_bundle_unconfigured`, so a deployment that was never finished
reads differently from one with nothing to bundle.

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

Before heartbeat and every scheduled job, the worker reads the singleton
cutover state. While frozen, or when that state cannot be read, it fails closed:
the process remains alive but performs no database write or Telegram delivery.
Database triggers protect the race between this check and a job's first write.
