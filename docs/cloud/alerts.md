# Alert Delivery

Every material incident transition writes an immutable snapshot to
`incident_alert_outbox` in the same transaction. The maintenance worker claims
those snapshots with an expiring lease and expands each generation into
independent delivery rows. Queueing is idempotent on
`(incident_id, incident_generation, channel)`. Each row stores the validated
status, severity, category, component and transition-time snapshot for that
generation, so delayed delivery cannot accidentally render newer incident
state.

Policy uses an explicit IANA/fixed time zone and configured night window:

- actionable active incidents queue Telegram and local macOS delivery
  immediately, including at night;
- non-actionable daytime changes queue Telegram immediately;
- non-actionable nighttime changes create an audited suppressed Telegram row
  and a morning-digest row due at the end of the night window;
- resolved incidents are never treated as actionable.

Local delivery acknowledgement updates only the `local_macos` row. It cannot
deliver, suppress or delete the independent Telegram row.

Telegram and morning-digest workers claim due rows with PostgreSQL
`FOR UPDATE SKIP LOCKED` and a one-minute-or-longer expiring lease. Ordinary
alerts are claimed one at a time so a batch cannot expire while earlier HTTPS
requests run. A crashed worker leaves no permanent lock: another worker can
claim the row after lease expiry. Failed sends retain a generic result code and
retry forever with bounded exponential delay starting at no less than one
minute; the attempt counter saturates instead of disabling delivery.

The outbox drain is capped at 50 snapshots per pass. Marking a snapshot
processed and inserting its channel rows occur in one transaction, so a crash
can delay queueing but retries cannot lose or duplicate a generation.

Morning-digest rows are claimed in a bounded group and rendered into one
summary with aggregate active, acknowledged and recovered counts. Telegram
messages contain only validated enums, generation and fixed text. Incident
UUIDs, node names, endpoints, raw evidence and adapter output are omitted.

The Telegram bot token and chat destination are runtime secrets. They are held
only by the HTTPS client and are never persisted in PostgreSQL, included in a
message or returned in an error. The client uses the fixed official
`sendMessage` endpoint, disables redirects, bounds request/response sizes and
returns only a generic unavailable error.

This cloud workflow sends notifications only. It has no route, process,
Pritunl, AdGuard or node-control authority.
