# Cloud Readiness

The API readiness endpoint is intentionally smaller than the operational
dashboard. It returns `200 {"status":"ready"}` only when:

- PostgreSQL responds through the API's ingest-role connection; and
- the configured worker heartbeat exists and is newer than the stale
  threshold without being implausibly far in the future.

Every failure returns the same bounded
`503 {"status":"not_ready"}` response with `Cache-Control: no-store`. Database
errors, worker instance IDs, versions and timestamps are not returned. Only
`GET` is accepted.

The worker writes its fixed name, random instance UUID, public application
version, start time and heartbeat time through the maintenance role. Older
heartbeat writes cannot overwrite a newer row. The API receives only `SELECT`
on `worker_heartbeats`; it cannot publish its own freshness evidence.

`make postgres-test` creates separate ingest and maintenance login identities
in a disposable PostgreSQL instance and proves missing, fresh and stale worker
states through those exact role boundaries.
