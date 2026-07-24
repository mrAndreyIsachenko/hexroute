# Signed Telemetry Ingestion

Hexroute's cloud ingest core accepts the same gzip batch and canonical
Ed25519 envelope produced by the node uploader. It has no control-plane command
or callback path.

## Validation order

1. Reject malformed identities and compressed bodies outside the fixed 1 MiB
   limit.
2. Load the node's public key and current rotation status from PostgreSQL.
3. Verify the envelope schema, body SHA-256, timestamp window and Ed25519
   signature without consuming an in-memory replay token.
4. Strictly decode the gzip batch and each allowlisted event schema.
5. Require the signed node identity to match every event.
6. Commit request replay protection, batch metadata, events, sequence state and
   gap records in one PostgreSQL transaction.

Request IDs are unique replay tokens. Event UUIDs are the idempotency key for
at-least-once delivery under a new request. An existing UUID is acknowledged
only when its immutable metadata and JSON payload match exactly. Reusing a
sequence with a different event UUID rejects and rolls back the complete batch.

Per-node/session advisory transaction locks serialize sequence high-water
updates without serializing unrelated nodes. A jump records the missing range;
late events shrink, split or resolve that range. Accepted batches also update
the node's bounded first/last-seen timestamps.

## Security audit

Rejected signature, timestamp, replay, size and schema checks write a separate
best-effort audit record. Audit rows contain only an application-generated UUID,
optional registered node/request UUIDs, an allowlisted category, a fixed reason
code and timestamp. They never contain the submitted envelope, event payload,
source address, credential or raw error text.

Database failures do not masquerade as client rejection and never cause cloud
logic to request a node restart. The API layer can map `ErrRejected` to a
bounded client response and `ErrUnavailable` to service unavailability without
returning PostgreSQL detail.

## Verification

`make postgres-test` applies the migrations to a disposable PostgreSQL 17
container published only on a random loopback port, verifies role isolation,
then exercises the ingest
store through an ingest-role login. The test proves:

- valid signed persistence and acknowledgement;
- UUID deduplication under a new batch/request;
- request replay rejection and bounded audit;
- sequence-gap creation and late resolution;
- conflicting sequence rollback;
- migration rollback after the ingest checks.
