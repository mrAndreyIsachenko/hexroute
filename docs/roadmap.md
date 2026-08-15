# Hexroute Roadmap

Status date: 2026-08-15.

## Current Baseline

- Twilight remains the production owner of sing-box, scoped routes and
  Keychain-backed Pritunl recovery.
- Hexroute root and user daemons are installed beside Twilight under disjoint
  labels, paths, sockets and stores. They remain pre-cutover and have no
  production data-plane mutation authority.
- The atomic policy compiler, user-presence signer, immutable root/user stores
  and typed cross-domain activation are implemented. A deny-only initial bundle
  and a higher-deny successor have passed a live local activation and rollback
  safety check while both Codex paths, Twilight and AdGuard remained available;
  private artifacts and evidence remain outside public Git.
- Atomic policy generations and the generation-bound network reconciler are
  synced into baseline specs. They remain pre-cutover and grant no production
  mutation authority.
- Deterministic lifecycle policy, typed peer-authenticated IPC, bounded local
  journals, signed ingestion and redacted diagnostics are implemented and
  covered by synthetic tests.
- The telemetry-only cloud API, worker and migrator implement PostgreSQL-backed
  ingestion, incidents, retention, SLOs, Telegram alerts, incident bundles and
  a passkey-protected read-only dashboard.
- Public reusable Terraform modules exist; live roots and deployment evidence
  remain in the private `hexroute-infra` repository.
- The first cloud foundation is live behind `status.hexroute.app` with external
  black-box monitoring. The control plane, PostgreSQL and private object
  storage are isolated in a dedicated DigitalOcean Team with canonical,
  disjoint production and provider-neutral edge states.
- The first measured logical PostgreSQL and private-object restore drill,
  RPO-zero cutover, 72-hour soak and old shared-Team decommission are complete.
- The cloud foundation is not yet a second transport provider.
- Provider-B reusable Lightsail infrastructure, secret-free bootstrap and four
  independent functional probes are published and documented, but no public
  fact claims a workload is deployed, qualified, inventory-admitted or
  failover-enabled. See
  [`docs/architecture/provider-b-ingress.md`](architecture/provider-b-ingress.md).
- The immediate next gate is the operational acceptance drill in
  [`docs/testing/operational-acceptance.md`](testing/operational-acceptance.md):
  it proves the operator-visible work path before any further runtime or
  cutover step.

## Active Changes

`add-operational-acceptance-drill` is active. It defines and implements the
user-visible operational acceptance drill before further runtime/cutover work.
It is non-mutating and writes only redacted local evidence.

`add-observable-connectivity-state-machine` is planned next. It will normalize
peer, relay, DNS, SSH and session state and drive only observe-only proposed
mutations until its own qualification gates pass. It cannot absorb the current
operational acceptance drill or take ownership from Twilight.

## Ordered Changes

1. Complete and run the operational acceptance drill for the current working
   path, including baseline and recovery evidence.
2. Implement and qualify the observable connectivity state machine without
   enabling production mutations.
3. Complete root observe-only soak and resolve every materially divergent
   proposed action without enabling mutations.
4. Run an evidence-based provider-B bake-off and deploy an independent
   VLESS/Reality ingress in a different provider and ASN.
5. Deploy and qualify two-provider Telegram ingress using native MTG, Nginx SNI
   pass-through and functional MTProto health evidence.
6. Add signed configuration and A/B release delivery with local rollback.
7. Cut root tunnel ownership from Twilight to Hexroute transactionally.
8. Cut user Pritunl recovery ownership from the legacy OTP watchdog to
   `hexroute-userd` transactionally.
9. Complete public qualification, supply-chain evidence and legacy cleanup.

Each numbered item requires its own grill session and bounded OpenSpec change.
No future item may be bundled into an earlier cutover merely because
supporting code already exists.
