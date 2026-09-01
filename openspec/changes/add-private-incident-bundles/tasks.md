# Tasks

## 1. Record What Already Exists

Each item below was built before this change and is ticked only after being
read in the tree, not because a document said so.

- [x] 1.1 Assemble a bundle from events already linked to an incident, passing every record through the strict event decoder (`internal/incidentbundle/create.go`, `event.Decode`).
- [x] 1.2 Bound a bundle in record count and encoded size (`telemetry.MaxIncidentBundleEvents` = 128, `telemetry.MaxIncidentBundleCompressedBytes` = 1 MiB).
- [x] 1.3 Address a bundle by the digest of its complete encoded content and reuse the retained row when identical content is assembled again (`loadExisting`, `objectKey`).
- [x] 1.4 Record an expiry with each bundle and remove both the stored object and the row naming it when it is reached (`internal/incidentbundle/expiry.go`, `storage.DeletePrivate`).
- [x] 1.5 Persist the bundle model and its expiry in PostgreSQL (`migrations/postgres/000009_incident_bundle_expiry`).
- [x] 1.6 Cover creation, reuse and expiry with PostgreSQL integration tests that the gate actually runs (`internal/incidentbundle/postgres_integration_test.go`, named in `tests/postgres_migrations_test.sh`).
- [x] 1.7 Document the storage contract an implementation must satisfy (`docs/cloud/incident-bundles.md`).

## 2. Close What Was Left Open

- [ ] 2.1 Call bundle creation from the maintenance worker, so that an incident's evidence is assembled rather than only assemblable.
- [ ] 2.2 Call the expiry worker from the same pass, so that a recorded expiry is acted on rather than only recorded.
- [ ] 2.3 Disable the pass when no bundle storage is configured, leaving incident correlation, retention and alerting unchanged.
- [ ] 2.4 Record that the pass was not attempted when storage is absent, so a deployment that was never finished can be told from one with nothing to bundle.
- [ ] 2.5 Assert that no path reads a bundle as input to a reduction, policy decision, action lease or mutation, and that the refusal does not depend on the bundle's content. No such assertion exists today.
- [ ] 2.6 Remove `incidentbundle` from the unwired list in `tests/package_reachability_test.sh` once the worker reaches it, and confirm the census reports the debt smaller.

## 3. Keep The Boundary

- [ ] 3.1 Configure object storage in `hexroute-infra`: private access, no public URL, idempotent write on identical key and content, and a lifecycle ceiling no longer than the recorded expiry.
- [ ] 3.2 Confirm no bucket name, credential, endpoint or lifecycle value enters public Git, and that `make secret-test` and the repository-boundary guard still pass.

## 4. Verification

- [ ] 4.1 Run `make check` and `make postgres-test` and resolve every failure.
- [ ] 4.2 Run `openspec validate add-private-incident-bundles --strict` and keep proposal, design, specs and tasks synchronized with what was built.
- [ ] 4.3 Sync the delta requirement into the baseline `cloud-telemetry` spec only after the worker connection and the storage contract are both in place.
