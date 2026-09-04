# Hexroute Roadmap

Status date: 2026-09-04.

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
- The observable connectivity read model is qualified and synced into the
  baseline specs. Its shadow evidence chain is complete: 260,921 eligible
  seconds against the 259,200 required, two sleep/wake cycles, one reboot and
  all thirteen mandatory injected faults, with zero divergences and nothing
  left unbound. It runs on the host behind an off-by-default gate. Root adapts its existing network, default-path, scoped-route,
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
- Incident-bundle creation and expiry are scheduled in the maintenance worker.
  A closed incident with linked evidence is bundled once; an incident with
  nothing linked is never selected, and one whose bundle was removed at its
  recorded expiry is not bundled again, so retention is not undone by the pass
  that would restore it. Where bundle storage is unconfigured the pass records
  `cloud_incident_bundle_unconfigured` on every interval, so a deployment that
  was never finished reads differently from one with nothing to bundle. The
  object store it writes through implements two methods and cannot read; its
  SigV4 signing is checked against fourteen of the provider's published test
  vectors at all three stages, not against itself alone.
- Durable local event retention keeps typed events by age and size rather than
  by upload state, with a scheduled review and an annotator that refuses to
  write when attaching commentary would move the digest.
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
  and left reachable from nothing without saying so. Four packages are currently
  recorded as unwired.
- The operational acceptance drill in
  [`docs/testing/operational-acceptance.md`](testing/operational-acceptance.md)
  is complete and archived.

## Active Changes

None. `record-ingress-fleet-purpose` closed on 2026-09-05, recording what each
ingress host is for, correcting the provider-B lifecycle state and rewriting
item 4 below. The three before it closed on 2026-09-03 and 2026-09-04:
`add-observable-connectivity-state-machine`, `add-local-event-archive` and
`add-private-incident-bundles`, with `observe-sentinel-recovery` alongside them.

## Ordered Changes

1. ~~Complete and run the operational acceptance drill for the current working
   path, including baseline and recovery evidence.~~ Done. See the archived
   `add-operational-acceptance-drill` change and
   [`docs/testing/operational-acceptance.md`](testing/operational-acceptance.md).
2. ~~Qualify the observable connectivity state machine without enabling
   production mutations.~~ Done. The evidence chain closed on 2026-09-03 with
   260,921 eligible seconds against the 259,200 required, two sleep/wake
   cycles, one reboot and all thirteen mandatory faults injected. Evidence is
   retained privately.
3. ~~Complete root observe-only soak and resolve every materially divergent
   proposed action without enabling mutations.~~ Done, and closed by that same
   evidence: divergences were zero and nothing was left unbound, so there was
   no divergent proposed action to resolve. The sentinel observes recovery and
   records the plan it would run, with no restarter attached.
4. ~~Run an evidence-based provider-B bake-off and deploy an independent
   VLESS/Reality ingress in a different provider and ASN.~~ The deployment half
   was delivered before this item was read: the provider-B ingress exists, its
   provider and ASN are distinct, and a private workload consumes it. The
   bake-off half cannot be run and was ruled out on evidence — the word appears
   nowhere in this repository except in this line, no criterion or threshold is
   written anywhere, and exactly one provider serves ingress, so there is no
   second candidate to compare against. Standing one up purely to have a
   comparison would pay for an answer nobody needs. What remains is recorded by
   `record-ingress-fleet-purpose`: the fleet's purposes, the corrected lifecycle
   state, and the failure domain that two configured entries share.
5. Deploy and qualify two-provider Telegram ingress using native MTG, Nginx SNI
   pass-through and functional MTProto health evidence.
6. Add signed configuration and A/B release delivery with local rollback.
7. Cut root tunnel ownership from Twilight to Hexroute transactionally.
8. Cut user Pritunl recovery ownership from the legacy OTP watchdog to
   `hexroute-userd` transactionally.
9. Complete public qualification, supply-chain evidence and legacy cleanup.

Item 4 also ruled out two placements that were proposed and withdrawn during its
grill session. Moving the named-country host nearer the operator was recommended
before its purpose was known and would have cost latency to serve a need already
met. Holding a permanent host for the occasional heavily filtered country was
dropped because the need is rare, is met by buying a service locally, and could
not be verified from any vantage point that exists.

Each numbered item requires its own grill session and bounded OpenSpec change.
No future item may be bundled into an earlier cutover merely because
supporting code already exists.

## Debt

Code that exists and no binary contains. Each entry is a claim this repository
has made and not yet kept; the list is enforced by `make check`, so it cannot
grow in silence.

- `credentials`, `pritunlrescue` — held behind the user-domain cutover, item 8.
- `resumeexecutor` — operator resume enforcement.
- `policyadvisor` — redacted policy observability.

`incidentbundle` and `objectstore` left this list on 2026-09-04, when the
maintenance worker began calling bundle creation and expiry: the census fell
from six packages and 1800 unrun lines to four and 638. `reconciler` and `slo`
left it on 2026-08-30, the first when its shadow status became answerable and
the second when its calculation was scheduled. It still compares nothing: correlating its planner output with the
read model's proposals is part of item 2.
