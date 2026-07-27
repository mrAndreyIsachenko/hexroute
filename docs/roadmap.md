# Hexroute Roadmap

Status date: 2026-07-26.

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
  black-box monitoring. It is not yet a second transport provider.

## Active Change

The active infrastructure change is
`migrate-production-to-dedicated-do-team` in the private repository. It moves
the control plane out of the DigitalOcean Team shared with Twilight by using an
independent green state, tested backup/restore, reversible DNS cutover, a
72-hour soak and allowlisted decommission.

This Team split improves ownership, credentials and billing isolation. It does
not provide a second provider failure domain.

## Ordered Changes

1. Complete the dedicated-Team migration and first measured restore drill.
2. Complete root observe-only soak and resolve every materially divergent
   proposed action without enabling mutations.
3. Run an evidence-based provider-B bake-off and deploy an independent
   VLESS/Reality ingress in a different provider and ASN.
4. Deploy and qualify two-provider Telegram ingress using native MTG, Nginx SNI
   pass-through and functional MTProto health evidence.
5. Add signed configuration and A/B release delivery with local rollback.
6. Cut root tunnel ownership from Twilight to Hexroute transactionally.
7. Cut user Pritunl recovery ownership from the legacy OTP watchdog to
   `hexroute-userd` transactionally.
8. Complete public qualification, supply-chain evidence and legacy cleanup.

Each numbered item after the active migration requires its own grill session
and bounded OpenSpec change. No future item may be bundled into an earlier
cutover merely because supporting code already exists.
