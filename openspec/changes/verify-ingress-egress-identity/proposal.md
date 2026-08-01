## Why

The authenticated ingress probe currently proves VLESS/Reality transport and
an allowed HTTP status, but it cannot prove that a bounded identity endpoint
observed the expected egress address. Provider-B qualification needs that
assertion without exposing the response body or live address in probe output.

## What Changes

- Add an optional SHA-256 assertion for the bounded authenticated canary
  response body.
- Bound the compared response size and preserve the existing redacted result
  envelope for matches and mismatches.
- Add synthetic tests for matching, mismatching, oversized and omitted body
  assertions.
- Keep live expected hashes, endpoints and credentials in private
  `hexroute-infra`; public Hexroute owns only provider-neutral comparison logic.
- Roll out by pinning the accepted public commit in private canary policy.
  Roll back by pinning the preceding probe commit and disabling private canary
  scheduling; neither operation changes production traffic.

## Capabilities

### New Capabilities

- `ingress-egress-identity`: Bounded, redacted authenticated-canary response
  identity verification using an optional expected body digest.

### Modified Capabilities

None.

## Impact

- Public Go request validation, SOCKS HTTP response handling, probe tests and
  operator documentation.
- No provider, route, DNS, TUN, Twilight, AdGuard or Pritunl mutation.
- Twilight remains the active production traffic owner; this remains an
  observation-only qualification primitive.
