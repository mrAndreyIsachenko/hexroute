## Why

Provider-B qualification needs evidence that distinguishes a reachable socket,
a valid public TLS fallback, a working authenticated VLESS/Reality path and a
fresh heartbeat from the expected instance. Reusable bounded probes are needed
before private infrastructure can qualify or monitor any live ingress without
embedding transport secrets or provider identities in public code.

## What Changes

- Add provider-neutral TCP and TLS-fallback probes with explicit timeouts and
  structured, stable result categories.
- Add an authenticated transport probe that starts a temporary loopback SOCKS
  listener through an installed `sing-box`, exercises one bounded HTTPS target
  and always removes its secret-bearing temporary configuration.
- Add validation for a signed, fresh instance-heartbeat response using an
  expected Ed25519 node key and deployment generation.
- Add a small JSON CLI for operators and automation, with credentials accepted
  only through standard input and generic redacted errors on standard error.
- Add deterministic unit and failure fixtures proving timeout, signature,
  generation, cleanup and secret-redaction behavior.
- Keep live endpoints, credentials, scheduling, provider resources, TUN/route
  changes, qualification policy and automatic failover out of this change.

## Capabilities

### New Capabilities

- `ingress-functional-probes`: Bounded provider-neutral probes for TCP, public
  TLS fallback, authenticated VLESS/Reality transport and signed heartbeat
  authenticity/freshness.

### Modified Capabilities

- None.

## Impact

This public Hexroute change adds a Go probe package, a standalone CLI, synthetic
fixtures, tests and operator documentation. It uses the existing Ed25519
envelope implementation and the existing `golang.org/x/net` dependency; an
installed `sing-box` is required only for the authenticated probe. Private
`hexroute-infra` will pin the published commit and own live targets, public
keys, Keychain retrieval, scheduling and qualification evidence. Twilight,
AdGuard, Pritunl, current DigitalOcean resources and local routes remain
unchanged. Rollout publishes an unused binary; rollback reverts the public
commit before any private adoption.
