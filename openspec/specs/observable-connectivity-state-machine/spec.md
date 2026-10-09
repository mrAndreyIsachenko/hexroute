# Observable Connectivity State Machine Specification

## Purpose

Define the implemented observe-only connectivity read model: one causal view of
host connectivity, reproducible from ordered source facts and an exact policy
generation, that describes what is observed and what would be reconciled
without holding the authority to reconcile it.

## Requirements

### Requirement: Source-owned complete connectivity facts

Hexroute SHALL represent every connectivity observation as a strict bounded
typed fact describing the complete current state of one configured component.
Each fact SHALL identify its schema, event, privilege domain, component, source,
source boot, source sequence, wall observation time, monotonic tick, lifecycle
state, freshness deadline and allowlisted reason. Static configuration SHALL
assign exactly one authoritative source to each component, and corroborating
sources SHALL NOT overwrite authoritative state.

#### Scenario: Authoritative collector publishes a component baseline

- **WHEN** the configured owner observes the current state of its component
- **THEN** it publishes one complete baseline fact with its next source sequence
- **AND** the fact contains no command, arbitrary path or credential value

#### Scenario: A source claims a component outside its ownership

- **WHEN** a validly encoded fact names a component or domain not assigned to its authenticated source
- **THEN** the fact is rejected as an ownership conflict
- **AND** the prior authoritative component state remains unchanged

### Requirement: Ordered idempotent fact acceptance

The aggregate control plane SHALL assign a durable host acceptance sequence to
accepted facts and SHALL treat `(source_id, boot_id, source_sequence)` plus the
canonical fact digest as the source identity. An exact retry SHALL be
idempotent, reuse of that identity with different content SHALL create a
conflict, and a source sequence gap SHALL remain visible until a later explicit
complete baseline is accepted.

#### Scenario: A fact is delivered twice

- **WHEN** the same source identity and canonical digest are received again
- **THEN** the retry does not create another accepted event or snapshot generation

#### Scenario: A sequence identity is reused with different content

- **WHEN** an accepted source identity is received with a different canonical digest
- **THEN** the aggregate state records a source conflict
- **AND** it does not replace the accepted component fact

#### Scenario: Source sequence skips a value

- **WHEN** the next fact advances beyond the expected source sequence
- **THEN** the gap is recorded in source and snapshot metadata
- **AND** healthy aggregate output is not inferred from the missing observation

#### Scenario: One component restates itself while its source speaks for others

- **WHEN** a complete baseline arrives for one component of a source that is
  declared for more than one
- **THEN** the gap and the awaiting-baseline condition remain recorded, because
  a source sequence numbers the source and nothing in the surviving facts says
  which components the lost ones described
- **AND** the components still owing a complete restatement are named
- **AND** the gap is cleared only once every one of them has restated itself

#### Scenario: A source accumulates more holes than may be retained

- **WHEN** the bound on retained gap ranges is exceeded
- **THEN** the oldest ranges are dropped and the overflow is recorded on the
  source watermark, the snapshot summary and the redacted projection
- **AND** the retained ranges never exceed the bound in any persisted snapshot,
  so a stored read model always remains restorable

### Requirement: Normalized inspectable connectivity snapshot

Hexroute SHALL build one typed `ConnectivitySnapshot` containing source
watermarks, freshness and gap metadata, active policy generations, ownership
conflicts and separately inspectable component state for physical connectivity,
default path, DNS, scoped routes, managed transports, relays/ingress,
Pritunl/user access and session expiry. A derived operator summary SHALL NOT
remove or replace those component records.

#### Scenario: DNS fails while a tunnel remains connected

- **WHEN** accepted facts report a ready transport and failed DNS
- **THEN** the snapshot preserves both component states
- **AND** the aggregate summary reports degradation rather than flattening the host to connected

#### Scenario: A configured component has no fresh fact

- **WHEN** no authoritative fact exists before the component freshness deadline
- **THEN** the component is represented as `unknown` or `stale`
- **AND** it is not omitted from the snapshot

### Requirement: Pure deterministic generation-bound reduction

The reducer SHALL accept only a validated prior snapshot, an ordered accepted
fact batch and an exact compatible active policy-generation descriptor, and
SHALL return a new snapshot, desired state, typed diff and reconciliation
proposals without performing I/O. Equal canonical inputs SHALL produce equal
canonical outputs. A semantic no-op SHALL NOT advance snapshot generation, and
a semantic change SHALL advance it exactly once while binding the output to the
bundle and root/user policy generations used.

#### Scenario: Identical facts are replayed offline

- **WHEN** the same prior checkpoint, accepted fact sequence and policy generations are reduced repeatedly
- **THEN** canonical snapshot, desired-state, diff and proposal digests are identical

#### Scenario: A fact changes no effective component state

- **WHEN** a newly accepted complete fact is semantically equivalent to the current component state
- **THEN** the snapshot generation remains unchanged

#### Scenario: Active policy generation changes

- **WHEN** the policy bundle or owning domain generation differs from the generation bound to a prior reduction
- **THEN** Hexroute performs a fresh reduction before publishing current desired state
- **AND** proposals from the prior policy generation are stale

### Requirement: Fail-closed policy and observation uncertainty

Observations SHALL remain available when no compatible active policy can be
validated, but desired state, diff and proposals SHALL be marked unauthorized.
Missing, conflicting, stale or unreconstructible observations SHALL remain
explicit and SHALL NOT be converted into guessed healthy state.

#### Scenario: No compatible policy is active

- **WHEN** startup revalidation finds no compatible active policy generation
- **THEN** Hexroute publishes observed component state
- **AND** it produces no authorized desired state or reconciliation proposal

#### Scenario: Authoritative collector is unavailable

- **WHEN** a component owner stops publishing beyond its freshness deadline
- **THEN** the component becomes stale
- **AND** the reducer does not substitute corroborating evidence as authoritative state

### Requirement: Typed desired state and reconciliation diff

Desired state SHALL be derived only from the installed safety envelope and the
exact active effective policy. The diff SHALL classify each component as
`converged`, `missing`, `unexpected`, `divergent`, `stale`, `unknown`,
`conflict` or `grandfathered_noncompliant` with a bounded reason. It SHALL NOT
embed credentials, executable commands or arbitrary filesystem paths.

#### Scenario: Observed route differs from policy

- **WHEN** an authoritative scoped-route fact does not match the active desired state
- **THEN** the diff classifies the route as `divergent` with its owning domain and bounded reason
- **AND** reduction does not change the route

#### Scenario: Established state is no longer authorized

- **WHEN** a new policy generation excludes an already observed established state
- **THEN** the diff classifies it as `grandfathered_noncompliant`
- **AND** no implicit disconnect is produced

### Requirement: Immutable non-executable reconciliation proposals

A reconciliation proposal SHALL bind the snapshot generation, bundle and
owning-domain policy generation, target identity, proposal class and canonical
diff digest. It SHALL be immutable and digest-addressed, SHALL contain no
command, argument, endpoint, route selector, path, process detail or credential
reference, and SHALL NOT be accepted by any IPC operation as an executable
action or action lease.

#### Scenario: Divergence produces a proposal

- **WHEN** active policy and fresh authoritative observations produce a reconcilable typed diff
- **THEN** Hexroute emits a generation-bound proposal for the owning domain
- **AND** no process, route, DNS, VPN or credential API is invoked

#### Scenario: Proposal is submitted as an action

- **WHEN** a caller sends a proposal digest or body to an existing daemon IPC endpoint for execution
- **THEN** the request is rejected as unsupported
- **AND** no action lease or mutation is created

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

### Requirement: The observation loop aims at a period

The runtime SHALL schedule each observation relative to when the previous one
began rather than to when its work finished, so that a cycle which overran does
not push the next observation out by however long it took. A cycle that has
already consumed its period SHALL begin the next observation immediately rather
than waiting a further period.

This is not about speed. The interval between observations is the quantity the
tunnel decision compares against a wake threshold, and a loop that adds its own
work to that interval reports a gap that nothing outside the runtime caused.
Measured on 2026-09-12: a fold costing 32.4 seconds turned a sixty-one second
period into a ninety-three second gap, which crossed a ninety second threshold.

#### Scenario: A cycle takes longer than its period

- **WHEN** an observation and everything it writes take longer than the configured interval
- **THEN** the next observation begins immediately rather than after a further interval

#### Scenario: A cycle finishes early

- **WHEN** a cycle finishes well inside its period
- **THEN** the next observation begins one period after the previous one began, not one period after this one ended

### Requirement: Observe-only coexistence and qualification

This state machine SHALL have no route, DNS, process, tunnel, Pritunl, AdGuard,
Keychain or credential mutation path. Before the change can be considered
qualified, shadow evidence SHALL cover 72 eligible hours, two sleep/wake
cycles, one reboot and injected duplicate, reorder, sequence-gap,
collector-loss, checkpoint-corruption and policy-generation-change cases while
Twilight remains the production owner. Qualification results SHALL be canonical
hash-linked records bound to their source checkpoint, snapshot, diff, proposal
and fault-trace digests. Completion SHALL be derived by replay of a durable,
gap-free evidence chain; aggregate flags or probabilistic confidence SHALL NOT
complete the gate.

#### Scenario: Reducer or proposal generation fails

- **WHEN** the state machine rejects input or cannot generate current output
- **THEN** existing Twilight, AdGuard and established connectivity remain unchanged
- **AND** the failure is represented as bounded local diagnostic state

#### Scenario: Shadow qualification finds unexplained divergence

- **WHEN** a generated snapshot or proposal cannot be reproduced from retained facts and policy generation
- **THEN** qualification fails
- **AND** no later mutation change may use that evidence as a passing gate

#### Scenario: Qualification provenance is incomplete

- **WHEN** a qualification record or source digest is missing, reordered, rewritten or belongs to a different qualification session or policy generation
- **THEN** qualification replay fails and the gate remains incomplete
- **AND** an aggregate pass result cannot replace the missing evidence

### Requirement: A component fact is judged against its own configured expectation

A fact about a configured component SHALL be computed against the expectation
that component's configuration sets, and not against one expectation chosen for
every part of it. Where a configuration assigns parts of a component to
different places, a part SHALL be judged against the place its own
configuration asks for.

A part the configuration does not ask for SHALL NOT be counted as present or as
diverging. It is outside the question, and counting it answers a question nobody
asked.

The lifecycle a fact reports SHALL be reachable for every configuration the
runtime accepts. A fact whose ready condition cannot hold for a valid
configuration reports the same value for the life of that configuration and
therefore reports nothing: measured 2026-10-08, `scoped_routes` read `degraded`
with `configured 21, conflicting 14, installed 7` while every other component
read `ready`, and the 14 were exactly the routes whose role asks for a link
other than the managed tunnel — correct placement, counted as conflict, by a
ready condition requiring all 21 on one link.

A quantity SHALL answer one question. A part the configuration asks for and the
host lacks, and a part the host has in the wrong place, are different states
with different remedies, and SHALL be counted separately rather than summed
under one name.

The rule that decides where a part belongs SHALL have one statement. A fact
SHALL be derived from the output of that rule rather than from a second
statement of it beside the collector.

#### Scenario: A component's parts belong in different places

- **WHEN** a configuration assigns parts of one component to several places
- **THEN** each part is judged against the place its own configuration names
- **AND** a part correctly in a place other than the first is not counted as diverging

#### Scenario: A part the configuration does not ask for

- **WHEN** the configuration asks for no such part under the current conditions
- **THEN** it is counted neither as present nor as diverging

#### Scenario: A part is asked for and absent

- **WHEN** the configuration asks for a part the host does not have
- **THEN** the fact counts it apart from a part the host has in the wrong place

#### Scenario: Ready is reachable

- **WHEN** every part the configuration asks for is where it asks for it
- **THEN** the component reads ready, whatever places the configuration spread its parts across

#### Scenario: The deciding rule is not restated

- **WHEN** a fact needs to know where a part belongs
- **THEN** it takes the answer from the one rule that decides it, so the two cannot disagree

### Requirement: What the read model publishes is explained where it is owned

Every value and every quantity the read model publishes SHALL be explained in
the reference in the place that owns it: a component's payload under that
component, and a vocabulary under the section that vocabulary belongs to. A
reader meeting a word in a status answer SHALL be able to tell which of its
meanings is in play.

A word SHALL be explained once per place that uses it. The same word carries
different subjects in different places — `configured` counts declared routes in
one payload, configured transports in another and configured relays in a third;
`ready` and `degraded` are both transport counts and component states;
`missing` is both a scoped-routes quantity and a diff classification; `none` is
a diff reason and a value of six payload classes — and one explanation cannot
serve all of them.

A gate holding this SHALL be keyed on the place, not on the word. A check that
accepts the word anywhere in the document can be satisfied by a row belonging to
another vocabulary, which means it cannot notice the explanation it exists to
require being removed.

Measured 2026-10-09, twice. A flat check reported `TransportsPayload` fully
explained because `configured` matched a row about routes and `ready` and
`degraded` matched rows about component states. And the row explaining the
component state `degraded` was deleted from its own section while the gate still
passed, because the word survives where it counts transports. A gate that passes
for the wrong reason is worse than the absent gate it replaces, because it turns
a missing guarantee into a false one.

A gate SHALL refuse rather than widen when the place it is keyed to is absent,
and SHALL refuse a value explained in that place which the code no longer
declares, so the reference cannot describe a vocabulary that has been narrowed.

#### Scenario: A payload gains a quantity

- **WHEN** a component payload gains a field and the reference does not explain it under that component
- **THEN** the gate refuses and names the payload and the field

#### Scenario: A vocabulary gains a value

- **WHEN** a published vocabulary gains a value and the section that owns it does not explain it
- **THEN** the gate refuses and names the vocabulary and the value

#### Scenario: An explanation is removed

- **WHEN** the row explaining a value is deleted from the place that owns it, and the word remains elsewhere in the document
- **THEN** the gate refuses, which is what a check keyed on the word cannot do

#### Scenario: The place is missing

- **WHEN** the section a check is keyed to is absent from the reference
- **THEN** the check refuses rather than searching the whole document

#### Scenario: A word is explained for another payload

- **WHEN** a field's name is explained under a different component, or as a state, a reason or a classification
- **THEN** the gate still refuses, because the explanation is about another subject

#### Scenario: A payload loses a quantity

- **WHEN** a field leaves a payload and its explanation stays
- **THEN** the gate refuses, so the reference cannot describe a quantity nothing reports

#### Scenario: A vocabulary is narrowed

- **WHEN** a value leaves a published vocabulary and its explanation stays in the place that owned it
- **THEN** the gate refuses, for the same reason and in the same way

#### Scenario: Every payload is explained

- **WHEN** every field of every payload is explained under its own component
- **THEN** the gate passes, and says how many payloads and fields it checked

#### Scenario: Every vocabulary is explained where it is owned

- **WHEN** every value of every published vocabulary is explained in the section that owns it
- **THEN** the gate passes, and says how many sections and values it checked
