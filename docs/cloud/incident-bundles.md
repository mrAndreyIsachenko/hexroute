# Private Incident Bundles

The maintenance worker creates an incident bundle only from events already
linked through `incident_events`. It selects at most the newest 128 retained
evidence events, restores their immutable metadata and passes every record
through the strict event decoder before using the existing deterministic
gzip encoder. Unknown schemas, malformed payloads and unrestricted raw output
cannot enter a bundle. The compressed result is capped at 1 MiB.

The object key is derived from the SHA-256 digest of the complete encoded
bundle. Repeating the same incident snapshot therefore reuses the PostgreSQL
row and does not upload a second object while it is retained. A policy request
after confirmed deletion may repopulate that row and starts a new 30-day
expiry. The storage implementation contract requires:

- private access with no public URL;
- an idempotent write when key and content are identical;
- `application/json` with gzip content encoding;
- a lifecycle ceiling equal to the recorded 30-day expiry.

PostgreSQL stores only the object key, digest, compressed size, creation time
and expiry state. Provider credentials, signed URLs, raw errors and object
contents are never persisted there.

Expired rows are claimed with `FOR UPDATE SKIP LOCKED` and a two-minute lease.
The worker marks a row deleted only after private storage confirms deletion;
object-not-found must be treated as successful deletion by the storage adapter.
A provider failure clears the lease, stores only a generic result code and
retries with bounded exponential delay from one minute to one hour. Attempts
continue indefinitely and saturate at the database limit instead of silently
abandoning an object.

Provider lifecycle expiry remains a defense in depth for an object uploaded
immediately before a database transaction fails. Database expiry processing
does not delete the durable incident or its normalized lifecycle.
