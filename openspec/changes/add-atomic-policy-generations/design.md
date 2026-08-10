## Context

Hexroute currently has typed root and user daemons, peer-authenticated IPC,
monotonic lifecycle state generations, an operator resume guard, bounded local
journals and telemetry-only cloud ingestion. Root and user configuration is
validated independently at daemon startup. There is no separately signed
effective policy snapshot, no atomic activation across privilege domains and no
authorization object that binds a mutation to both policy and state.

That is acceptable while Hexroute remains observe-only, but not as a foundation
for later route, process or Pritunl authority. A malformed or partially applied
configuration must never broaden recovery authority, and a policy change must
not invalidate the working connectivity that lets the operator reach Codex.
Twilight therefore remains the production owner throughout this change.

The design borrows the effective-snapshot, ambiguity-rejection and
generation-pinned authorization ideas described by NVIDIA OpenShell's
[security policy architecture](https://github.com/NVIDIA/OpenShell/blob/736e431d454c7de8a71e0fcdd3221ad6f9a552cb/architecture/security-policy.md),
[ambiguity validator](https://github.com/NVIDIA/OpenShell/blob/736e431d454c7de8a71e0fcdd3221ad6f9a552cb/crates/openshell-policy/src/ambiguity.rs)
and [relay generation guard](https://github.com/NVIDIA/OpenShell/blob/736e431d454c7de8a71e0fcdd3221ad6f9a552cb/crates/openshell-supervisor-network/src/proxy/relay.rs).
Hexroute implements its own narrower Go model and has no OpenShell runtime
dependency.

## Goals / Non-Goals

**Goals:**

- Compile all local authorization sources into one deterministic effective
  snapshot and reject ambiguity before activation.
- Authenticate policy authorship with a user-presence-gated Ed25519 key while
  keeping the private key outside both daemons.
- Activate root and user payloads as one recoverable transaction without
  crossing the root-network/user-Keychain privilege boundary.
- Bind authorized actions to exact policy, state, target and plan generations,
  and make replay or stale execution impossible.
- Keep the last valid policy and existing data plane available through invalid
  candidates, cloud loss, daemon crashes, sleep, wake and reboot.
- Prove the model in shadow mode and then enforce only `operator_resume`.

**Non-Goals:**

- Taking production ownership from Twilight or changing AdGuard, routes,
  sing-box, Pritunl processes, credentials or live provider infrastructure.
- Giving the cloud service any local configuration or mutation authority.
- Implementing L7, GraphQL, JSON-RPC or MCP selectors.
- Automatically learning or activating policy from observations or Codex.
- Importing OpenShell, OPA, a Rust policy crate or a general-purpose solver.
- Enabling full active recovery control in this change.

## Decisions

### A separate unprivileged compiler owns composition and signing

`hexroute-policy` is a separate operator process. It reads strict operator YAML,
the compiled safety baseline and the disjoint root and user policy sources,
then emits canonical JSON for the manifest and each domain payload. The YAML
decoder rejects duplicate keys, anchors, aliases and unknown fields. The root
and user daemons never parse YAML, compose sources or hold the signing key.

The source precedence is deliberately not last-writer-wins:

1. A compiled safety baseline defines a non-overridable authorization envelope.
2. Root and user sources own disjoint namespaces inside that envelope.
3. An operator lease may activate only a predeclared capability and narrows it
   by intersection.
4. A compiled deny always wins.
5. Any remaining semantic conflict rejects the complete candidate.

The operator Ed25519 private key lives in the user's macOS Keychain and requires
user presence, normally Touch ID, for a policy signature. The static daemon
configuration pins the public-key fingerprint. A trust-key change is a static
installation and guarded restart, not a dynamic policy update. Go standard
library Ed25519 and SHA-256 are sufficient; a general policy runtime would
increase the privileged dependency and attack surface without helping the
bounded selector model.

Biometric protection uses the macOS Data Protection Keychain rather than the
legacy file-based Keychain ACL model. Provisioning, public-key export,
fixed-challenge verification and signing run in the same app-like,
Developer-signed `hexroute-policy` host whose provisioning profile authorizes
its application identifier. Unsigned, ad-hoc, `go run` and standalone test
binaries fail closed on missing entitlements. Public source contains no Team ID,
application identifier value, provisioning profile, generated public key or
signer fingerprint.

### Public source contains policy contracts, not live policy

The public Hexroute repository contains the policy schema, compiler and verifier
code, OpenSpec requirements, reserved synthetic examples and secret-canary test
fixtures. It does not contain operator policy for a real machine, compiled or
signed bundles, signer fingerprints, live selectors or endpoints, profile and
credential references, activation receipts or deployment evidence.

Live YAML and generated bundles are mode-private local operational artifacts and
are excluded from Git. The private infrastructure repository may carry only
redacted digests, release references and private deployment workflow; actual
secret values remain in Keychain or the designated provider secret store. Git
ignore rules, repository tests and secret canaries fail before live policy
material can enter the public tree. This separation keeps the public design
auditable without publishing a deployable authorization snapshot.

### Canonical effective content defines generation identity

The compiler applies deterministic ordering and RFC 8785 JSON canonicalization.
The signed manifest includes `policy_schema`, `compiler_version`,
`compiler_digest`, `bundle_generation`, `parent_bundle_generation`, root and
user domain generations and payload hashes, `static_digest`, UTC validity
bounds, authorization metadata and the pinned signer identity.

Equivalent effective content has the same digest. Reordering YAML, changing
comments or recompiling with no semantic authorization change cannot create a
generation. A semantic no-op commit is rejected. A generation advances only
when effective authorization metadata, including expiry or trust identity,
changes.

There are three monotonic counters:

- `bundle_generation` identifies the cross-domain decision.
- `root_policy_generation` advances only when the root payload changes.
- `user_policy_generation` advances only when the user payload changes.

Every action binds the bundle generation and its owning domain generation.
Cross-domain actions are permitted only when both daemons report the same
bundle. A domain mismatch blocks new mutations but does not stop existing data
plane connections.

### Static authority is separated from dynamic policy

Static configuration owns role and domain identity, UID/GID, socket and launchd
labels, executable identities, fixed storage roots, credential ownership, the
action-type allowlist, protected namespaces, signer fingerprint and supported
schema range. These values define the process sandbox and cannot change live.

Dynamic policy may define bounded route or ingress selectors, probes and TLS
requirements, thresholds, budgets, backoff and cooldown, Pritunl profile
references and OTP timing, readiness dependencies, and operator capability
leases. Most of these types are modeled in this change but remain unauthorized
for active mutation. Only `operator_resume` is admitted by the first rollout.

If a candidate `static_digest` differs from the daemon's installed static
configuration, prepare returns `restart_required` and cannot activate it. Binary
rollout and static installation are separate from policy activation. Unknown
fields, unsupported schema versions and default downgrades are rejected; schema
migration occurs in the compiler before signing.

### Conflicts are rejected without specificity precedence

The compiler builds typed indexes for host, port, path, route/CIDR, action and
credential-reference selectors. Overlap with different role, path, TLS,
protocol, signing or action semantics rejects the candidate. Wildcard and
concrete selectors do not implicitly override one another. Exact selectors
with identical semantics are deduplicated. Operator leases can only narrow the
compiled set by intersection, and compiled denies are never overridden.

This is intentionally stricter than a specificity algorithm. Explicit conflict
rejection is easier to audit and prevents a future broad rule from silently
changing the meaning of a narrow safety rule. L7 selector families can be added
in separate changes only with their own ambiguity tests.

### Diff and replay precede the single human approval

Before signing, `hexroute-policy` produces a semantic diff and runs a
deterministic offline evaluator over synthetic invariant fixtures and recent
redacted local observation traces. The report separates newly allowed, newly
denied and changed action plans and highlights every authorization expansion.
Any safety-invariant violation blocks signing.

The Keychain signature is the single human approval. It signs the exact
canonical manifest and payload hashes after diff and replay. Prepare and commit
accept only that digest within a short signed validity window; there is no
second approval phrase. Automatic recovery under a previously activated policy
does not require Touch ID. Runtime observations may produce a redacted draft
proposal, and Codex may edit source YAML, but neither can sign, merge or activate
it automatically.

### Immutable storage and typed IPC form the activation boundary

Root and user policy stores are separate mode-private directories. Candidates
and valid generations are immutable, generation-addressed regular files. Active
pointers are signed JSON records replaced with atomic rename; symlinks and
caller-supplied filesystem paths are rejected. Each domain retains the newest
16 valid generations plus any unresolved prepared generation. Rejected payloads
are removed after retaining only digest, timestamp and a bounded reason. Audit
metadata is bounded to 90 days.

The operator copies the root payload through a fixed, guarded privileged
installation step; the user payload remains in the user domain. IPC adds
versioned `PreparePolicy`, `CommitPolicy` and `AbortPolicy` requests containing
only generation, digest, bounded transaction identity and an allowlisted
`stage`, `activate` or `confirm` commit phase. The daemon derives
the fixed local path and independently validates ownership, mode, regular-file
type, schema, canonical hash, signature, validity window, static digest and
domain semantics. There is no file watcher, `SIGHUP`, arbitrary path or payload
over IPC.

### Two-domain activation converges forward after crashes

Activation follows an explicit transaction:

1. Install immutable candidate files into each domain's staging location.
2. Prepare root and user independently and persist their validated receipts.
3. Verify both receipts refer to the same signed bundle and domain hashes.
4. Stage the signed activation intent durably in both daemons without replacing
   either pointer.
5. Atomically replace each active pointer; an unconfirmed pointer publishes
   `domain_mismatch` and blocks mutation dispatch.
6. Confirm both pointers only after the coordinator receives matching activate
   responses from both domains.

If either prepare fails, both candidates are aborted and the last valid policy
continues. If one daemon commits and the other crashes, the active daemon enters
visible `domain_mismatch`, both domains block new mutations, and existing data
plane traffic remains untouched. On restart the lagging daemon revalidates the
same signed commit and converges forward. Startup derives dynamic generation
state only from fully revalidated signed evidence and repairs a missing active
resolution after a crash immediately following pointer replacement. It does
not automatically roll the
already active daemon back. Reversing the decision requires a new signed
generation.

### Invalid policy fails closed without destroying connectivity

An invalid candidate is rejected atomically and never partially activates. If
a valid generation exists, it remains active. If no valid generation exists,
the daemon enters observe-only `SAFE_MODE` and cannot authorize mutations.
For a daemon with an installed policy store this state is represented as
`none` with `no_valid_generation`. Omitting policy control entirely remains the
explicit pre-enforcement shadow bypass and is not treated as a failed policy.

Each daemon also owns a non-generational `authorization_suspended` overlay. It
may be asserted for active-bundle corruption, signature or digest mismatch,
domain mismatch, clock anomaly or IPC ownership violation. The overlay only
narrows authority: it cancels new mutations and emits a bounded incident while
preserving existing data plane processes and connections. It is removed only
after local revalidation. A daemon cannot invent a replacement policy; even an
emergency revoke is a valid signed deny generation. The built-in observe-only
safe mode needs no external signature.

The overlay exposes only `corruption`, `invalid_signature`, `digest_mismatch`,
`domain_mismatch`, `clock_anomaly` or `ipc_ownership`, plus the first assertion
time. It does not replace the active lifecycle state or generation identity.

Cloud availability is irrelevant to activation and local policy evaluation.
The cloud receives telemetry but can neither supply a candidate nor request a
commit, rollback or local action.

### One-time action leases bind authorization to execution

The owning daemon issues an action lease only after evaluating the active
policy. A lease includes `action_id`, bundle generation, domain generation,
control-state generation, exact target, immutable plan digest, issue and expiry
times, boot ID and a nonce. It is short-lived, one-time and durably recorded as
committed, aborted or expired. Replay, expiry, boot mismatch or any generation
change rejects execution.

Issuance defaults to a 30-second TTL and rejects any TTL above five minutes.
The domain-local protected policy store accepts a pending lease only against a
confirmed active pointer with matching bundle and domain generations. It claims
the nonce durably before writing the immutable lease, so an interrupted write
may conservatively burn a nonce but can never leave an unclaimed executable
lease. A resolved lease, nonce claim, optional execution claim and durable
outcome remain an immutable replay-evidence unit. Unresolved claimed leases
remain fail-closed for explicit recovery.

Before the first mutation step, the executor durably claims the lease with a
fresh execution-attempt ID bound to the action, nonce and boot ID. The same
attempt must revalidate that immutable claim immediately before every mutation
step and commit. A different process or post-crash executor cannot resume a
claimed lease; it fails closed until the explicit verified recovery path
resolves the transaction. This closes the crash window between a mutation and
outcome persistence without treating an uncertain action as safe to repeat.

The executor also rechecks policy and control-state generations immediately
before each mutation step and before commit. A multi-step action has an ordered
plan, per-step verification and an inverse plan. On staleness or failure it
rolls back only the Hexroute-owned subset that this transaction applied;
foreign or ambiguous state is never changed. Failed rollback places that
target in `SAFE_MODE`, blocks further actions and emits a critical incident.

For `operator_resume`, the policy handler issues the durable action lease only
after revalidating the confirmed active payload and matching the exact domain,
target, plan digest and original control-state generation. Its authorization
source repeats active-policy validation before every step and commit. The
source retains the original control-state generation as the authorization
origin while fresh runtime observations prove the executor's own expected state
transition; a changed bundle or domain generation therefore invalidates the
lease without mistaking the executor's own monotonic state advance for foreign
staleness.

The ordered plan is a private immutable value built from bounded typed steps.
Its canonical digest binds target, sequence, operation kind, input digest,
before/applied state digests and inverse kind/input/restored-state digests. An
inverse may restore the prior semantic state under a newer generation instead
of decrementing a monotonic control-state counter. Execution records
are value-style append-only ledgers bound to the lease action ID and execution
attempt ID. Rollback selection walks only the applied prefix in reverse order
and emits an inverse operation only when a fresh observation proves the exact
transaction still owns the exact applied-state digest. Missing, available,
foreign, ambiguous or changed state is skipped without exposing a mutation
primitive, and ownership is checked again immediately before each inverse.
The initial allowlist contains only state-only `operator_resume` with
`restore_control_snapshot`; process, route, tunnel and credential operations
are not represented.

The runner is one-shot per execution guard. It obtains a fresh authorization
sample and durable lease check, then verifies ownership and before-state
immediately before each apply. Every successful apply must be observed as the
exact transaction-owned applied digest before the ordered ledger advances.
Failure uses a bounded cancellation-independent compensation context, selects
only the verified reverse suffix and rechecks ownership immediately before and
after each inverse. An uncertain apply, skipped rollback state or inverse
failure enters only the plan target into `SAFE_MODE` and emits the bounded
critical code `action_rollback_failed`; no raw runtime error or foreign owner is
included in the incident.

For this change the only executable plan is the existing `operator_resume`
state transition. It clears an exhausted recovery budget into `DEGRADED` but
does not restart, reconnect or change network state. Generation changes cancel
queued actions and close temporary helpers or credential leases, but they do
not disconnect established Pritunl or sing-box sessions. A later intentional
drain requires an explicit transition plan in a separate change.

The active executor is an isolated state-only package. It can call only the
target snapshot's resume, semantic compensation and safe-mode operations. If a
post-apply authorization or commit check fails, compensation restores
`SAFE_MODE` semantics under a generation newer than the applied generation;
the control generation never moves backward. Import-boundary tests reject
command, route, Pritunl, sing-box, credential, process and network dependencies
from both the resume plan and executor.

### Wall-clock validity and monotonic execution serve different purposes

The signed bundle carries UTC `issued_at`, `not_before` and `expires_at` values.
Policy validity uses the half-open interval `[not_before, expires_at)`. After the
first trusted runtime sample, the daemon compares wall-clock movement with its
continuous monotonic clock and suspends authorization when they diverge by more
than two minutes or monotonic time moves backward. Action leases use only that
continuous monotonic clock plus a boot ID for execution timing; wall timestamps
remain signed audit metadata. Sleep counts toward TTL and reboot invalidates
unfinished leases. An active policy can survive reboot only after its signature,
digest, static binding and validity are rechecked.

The production source is explicitly sleep-aware: Darwin uses
`CLOCK_MONOTONIC`, and Linux uses `CLOCK_BOOTTIME`. A process-relative
`time.Since` source is not acceptable because its Darwin runtime clock excludes
system sleep and would misclassify a normal lid cycle as `clock_anomaly`.

This avoids treating a wall clock as an execution timer while still making
signed policy expiry portable and auditable.

### Existing sessions are grandfathered, not silently terminated

If a new policy no longer authorizes an existing data plane state, Hexroute
marks it `grandfathered_noncompliant`. It cannot start new recovery or actions
under the old authorization. Reconciliation or draining requires an exact plan
under a new lease. Passing `reconcile_by` raises an incident but does not cause
an implicit disconnect. A hard policy violation suspends new mutations; it does
not create a hidden stop command for AdGuard, Twilight or VPN processes.

The policy handler reports this as a separate generation-bound existing-state
overlay rather than changing policy lifecycle state. The bounded report carries
only domain, policy generations and UTC `reported_at`, `reconcile_by` and
optional `incident_at` timestamps. It contains no target, endpoint, command or
action. Detailed per-component reconciliation remains owned by the separate
observable connectivity state-machine change.

### Observability is redacted and cloud authority remains one-way

Local journals and telemetry may expose generation numbers and digests,
`prepared`, `active`, `rejected`, `restart_required`, `domain_mismatch` and
`authorization_suspended`, a bounded allowlisted reason and activation time.
They never expose selectors, endpoints, source paths, operator leases,
credential references, PINs, TOTP data or transport secrets. Cloud loss queues
eligible bounded events locally and cannot block prepare, commit, resume or
safe-mode behavior.

### Rollout starts with shadow evaluation and one bounded capability

The compiler, stores and daemons first run shadow validation while current
behavior remains authoritative. The gate is 72 continuous eligible hours, at
least two sleep/wake cycles, one reboot and injected invalid signature,
selector conflict, stale generation and crash-between-domain-commits cases,
with no unexplained safety mismatch.

After the gate, policy enforcement is enabled only for `operator_resume`.
Pritunl reconnect, route mutation, sing-box process control, credentials and
ingress changes each require a later grill, OpenSpec change, mutation tests and
transactional cutover. Twilight continues to own the production data plane.

The enforcement API accepts a typed qualification gate whose completion bit is
private and can be produced only by validating all required evidence fields; a
zero or incomplete gate fails closed. The current daemon wiring does not call
the enforcement API, so runtime behavior remains shadow-only until the durable
qualification recorder API and its user-owned operational agent produce and
validate real evidence. This prevents implementation of the bounded executor
from being mistaken for a live activation or from being enabled by a
configuration boolean.

### Qualification evidence is provenance-bound, not asserted

The durable qualification recorder writes a canonical append-only chain of
strict `QualificationEvidenceRecord` values. Every record carries a schema and
record identity, the prior record digest, one allowlisted evidence kind, boot
and qualification-session identities, exact bundle/domain generations and
manifest digest, bounded source-event references and digests, UTC observation
time plus source monotonic time where applicable, a typed result/reason and the
canonical record digest. Reordering, removing, rewriting or joining records
from a different boot, session or policy generation breaks validation.

The typed qualification gate is derived only by replaying a durable, gap-free
and hash-valid chain that covers all required eligible time, lifecycle events,
fault injections and safety comparisons. Duration counters and pass/fail flags
are projections of that chain, never caller-supplied authorization evidence.
Unsigned summaries, probabilistic confidence and a generic polymorphic evidence
record cannot enable enforcement. Evidence kinds may share a minimal provenance
header, but keep their own strict payloads so an approval result, connectivity
observation and recovery outcome cannot be substituted for one another.

The operational recorder is a disjoint user LaunchAgent and CLI. It has no root,
process-mutation, route, DNS, network, TLS, credential or policy-activation
authority. It
reads only the redacted typed policy status exposed by the existing authenticated
root and user Unix sockets, writes a private user-owned session root, and records
bounded source artifacts before referring to their event IDs and digests. The
agent never reads the root policy store or raw policy payloads.

A session is initialized from one exact active root/user binding. A binding
change, authorization suspension, policy-status disagreement, invalid source,
hash-chain failure or sampling gap closes that session as ineligible; elapsed
wall time is never carried into another session. Periodic samples append exact
contiguous eligible windows using a boot-scoped monotonic source. Agent relaunch
may resume only after state, chain, sources, binding and monotonic continuity are
revalidated. A reboot starts a new boot segment and appends one typed reboot
boundary without joining unrelated sessions.

Sleep/wake evidence is deliberately operator-armed before the lid closes. On
wake, the agent accepts the cycle only when the persisted arm, boot identity,
continuous-clock interval, current full-wake state and unchanged policy binding
all validate. An armed cycle remains pending through macOS dark wakes and a
pre-sleep sampling race; those samples do not clear the explicit arm. The arm
remains valid for at most 24 hours and is consumed only by a validated full
wake. Neither pending dark wake nor pre-sleep samples create sleep/wake
evidence. A scheduler delay without a valid arm, agent outage or
unarmed lid cycle does not count as sleep/wake. Controlled fault-injection evidence is imported only from bounded
typed source results whose event identity and digest are persisted before the
qualification record. Status is a replay projection (`collecting`, `invalid` or
`complete`), never an editable completion flag.

If a sleep arm is rejected while the session is invalidated, the state retains
that arm only as forensic metadata. Invalid lifecycle state prevents the arm
from authorizing or counting any later wake.

### Executor capability boundaries are repository-enforced

Every data-plane executor change must add a repository capability-leak firewall
derived from the installed safety envelope. The gate combines Go dependency
graph/import checks, a typed capability allowlist and a gated live test proving
there is no undeclared direct network fallback, route or DNS ownership escape,
TLS/certificate fallback, credential access or process control. The existing
`operator_resume` executor therefore remains limited to its state-only adapter;
future capabilities fail release until their separately approved boundary is
encoded and tested.

This adapts the fail-closed leak-firewall mechanism reviewed in
[`geiserx/tailscale-rs`](https://github.com/geiserx/tailscale-rs/tree/ec52d873bb78f260ae6b2a71c82c63d51d9fd54c)
as design input only. Hexroute does not copy its token scanner or an IPv4-only
rule: forbidden paths are generated from Hexroute's declared capability and
privilege envelope.

## Risks / Trade-offs

- **[The strict overlap model rejects a useful policy]** -> Require explicit,
  non-overlapping selectors and surface a bounded conflict report; do not add
  implicit precedence as a shortcut.
- **[Root and user commit at different times]** -> Persist prepare receipts and
  the signed commit intent, block new mutations on mismatch and converge the
  lagging domain forward after revalidation.
- **[A signing-key or Keychain failure blocks urgent expansion]** -> Continue the
  last valid generation; built-in safe mode and suspension can narrow authority,
  but no daemon can manufacture expanded policy.
- **[Clock state prevents activation]** -> Keep the existing active generation,
  report the bounded anomaly and require clock correction before retry.
- **[Policy files consume unbounded disk]** -> Retain 16 valid generations,
  unresolved prepare state and 90 days of bounded audit metadata only.
- **[Shadow success is mistaken for production cutover]** -> Scope active
  enforcement to `operator_resume`; keep all data-plane executors disabled and
  Twilight authoritative.
- **[Telemetry leaks infrastructure or credentials]** -> Use allowlisted event
  schemas and secret-canary tests that fail before persistence or upload.
- **[A live policy artifact is accidentally staged]** -> Ignore operational
  policy roots and fail repository checks on signed bundles, fingerprints,
  non-synthetic selectors and credential references.
- **[OpenShell behavior changes upstream]** -> Treat the cited revision as design
  input only and test Hexroute's independent contract.

## Migration Plan

1. Add typed models, strict decoding, canonicalization, conflict validation and
   deterministic evaluation with synthetic fixtures.
2. Add Keychain-backed signing, immutable root/user stores and independent
   verifier packages without changing daemon authority.
3. Add typed policy IPC, durable prepare/commit/abort state and crash-recovery
   convergence in shadow mode.
4. Add action-lease accounting and place the existing `operator_resume` path
   behind shadow authorization while retaining its current generation guard.
5. Run repository checks and fault injection, then install the candidate beside
   Twilight without changing any Twilight label, path, route or process.
6. Accumulate the 72-hour gate, two sleep/wake cycles, one reboot and all
   required injected failures.
7. Enable policy enforcement only for `operator_resume`; verify normal and
   Twilight fallback access plus redacted diagnostics.
8. Roll back, if needed, by disabling the bounded capability or activating a
   newly signed higher generation derived from prior valid content. Uninstall
   only Hexroute candidate artifacts; never modify Twilight or AdGuard.

## Open Questions

None for this change. Exact production recovery capabilities, cutover timing and
private deployment evidence are intentionally deferred to later bounded changes.
