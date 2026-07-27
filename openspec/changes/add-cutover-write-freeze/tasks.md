## 1. Database Contract

- [x] 1.1 Add migration tests for the singleton control row, role grants, stable error, and complete runtime-table trigger inventory
- [x] 1.2 Add the additive PostgreSQL migration and rollback for cutover control state and transactional write-gate triggers
- [x] 1.3 Add a provider-neutral Go package for reading and classifying cutover freeze state

## 2. API Behavior

- [x] 2.1 Add regression tests for frozen ingest rejection and race-safe database enforcement
- [x] 2.2 Add regression tests for frozen passkey registration, write-free login, logout, and read-only dashboard access
- [x] 2.3 Add regression tests for normal, valid-frozen, expired-frozen, and malformed readiness responses
- [x] 2.4 Implement freeze-aware API middleware, passkey behavior, and readiness response mapping

## 3. Worker Behavior

- [x] 3.1 Add regression tests proving heartbeat, maintenance, alert delivery, and retention do not write while frozen
- [x] 3.2 Implement a freeze-aware worker gate that keeps the process alive and quiesces all write jobs

## 4. Release Safety

- [x] 4.1 Document immutable-image rollout, private orchestration ownership, fail-closed deadline behavior, and guarded abort rollback
- [x] 4.2 Run focused PostgreSQL integration tests, `make check`, and strict OpenSpec validation
- [x] 4.3 Commit and push the isolated feature branch without modifying Twilight or the active production runtime
