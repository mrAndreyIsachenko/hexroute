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

- [x] 2.0 Implement the object store the creator and the expiry worker take. `incidentbundle.Storage` is two methods and nothing in the tree implements either, so the worker has nothing to pass. Signed with SigV4 by hand rather than by adding an SDK: this repository has six direct dependencies and sixty lines of go.sum, and two operations do not justify dozens of modules.
- [x] 2.1 Call bundle creation from the maintenance worker, so that an incident's evidence is assembled rather than only assemblable. The rule that was missing is now written down: a closed incident with linked evidence that has never been bundled. Two exclusions are load-bearing rather than tidy. An incident with nothing linked returns `ErrNoIncidentEvidence` and nothing about it will change, so selecting it would fail identically on every pass forever. An incident whose bundle was removed at its expiry stays excluded because `Create` revives a deleted row rather than skipping it — the literal reading of "has no current bundle" would have the pass restore what expiry just removed, each undoing the other every interval, and retention would never take effect. `TestPostgresOnlyClosedIncidentsNeverBundledArePending` fails on that literal reading.
- [x] 2.2 Call the expiry worker from the same pass, so that a recorded expiry is acted on rather than only recorded. One incident failing to bundle does not hold back the expiry half: an expiry that is due stays due whether or not storage accepted some other object.
- [x] 2.3 Disable the pass when no bundle storage is configured, leaving incident correlation, retention and alerting unchanged. Storage is all five settings or none; three of five is refused at load rather than started, because a misconfigured deployment would otherwise be indistinguishable from an unconfigured one and both write the same log.
- [x] 2.4 Record that the pass was not attempted when storage is absent, so a deployment that was never finished can be told from one with nothing to bundle. The record is an event name, `cloud_incident_bundle_unconfigured`, because a log record carries a fixed field set and cannot say in a field what it did not do.
- [x] 2.5 Assert that no path reads a bundle as input to a reduction, policy decision, action lease or mutation, and that the refusal does not depend on the bundle's content. The assertion did exist, which this task said it did not: `internal/incidentbundle` is in the cloud-dependency set of `tests/policy_cloud_independence_test.sh`, so no local binary may import it and the refusal is structural rather than about any bundle's content. That gate now covers twelve local binaries rather than four.
- [x] 2.6 Remove `incidentbundle` from the unwired list in `tests/package_reachability_test.sh` once the worker reaches it, and confirm the census reports the debt smaller. `objectstore` left the list in the same pass. The census went from six packages and 1800 unrun lines to four and 638.

## 3. Keep The Boundary

- [x] 3.1 Already configured, and since 2026-07-26 — this task was written as though it were pending and never checked. `roots/production/foundation.tf` in `hexroute-infra` declares `module "incident_storage"` from `modules/private-spaces` with `retention_days = 30`, matching the expiry the bundle records, and creates a runtime key scoped to that bucket alone. The apply evidence records anonymous HTTP refused with 403, versioning on, retention 30, the runtime key allowed on its own bucket and denied on a foreign one. Idempotent write on identical key and content is the object store's own semantics rather than something the module configures.
- [x] 3.2 Confirmed. The live bucket name appears nowhere in this repository; the only matches are Terraform resource names in the generic module and, in `modules/app-platform`, a guard that refuses to hand the incident storage keys to the API component. `make secret-test` and the repository-boundary guard pass, and no local Terraform state exists here.

## 4. Verification

- [x] 4.1 Run `make check` and `make postgres-test` and resolve every failure.
- [x] 4.2 Run `openspec validate add-private-incident-bundles --strict` and keep proposal, design, specs and tasks synchronized with what was built.
- [x] 4.3 Sync the delta requirement into the baseline `cloud-telemetry` spec only after the worker connection and the storage contract are both in place.
