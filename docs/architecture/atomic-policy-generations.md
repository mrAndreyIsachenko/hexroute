# Atomic Policy Generations

Hexroute's policy control plane is an independent Go implementation designed
for a narrow macOS reliability domain. It uses ideas documented by NVIDIA OpenShell,
but it does not import, embed or run OpenShell code.

## Architectural Attribution

The design is informed by these OpenShell sources at pinned commit
`736e431d454c7de8a71e0fcdd3221ad6f9a552cb`:

- [security policy architecture](https://github.com/NVIDIA/OpenShell/blob/736e431d454c7de8a71e0fcdd3221ad6f9a552cb/architecture/security-policy.md): collect policy sources into one effective snapshot before activation;
- [ambiguity validator](https://github.com/NVIDIA/OpenShell/blob/736e431d454c7de8a71e0fcdd3221ad6f9a552cb/crates/openshell-policy/src/ambiguity.rs): reject overlapping selectors with incompatible semantics rather than rely on implicit specificity;
- [relay generation guard](https://github.com/NVIDIA/OpenShell/blob/736e431d454c7de8a71e0fcdd3221ad6f9a552cb/crates/openshell-supervisor-network/src/proxy/relay.rs): bind authorization-bearing work to the policy generation that admitted it and invalidate stale work when that generation changes.

These are architectural references, not a source or binary dependency. The
policy compiler, canonical model, conflict indexes, signatures, stores, IPC,
action leases and tests under `internal/` are implemented in Go for Hexroute.
The `policy_cli_boundary_test.sh` dependency gate keeps the compiler offline
and mutation-free, while `policy_cloud_independence_test.sh` keeps local policy
paths independent of the cloud API, PostgreSQL and workers.

## Hexroute Adaptation

Hexroute narrows the model to two privilege domains:

1. an unprivileged compiler builds one complete effective snapshot;
2. canonical manifest and root/user payloads receive separate monotonic domain
   generations under one bundle generation;
3. every selector overlap with incompatible semantics rejects the complete
   candidate before signing;
4. a user-presence Ed25519 approval binds the canonical manifest, both payload
   digests, semantic diff, replay result and validity window;
5. root and user daemons independently prepare, then converge through durable
   stage, activate and confirm records;
6. every executable action lease binds bundle, domain-policy and control-state
   generations, exact target, immutable plan digest, boot ID and nonce;
7. a generation change cancels uncommitted work and credential helpers, while
   established data-plane sessions remain grandfathered until an explicit
   authorized reconciliation plan exists.

That final point intentionally differs from blindly closing established
connections. Hexroute's availability invariant forbids an implicit stop or
disconnect of Twilight, AdGuard, Pritunl or sing-box. A policy generation can
block new mutations immediately, but draining existing state requires a
separate explicit plan and lease.

## Deliberately Excluded Scope

OpenShell models broader sandbox and protocol policy. This Hexroute change does
not implement or depend on:

- dynamic filesystem or process policy;
- a generic network proxy, relay runtime or packet tunnel;
- TLS rewriting, credential signing or request transformation;
- GraphQL, JSON-RPC or MCP parameter selectors;
- a Rust policy crate, OPA, a general-purpose solver or an OpenShell daemon;
- automatic learning, merge, signing, installation or activation of advisor
  output;
- cloud-originated local policy or mutation requests.

Static filesystem/process restrictions remain launchd, ownership, mode,
executable identity and fixed-path configuration applied at installation or
process startup. Dynamic policy is intentionally bounded to compiled Hexroute
capabilities. The first actively enforced capability is only
`operator_resume`, whose plan changes a local control snapshot from `SAFE_MODE`
to `DEGRADED` and has no process, route, tunnel or credential operation.

Future L7 selectors or data-plane authority require separate OpenSpec changes,
new ambiguity rules, capability boundary tests, shadow qualification and an
explicit rollback plan. Existing policy types or OpenShell precedent do not
grant that authority automatically.
