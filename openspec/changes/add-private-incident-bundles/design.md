# Design

## Context

This change is unusual in that most of what it describes is already built.
`internal/incidentbundle` carries creation and expiry, migration `000009` is
applied and exercised, and its integration tests run under the PostgreSQL
gate. What is absent is the requirement, and with it any statement of what the
work was for.

That absence is the reason to write this down rather than simply calling the
worker. A connection made without a requirement produces the same situation
again: something that runs, passes, and answers to nothing.

## Goals / Non-Goals

**Goals:**

- Say what a bundle is, what bounds it, and what expiry means.
- Make the storage contract a stated precondition rather than an assumption
  living in a document beside the code.
- Make the missing connection visible as an open task instead of an absence.

**Non-Goals:**

- No local control-plane behaviour changes. Nothing here can reach a host.
- No object storage is configured by this repository. The credentials, bucket
  and lifecycle stay in `hexroute-infra`.
- No new redaction mechanism. The strict event decoder and the deterministic
  encoder already in the package are what bound a bundle's content, and this
  change describes them rather than replacing them.

## Decisions

### The worker assembles bundles; the host never does

A bundle is written to private object storage, and the host has no path to
object storage and must not acquire one. The local runtime already uploads
signed events; correlating them into an incident and assembling evidence from
it is work the cloud side already does for incidents, retention and SLOs.

The consequence worth stating: losing the cloud costs bundles and nothing
else. No local recovery, reduction or policy decision waits on one, because
nothing may read one back.

### Absent storage disables the pass rather than failing it

A worker that refused to run because bundle storage was unconfigured would
take down incident correlation, retention and alerting for a capability that
produces evidence a person reads afterwards. So an unconfigured deployment
creates no bundle, records that the pass was not attempted, and is otherwise
unchanged.

Recording the skip matters as much as skipping. A pass that quietly does
nothing is indistinguishable from a pass that found nothing to do, and those
are different states — one is a deployment that was never finished.

### A bundle is addressed by its content

The object key derives from the digest of the complete encoded bundle, so
assembling the same incident snapshot twice reuses the stored row instead of
uploading a second object. This is what makes repeated assembly safe to retry,
and it is why the idempotent-write requirement is part of the storage contract
rather than an implementation detail: a store that wrote a second object for
identical content would make retry a cost rather than a no-op.

### Nothing reads a bundle back

The refusal is stated so that it does not depend on a bundle's content. A
bundle that happened to contain nothing sensitive would still be refused as
input, because the property being protected is that cloud-side evidence has no
authority over a host — and a rule that inspected content first would be a
rule that could be argued with.

## Risks / Trade-offs

- **The bundle stays unreachable until storage exists.** This change does not
  close the debt on its own; it makes the debt legible. The task that connects
  the worker stays open, and the reachability census keeps reporting the
  package as unwired until it is done.
- **Expiry removes evidence.** A 30-day ceiling is a deliberate bound on how
  long private incident detail is retained, and an incident investigated later
  than that has aggregate history rather than bundled evidence. Extending it is
  a decision about retention, not about this mechanism.

## Open Questions

- Whether bundle creation should be triggered by incident resolution or by a
  periodic pass. The existing creator supports either; the requirement does not
  choose, because the answer depends on operational load that this deployment
  has not yet measured.
