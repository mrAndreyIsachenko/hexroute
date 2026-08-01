## Context

The authenticated probe already owns a short-lived, loopback-only `sing-box`
process and a bounded HTTPS request. Private provider qualification can know an
expected egress response, but the public probe currently discards the response
body after checking only the status code.

## Goals / Non-Goals

**Goals:**

- compare an optional expected SHA-256 against a size-bounded response body;
- report only the existing stable authenticated-transport category on mismatch;
- retain the existing stdin, process cleanup and no-network-mutation boundary.

**Non-Goals:**

- store live expected addresses, hashes or target URLs in the public repository;
- return response content or calculated digests;
- schedule probes, read Keychain or qualify a provider;
- alter local routes, DNS, TUN interfaces or production process ownership.

## Decisions

### Optional digest in the existing authenticated request

`expected_body_sha256` is an optional lowercase hexadecimal SHA-256. An empty
value preserves status-only behavior. This keeps one authenticated transport
primitive instead of adding a provider-specific probe kind.

Alternative: expose the response body to private automation. Rejected because
it expands the output contract and can leak identity-service responses.

### Compare a bounded body in memory

The SOCKS fetch reads at most 4097 bytes. More than 4096 bytes is a failure when
a digest assertion is present; otherwise the existing bounded discard behavior
remains. The digest and body never enter result fields or dependency errors.

Alternative: stream an unbounded body into SHA-256. Rejected because canary
targets are external and must not control local resource consumption.

### Preserve observation-only ownership

The public binary continues to own only its child process and private temporary
directory. Private infrastructure owns expected values and scheduling. A
mismatch fails the probe and cannot request local or provider recovery, so
cloud loss leaves Twilight and existing local recovery available.

## Risks / Trade-offs

- [Identity service changes body formatting] -> Private policy pins the exact
  target contract and treats mismatch as bounded failed evidence, not outage
  recovery authority.
- [A response is oversized] -> Fail before hashing beyond the fixed limit.
- [Digest input is malformed] -> Reject before starting `sing-box`.

## Migration Plan

1. Add validation, bounded hashing and synthetic tests.
2. Publish and pin the exact commit in private provider-B canary policy.
3. Roll back by pinning the preceding public commit and disabling private
   scheduling; no client route or production process changes.

## Open Questions

None.
