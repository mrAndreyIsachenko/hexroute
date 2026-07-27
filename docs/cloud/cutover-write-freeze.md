# Cutover Write Freeze

Hexroute uses a PostgreSQL transaction barrier before a database-backed cloud
environment can move from old to green. This is an application contract, not a
public control endpoint. Live freeze, copy, edge verification, DNS activation,
abort, and evidence remain in the private `hexroute-infra` repository.

## State And Locking

`cutover_write_control` contains exactly one row. Normal state has
`write_frozen=false` and null cutover metadata. Frozen state requires a UUID,
`frozen_at`, and a later `deadline_at`.

Every table writable by an API or worker role has a statement trigger that
calls `hexroute_enforce_writable()`. The function takes `FOR SHARE` on the
control row before the statement can mutate data. The migrator's freeze update
needs an exclusive row lock, so it returns only after older write transactions
commit or roll back. Later runtime writes fail with SQLSTATE `55000` and the
fixed message `write_frozen`.

Runtime roles can read control state but cannot update it. The migrator owns
the table and function. Migration tests enumerate the complete protected-table
set and exercise lock draining and post-freeze rejection against PostgreSQL 17.

## Frozen Runtime

- Signed ingest and passkey registration return HTTP 503 with
  `status=write_frozen` and `Retry-After: 60`.
- Existing passkeys can complete cryptographic login, but sign counters,
  credential metadata, and last-authentication timestamps are not persisted.
- Read-only dashboard requests and logout remain available.
- The worker process remains alive but does not write heartbeat, reconcile
  incidents, claim alerts, deliver Telegram messages, or run retention.
- `/readyz` bypasses worker freshness only while PostgreSQL reports a valid
  frozen state before its deadline. The response includes
  `write_frozen=true`.

The database trigger is the final race-safe guard if freeze begins after a
process checks state.

## Rollout And Rollback

Apply the additive migration first, then deploy the same immutable image digest
to old and green in normal mode. Verify both environments and retain the prior
digest and database backup before private cutover approval is regenerated.

Freeze does not expire open. After `deadline_at`, writes remain blocked and
readiness fails. A pre-switch abort may thaw old only after private automation
proves the public edge still targets old. Once the edge targets green, old is
never casually thawed; rollback follows the reviewed reverse-cutover workflow.
No step changes Twilight, AdGuard, local routes, VPN processes, or local
credential ownership.
