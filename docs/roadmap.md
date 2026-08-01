# Hexroute Roadmap

Status date: 2026-08-01.

## Current Baseline

- Twilight remains the production owner of sing-box, scoped routes and
  Keychain-backed Pritunl recovery.
- Hexroute provides typed Go commands for root, user, sentinel, operator and
  cloud roles, but local daemons remain pre-cutover and mutation-disabled.
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

## Active Change

The next infrastructure change is an evidence-based provider-B bake-off and
deployment of an independent VLESS/Reality ingress outside DigitalOcean and
its ASN. It must preserve the current Twilight data path and the dedicated-Team
Hexroute control plane throughout qualification.

## Ordered Changes

1. Complete root observe-only soak and resolve every materially divergent
   proposed action without enabling mutations.
2. Run an evidence-based provider-B bake-off and deploy an independent
   VLESS/Reality ingress in a different provider and ASN.
3. Deploy and qualify two-provider Telegram ingress using native MTG, Nginx SNI
   pass-through and functional MTProto health evidence.
4. Add signed configuration and A/B release delivery with local rollback.
5. Cut root tunnel ownership from Twilight to Hexroute transactionally.
6. Cut user Pritunl recovery ownership from the legacy OTP watchdog to
   `hexroute-userd` transactionally.
7. Complete public qualification, supply-chain evidence and legacy cleanup.

Each numbered item requires its own grill session and bounded OpenSpec change.
No future item may be bundled into an earlier cutover merely because
supporting code already exists.
