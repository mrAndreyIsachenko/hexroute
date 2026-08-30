# Hexroute Roadmap

Status date: 2026-08-30.

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
  mutation authority. The reconciler is reachable from `hexrouted` only far
  enough to answer what its shadow store holds; it compares nothing against the
  host and cannot, because its dependencies forbid every package that could
  reach one.
- The observable connectivity read model runs on the host behind an off-by-
  default gate. Root adapts its existing network, default-path, scoped-route,
  transport and relay observations into facts; the user daemon publishes what
  only it can see over authenticated IPC; the aggregate reduces both into one
  snapshot and checkpoints it on effective change. DNS has no observer, so that
  component reports `unknown`. Nothing in this path can mutate anything.
- Deterministic lifecycle policy, typed peer-authenticated IPC, bounded local
  journals, signed ingestion and redacted diagnostics are implemented and
  covered by synthetic tests.
- The telemetry-only cloud API, worker and migrator implement PostgreSQL-backed
  ingestion, incidents, retention, Telegram alerts, the redacted connectivity
  projection and a passkey-protected read-only dashboard.
- SLO availability is calculated hourly per node from stored evidence and
  upserted, so the dashboard's SLO section reports measured availability. A
  window whose opening state cannot be established, or whose failed time no
  incident explains, is left uncomputed rather than filled in.
- Incident-bundle creation and expiry are implemented and **not scheduled**:
  nothing creates a bundle, so expiry has nothing to expire, and creation needs
  private object storage configured outside this repository.
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
- Repository checks run in CI on every pull request and on `main`: `make check`
  on macOS, the Go gate on Linux, and `make postgres-test`. A package that no
  binary contains fails `make check`, so code cannot be written, marked done
  and left reachable from nothing without saying so. Six packages are currently
  recorded as unwired.
- The operational acceptance drill in
  [`docs/testing/operational-acceptance.md`](testing/operational-acceptance.md)
  is complete and archived.

## Active Changes

`add-observable-connectivity-state-machine` is active. Sections 1 through 8 are
implemented and running observe-only on the host; sections 9 and 10 — replay
and shadow qualification, then documentation and baseline sync — are open. Its
qualification gate requires 72 eligible hours of shadow evidence, two
sleep/wake cycles, one reboot and every mandatory injected failure. That
evidence is produced by running the host, not by this repository, and it is
retained privately.

`add-local-event-archive` is proposed and unstarted. It would retain typed
events locally by age and size rather than by upload state, so the host can
answer questions about last week. It cannot begin while the connectivity change
is mid-qualification.

## Ordered Changes

1. ~~Complete and run the operational acceptance drill for the current working
   path, including baseline and recovery evidence.~~ Done. See the archived
   `add-operational-acceptance-drill` change and
   [`docs/testing/operational-acceptance.md`](testing/operational-acceptance.md).
2. Qualify the observable connectivity state machine without enabling
   production mutations. Implementation is complete and running observe-only;
   the replay harness, the thirteen fault traces and the shadow comparison
   recorder exist. What remains is the evidence chain itself: 72 eligible
   hours, two sleep/wake cycles, one reboot and every trace injected. That is
   produced by running the host, and its evidence is retained privately.
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

## Debt

Code that exists and no binary contains. Each entry is a claim this repository
has made and not yet kept; the list is enforced by `make check`, so it cannot
grow in silence.

- `incidentbundle` — bundle creation and expiry. Nothing creates a bundle, so
  expiry has nothing to expire; creation needs private object storage
  configured outside this repository.
- `credentials`, `pritunlrescue` — held behind the user-domain cutover, item 8.
- `resumeexecutor` — operator resume enforcement.
- `policyadvisor` — redacted policy observability.

`reconciler` and `slo` left this list on 2026-08-30, the first when its shadow
status became answerable and the second when its calculation was scheduled. It still compares nothing: correlating its planner output with the
read model's proposals is part of item 2.
