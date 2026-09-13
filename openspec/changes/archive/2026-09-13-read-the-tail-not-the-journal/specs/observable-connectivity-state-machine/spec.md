# Observable Connectivity State Machine Delta

## MODIFIED Requirements

### Requirement: Crash-safe bounded reconstruction

Root and user fact journals and the aggregate checkpoint SHALL be bounded,
crash-safe and generation-guarded. Every checkpoint SHALL bind its immutable
identity and parent digest, prior input snapshot digest, consumed host sequence
range and source watermarks, exact policy generations and manifest digest,
reducer identity/version, and canonical snapshot, diff and proposal output
digests. A bounded append-only index SHALL preserve retained checkpoint lineage.
Startup SHALL validate the lineage and replay later accepted facts.

Replay SHALL read the records it folds and no others. A journal's fold
positions rise with the order its records were written, so the facts after a
watermark are a suffix of it: they SHALL be found by reading from the newest
record backwards and stopping at the first one at or below the watermark. The
watermark a broken lineage rebuilds from SHALL likewise come from the newest
record of each journal rather than from all of them.

Startup cost SHALL NOT grow with what a journal retains. Measured on 2026-09-13:
the daemon ran 152 seconds before it reported starting and observed nothing in
that window, because finding the facts after a watermark meant decoding every
record both journals held — 136,397 of them, to find four.

The suffix SHALL remain guarded rather than assumed. A replay range that is not
continuous from the watermark is already refused, and that refusal is what
catches a journal whose order does not hold. Retention
SHALL preserve the latest complete baseline for every configured component
before evicting diagnostics, and overflow SHALL remain observable.

When the newest read-model checkpoint is invalid, startup MAY search backward
within a configured bound for the newest fully valid retained ancestor and
deterministically replay a continuous journal forward. Missing ancestry,
journal gaps, policy/reducer mismatch, depth exhaustion or output-digest
mismatch SHALL yield visible `unknown`/`conflict` state. This recovery SHALL NOT
move the atomic-policy active pointer backward or authorize an older policy.

#### Scenario: Daemon restarts after checkpoint persistence

- **WHEN** startup loads a valid checkpoint and accepted facts after its watermarks
- **THEN** replay reconstructs the same canonical current snapshot and diff

#### Scenario: A restart follows a nearly current checkpoint

- **WHEN** startup replays from a checkpoint a handful of facts behind a journal holding tens of thousands
- **THEN** it opens only the records it folds and the one that ends the search
- **AND** the time it takes does not grow with how many records the journal retains

#### Scenario: A journal's order does not hold

- **WHEN** the records found after a watermark are not continuous from it
- **THEN** startup publishes uncertainty rather than folding them

#### Scenario: Latest checkpoint is corrupt and a valid ancestor is retained

- **WHEN** latest-checkpoint validation fails but its bounded retained lineage and post-ancestor journal are complete
- **THEN** Hexroute selects the newest fully valid ancestor and deterministically replays forward under the current active policy
- **AND** it does not load the corrupt output, move policy backward or trigger a mutation

#### Scenario: Checkpoint lineage cannot be proven

- **WHEN** a parent link or output digest is invalid, an ancestor or journal range is missing, or the bounded search depth is exhausted
- **THEN** Hexroute publishes unknown/conflict state with the lineage or journal gap visible
- **AND** it does not load an unverified healthy snapshot or trigger a mutation

#### Scenario: A durable record is reopened by a later process

- **WHEN** a component that keeps state across a restart is opened over a root an earlier process wrote
- **THEN** it resumes from what that root holds rather than from its own beginning
- **AND** it neither reuses an identity already recorded nor reports a position its own retained records contradict

#### Scenario: Journal reaches its configured bound

- **WHEN** retention must free capacity
- **THEN** lower-priority diagnostics are removed before the latest component baselines and critical transitions
- **AND** a bounded overflow record remains visible

### Requirement: Sleep, wake and reboot re-baselining

Same-boot freshness SHALL use monotonic source time, while UTC SHALL be used for
portable display and telemetry. A boot-ID change SHALL invalidate prior
monotonic deadlines. A full wake SHALL mark time-sensitive network, DNS,
tunnel, relay and session components stale until their owners publish new
complete baseline facts.

#### Scenario: Mac wakes after a long sleep

- **WHEN** the runtime observes a full wake transition
- **THEN** time-sensitive components become stale before any pre-sleep state is summarized as healthy
- **AND** each owner must publish a new baseline to clear staleness

#### Scenario: Runtime starts under a new boot ID

- **WHEN** retained facts and checkpoint carry the prior boot ID
- **THEN** prior monotonic deadlines are not reused
- **AND** current state remains unknown or stale until re-baselined
