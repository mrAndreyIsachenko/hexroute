# Design: record the shape a runtime reads

## Context

See proposal.md — Why. Four things were measured before this was written.

- The root example carries 46 settings; the installed configuration holds 112.
  It has no `tunnel_supervision` and no `policy_control` at all.
- The pre-install comparison already exists and is already required.
  `hexrouted --check --config X --installed Y` refuses a candidate that drops a
  setting and names it, and `policy-generation-continuity` requires exactly
  that. Nothing here replaces it; it had nothing to be checked against.
- The example decodes today, and a `tunnel_supervision` block with its full
  `execution` sub-block decodes with placeholder values.
- A `policy_control` block decodes with placeholders **only** when they are
  self-consistent: `StaticConfig.Runtime` requires
  `SHA256Hex(pinned_public_key) == signer_fingerprint`. A zero key with its own
  true digest passes. An all-zero fingerprint does not, which is why the first
  attempt at this failed.

## Goals / Non-Goals

Goals: the shape of each runtime's configuration is in the repository; the
examples decode; a setting cannot enter or leave a decoder without the example
following; no live value enters Git.

Non-Goals: changing the decoders; changing the pre-install comparison; reading
the host from a gate; recording *values* of any kind; carrying the working
copy's private configuration in the repository, which is impossible by
construction and must not become possible.

## Decisions

### The gate reflects over the wire struct, in Go

The settings come from the decoder's own wire types — `rootdaemon.Config` and
the user domain's equivalent — walked by reflection over their `json` tags,
including the types they reach into (`policyconfig.StaticConfig`,
`TunnelSupervisionConfig`, `TunnelExecutionConfig`, `PayloadProbeConfig`,
`RouteConfig`, `EndpointConfig`). The gate is a Go test, because reflection is
how a type is read and a shell gate would have to be given a list.

A list beside the decoder is the failure being fixed, so the gate must not
contain one. The only hand-written part is the set of root types to start from,
which is two names and fails loudly when a runtime is added.

Alternative: a JSON Schema checked into the repository — rejected, because it is
a second statement of the decoder, and keeping two statements in step is the
problem, not the solution.

### The comparison runs in both directions

A setting the decoder accepts and the example omits fails; so does a setting the
example carries and the decoder does not. Only the first direction was the
measured failure, but a record that describes a setting the runtime would reject
is wrong in the same way and costs nothing to catch.

### `omitempty` does not excuse a setting

The decision taken with the proposal: every field the decoder accepts, optional
or not. An optional setting is the kind that rots — `tunnel_supervision` and
`policy_control` are both optional, and both are absent from the example while
being the whole of what two changes built.

Mutually exclusive branches, if any appear, are answered by a comment in the
example rather than by leaving a setting out.

### A slice shows the shape with one element

Where the decoder accepts a list, the example carries at least one element, and
the gate walks the element type. An empty list records a key and no shape.

### Trust material is an empty value, and the decode test supplies it

This was decided twice. The first decision was a zero ed25519 key with its own
true SHA-256 as the fingerprint, which the decoder accepts because it checks one
against the other. `internal/repositoryguard` then refused the example outright:
`pinned_public_key` and `signer_fingerprint` must be **empty** in any tracked
artifact, whatever the value would have been. That boundary is older, deliberate
and stricter than the placeholder rule — a key-shaped string in the repository
is the leak whether or not it opens anything — so the design follows it.

The example therefore carries both settings with empty values, and anything
needing a configuration a runtime will load supplies the material itself. The
repository already held that convention: `tests/install_reduction_guard_test.sh`
builds a key from thirty-two fixed bytes with the comment that a real pinned key
in a fixture would be the leak this repository guards. `tests/example-with-trust.py`
is the same convention in one place, for the three gates that need a loadable
copy of an example.

Digests that stand alone — `static_sha256`, `trusted_compiler_sha256` — are
all-zero hex, which the boundary permits because they are not keyed on anything.

Alternative: leaving `policy_control` out of the example because it carries
trust material — rejected. That is how it went missing, and it is the block
whose absence hid a whole control plane.

### The examples' consumers change with them

Four gates read an example as a configuration a daemon will load, and a complete
example no longer is one. They supply the trust material first.
`tests/install_reduction_guard_test.sh` needed more than that: it built its
fixtures by relying on the example to *lack* `policy_control` and
`pritunl_service_label`, and a reduced candidate can no longer be the example
as it stands. Its fixtures now remove what they mean to remove, which is what
they always meant.

## Risks / Trade-offs

- **The gate asserts a key and someone writes a value beside it.** → The
  existing `secret-test` and the canary fixtures still run over the examples.
  The new gate must assert the presence of keys and never compare a value, so it
  cannot become a reason to put a real one in.
- **An exhaustive example is read as a recommended configuration.** → The
  example gains a line saying it is a record of the shape, and that the values
  are placeholders chosen to decode, not to run.
- **A decoder's wire type is reached only through an interface or `any`,** which
  reflection cannot walk. → None exists today; the gate fails loudly rather than
  skipping a field it cannot resolve, so this appears as a refusal rather than a
  silent gap.
- **Reflection over a recursive type loops.** → The walk records the types it has
  entered and refuses a cycle rather than recursing.

## Migration Plan

Nothing is installed and no runtime restarts. The examples grow, a gate is
added, two documents gain the comparison to run before an install.

The working copy's own divergence from this machine is carried forward once, as
a task: the live root configuration is copied over `private/root-observe.json`
after a comparison refuses to overwrite anything the working copy holds and the
machine does not. The user domain's copy was carried forward on 2026-10-08
already, byte-identical, with the previous version kept beside it. Rollback for
that part is the kept copy; for everything else it is the commit.

## Open Questions

- Whether the user domain's decoder reaches types the root one does not. It is
  read when the gate is written; the answer changes the gate's starting set and
  nothing else.
