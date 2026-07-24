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

For active nodes, the worker calculates a deadline from the last accepted event
(or node creation before first contact), expected heartbeat interval, missed
heartbeat count and minimum grace. A Mac is `sleeping` only when a signed sleep
interval covers the evaluation instant. Otherwise it is `healthy` before the
deadline and `silent` after it. Retired/revoked nodes are ignored and
implausibly future timestamps fail validation.

Incident opening and resolution consume these deterministic decisions in the
next stage; the correlator itself does not send alerts or control any node.
