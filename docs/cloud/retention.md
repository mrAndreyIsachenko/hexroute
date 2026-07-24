# PostgreSQL Retention

The maintenance worker applies fixed UTC-age policies:

- observation, diagnostic and sleep events: 30 days from cloud receipt;
- normalized security audit detail: 30 days;
- closed sleep intervals and resolved sequence gaps: 30 days;
- transition, recovery, incident, deployment and config lifecycle events:
  180 days from cloud receipt;
- normalized incident transitions: 180 days;
- delivered or suppressed alert-delivery history: 180 days;
- processed incident-alert outbox snapshots: 180 days;
- incidents, deployments, config versions, SLO aggregates and SLO-to-incident
  links: no automatic expiry.

Pending or failed alert deliveries, unprocessed alert snapshots, open sequence
gaps and open sleep intervals are never selected by retention.

One transaction-scoped PostgreSQL advisory lock serializes retention workers
without granting the maintenance role `UPDATE` on immutable event rows. Each
table delete is capped by the configured batch size, and the complete pass
commits atomically. Repeated passes converge without an unbounded statement.

Deleting an expired event uses existing foreign-key behavior deliberately:

- `latest_component_states` and retained sleep intervals lose only their
  optional event pointer;
- `incident_events` evidence links to expired detail are removed;
- the incident itself remains;
- a batch is deleted only after all of its events are gone and no sequence gap
  still references it.

The retention worker does not delete object-storage bundles. Bundle expiry and
confirmed object deletion are a separate workflow.
