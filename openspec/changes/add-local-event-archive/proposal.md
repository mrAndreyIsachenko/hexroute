## Why

Hexroute keeps every typed event in an upload spool, and the spool deletes each
record once telemetry acknowledges it. Raising its bound changes nothing:
retention there is a function of upload success, not of time. The consequence is
that the host cannot answer questions about last week. Incident review, drift
analysis and any offline study of how the machine actually behaves have no
durable local source, while the redacted cloud projection is deliberately too
narrow to substitute for one.

## What Changes

- Add a local append-only event archive that receives the same `internal/event`
  records the spool receives, retained by age and total size rather than by
  upload state, with the same crash-safe write discipline as the policy store.
- Keep the archive strictly inside the existing typed schemas. It stores what
  the schemas can express and nothing else, so the redaction properties already
  proven for those schemas are inherited rather than re-argued.
- Bound the archive explicitly: a configured maximum age and maximum size, with
  eviction by priority and an observable overflow record, so a full archive is a
  visible condition instead of silent truncation.
- Add a read API and an offline report tool that summarizes a window of the
  archive: event counts by schema and component, transition sequences, and a
  deterministic rarity ranking of what occurred.
- Add an optional local model pass over that summary, off by default, which may
  only add commentary to a report the deterministic ranking already produced.
  The model never selects, filters or orders the findings.
- Add a weekly scheduled local run that produces a dated report and nothing
  else. It performs no mutation, sends nothing off the host and does not touch
  the cloud projection.

Non-goals are uploading the archive, extending the cloud schema, changing the
spool's upload contract, retaining anything the typed schemas cannot already
express, and letting a model influence what the report contains.

## Capabilities

### New Capabilities

- `local-event-archive`: Defines durable local retention of typed events by age
  and size, its bounded eviction and overflow behaviour, its read contract, and
  the deterministic report the weekly review produces.

### Modified Capabilities

- `cloud-telemetry`: States that archiving is local-only and that a record's
  presence in the archive neither delays nor duplicates its upload, so the two
  paths cannot interfere.

## Impact

- Adds a local archive package and a report command; no daemon gains a new
  privilege, and neither daemon gains a mutation path.
- Adds disk usage bounded by explicit configuration on the host only.
- Adds one scheduled local job. It has no network, no credential and no
  privileged operation.
- Does not change provider resources, Twilight, AdGuard, Pritunl, sing-box, the
  cloud schema or current production ownership.
