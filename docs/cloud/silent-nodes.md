# Sleep-Aware Silent Nodes

Cloud silence is evaluated from signed evidence, not inferred as sleep.

Mac nodes emit the critical `node.sleep` schema with one of three allowed
transitions:

- `started/lid_closed`;
- `started/system_sleep`;
- `ended/full_wake`.

The worker projects those immutable events into explicit PostgreSQL sleep
intervals. A node can have at most one open interval, and start/end event IDs
are unique. Projection is idempotent; an unmatched wake becomes a zero-length
evidence interval but never suppresses a silent-node result.
Each worker pass selects at most 100 unprojected events in timestamp and
sequence order. A per-node transaction advisory lock serializes concurrent
projectors, so worker restart cannot create a second open interval.

For active nodes, the worker calculates a deadline from the last accepted event
(or node creation before first contact), expected heartbeat interval, missed
heartbeat count and minimum grace. A Mac is `sleeping` only when a signed sleep
interval covers the evaluation instant. Otherwise it is `healthy` before the
deadline and `silent` after it. Retired/revoked nodes are ignored and
implausibly future timestamps fail validation.

The incident reconciler consumes these deterministic decisions. A silent
result opens or refreshes one correlation key per node; a healthy result
resolves it with recovery evidence, and a signed sleep interval resolves it
with exclusion evidence. Correlation does not send alerts or control any node.
