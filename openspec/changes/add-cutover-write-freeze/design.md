## Context

The old and green cloud environments can run the same Hexroute image, but the
current application has no point at which a final PostgreSQL copy becomes
stable. Stopping containers is insufficient because health automation can
restart them and because an in-flight request may commit after the copy begins.
The public repository owns the provider-neutral enforcement mechanism; the
private infrastructure repository owns the live freeze, copy, DNS, abort, and
evidence workflow. Twilight and local recovery remain unchanged.

## Goals / Non-Goals

**Goals:**

- Establish a transaction boundary that drains in-flight writes and prevents
  all later runtime writes.
- Keep read-only operator access and cryptographically verified passkey login
  available during the bounded cutover window.
- Keep API and worker processes observable without allowing background writes.
- Fail closed when a cutover is abandoned or exceeds its deadline.
- Give private infrastructure an explicit, least-privilege state contract to
  copy, verify, freeze, and conditionally abort.

**Non-Goals:**

- Exposing freeze/thaw over the public HTTP API.
- Changing DNS, provider identities, Terraform roots, Twilight, AdGuard, or
  local VPN/recovery ownership.
- Automatically thawing after a deadline or after process restart.
- Allowing cloud services to mutate local machines.

## Decisions

### PostgreSQL is the freeze authority

A singleton `cutover_write_control` row stores `cutover_id`, `write_frozen`,
`frozen_at`, and `deadline_at`. PostgreSQL is already the consistency boundary
for all affected writes, so a process-local flag or container scale-to-zero
cannot provide equivalent guarantees. The migrator owns the row; runtime roles
can only read it and execute a fixed security-definer gate function.

### Database triggers enforce the write gate

Every runtime-mutated table receives a statement-level trigger. Before an
`INSERT`, `UPDATE`, or `DELETE`, the trigger takes a shared row lock on the
singleton control record and raises the fixed SQLSTATE `55000` error
`write_frozen` when frozen. A freeze `UPDATE` needs the conflicting exclusive
row lock, so it waits for all in-flight write transactions to commit or roll
back before returning. Trigger enforcement covers current write paths without
depending on every Go package to remember a wrapper; migration tests keep the
protected-table inventory explicit for future schema changes.

### Freeze state never expires open

`deadline_at` is an operational deadline, not an automatic lease. Writes stay
blocked after it passes and readiness fails, forcing an explicit operator
decision. The private abort command may thaw the old database only after its
edge-origin guard proves public traffic still points to old. There is no public
thaw endpoint and no timer-driven thaw in the application.

### API behavior is endpoint-specific

Ingest and passkey registration reject early with HTTP 503, a stable
`write_frozen` body, and `Retry-After`. The database trigger remains the final
race-safe enforcement. Read-only dashboard routes, login challenge creation,
assertion verification, and logout remain available. During freeze, a valid
login creates only an in-memory challenge/session and skips passkey counter,
credential, and last-authentication persistence.

### Worker is quiescent, not terminated

The worker polls freeze state before heartbeat and maintenance cycles. While
frozen it remains alive and logs bounded state transitions, but it does not run
database-writing jobs or Telegram delivery. Database triggers protect races in
which freeze begins between the poll and a write.

### Readiness represents a deliberate frozen source

Normal readiness still requires PostgreSQL and a fresh worker heartbeat. While
frozen and before the deadline, readiness requires PostgreSQL plus a valid
control row, bypasses worker freshness, and returns `status=ready` and
`write_frozen=true`. After the deadline or with malformed state it is not
ready. Liveness remains process-only.

## Risks / Trade-offs

- **[A runtime table is omitted from trigger installation]** -> Migration tests
  enumerate every table granted to runtime writer roles and require the gate
  trigger on each one.
- **[Trigger errors leak internal details]** -> HTTP mapping uses a fixed public
  error code; logs and responses contain no cutover UUID, credentials, or
  provider data.
- **[Freeze races with active writes]** -> The exclusive control-row update
  blocks until shared trigger locks drain; the final copy begins only after the
  update commits.
- **[Worker side effects occur during freeze]** -> The worker checks before jobs
  and the transactional outbox remains frozen; no delivery is attempted without
  first acquiring persisted work.
- **[Deadline expires during an outage]** -> The system fails closed and
  readiness exposes the fault. Recovery is an explicit private-infrastructure
  abort or completion action, leaving Twilight/local recovery untouched.

## Migration Plan

1. Apply the additive migration to old and green and verify normal-mode writes.
2. Build one immutable image and deploy the same digest to old and green.
3. Verify API, worker, passkey, readiness, and freeze regression tests on both.
4. Retain the previous digest and database backup as rollback inputs.
5. Let private infrastructure regenerate the guarded cutover plan and approval.
6. Freeze old, wait for the transaction barrier, copy and verify, then switch
   the edge. On pre-switch failure, prove the edge is old before explicit thaw.
7. Never thaw old after the edge has moved to green; rollback then follows the
   private reverse-cutover contract.

## Open Questions

None. Live provider identifiers, timing values, and evidence locations remain
private infrastructure inputs.
