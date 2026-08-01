# Ingress Functional Probes

Hexroute exposes four observation-only ingress probes through
`hexroute-ingress-probe`:

| Probe | Evidence | Stable failure boundary |
| --- | --- | --- |
| `tcp` | A TCP listener accepted a connection | timeout or reachability |
| `tls-fallback` | The public fallback completed normal certificate-verified TLS | timeout or TLS identity |
| `authenticated` | `sing-box` carried an HTTPS canary over VLESS/Reality | dependency, timeout or authenticated transport |
| `heartbeat` | The expected node signed a fresh healthy generation | response, authenticity, freshness, generation or runtime health |

TCP and TLS fallback do not prove authenticated Reality transport. Conversely,
an authenticated canary does not prove that the expected observer process is
fresh. Qualification policy must retain all four signals independently.
See the [provider-B architecture](../architecture/provider-b-ingress.md) for
signal ownership and the lifecycle gates that prevent a probe from enabling
traffic.

## Invocation Contract

The command takes exactly one probe name and reads exactly one strict JSON
request from standard input. Requests are limited to 64 KiB and reject unknown
fields. The command emits one result shaped as:

```json
{
  "schema": "hexroute.ingress-probe-result.v1",
  "probe": "tcp",
  "state": "pass",
  "category": "ok",
  "duration_ms": 12
}
```

The result never includes request values or dependency error text. A passing
probe exits zero; all invalid input and probe failures exit non-zero with the
generic standard-error message `probe failed`.

Requests contain the following fields:

- `tcp`: `endpoint.host`, `endpoint.port`, `timeout_ms`.
- `tls-fallback`: the TCP fields plus `server_name`.
- `authenticated`: endpoint, server name, VLESS user ID, Reality public key and
  short ID, HTTPS canary URL, optional accepted status bounds and timeout.
- `heartbeat`: endpoint, optional literal-loopback SOCKS URL, expected node/key
  IDs, Ed25519 public key, deployment generation, maximum age and timeout.

Private automation owns live values and must stream the request through an
anonymous stdin pipe. It must not persist the authenticated request, place its
fields in command arguments, or log the input. Keychain and provider secret
stores remain outside this public binary.

## Authenticated Process Boundary

The authenticated probe requires an installed, independently pinned
`sing-box`. Hexroute writes one minimal configuration into a mode-0700 temporary
directory with a mode-0600 file. The configuration has one unauthenticated SOCKS
inbound bound to `127.0.0.1` and one VLESS/Reality outbound. There is no TUN,
route command, DNS mutation or direct fallback outbound.

The child receives only `run --disable-color -c <temporary-path>` and its output
is discarded. Hexroute waits for loopback readiness, performs one HTTPS request
through SOCKS, stops the child and removes the directory on every return path.
Probe failure has no restart or mutation authority.

## Signed Heartbeat Contract

The observer response uses schema
`hexroute.ingress-heartbeat-response.v1`. It contains a raw
`hexroute.ingress-heartbeat.v1` body and the existing Hexroute signed envelope.
The body carries node ID, generation, canonical observation time and transport
health. Validation requires:

1. a bounded HTTP 200 response with no unknown fields;
2. an active expected Ed25519 node/key and valid body digest/signature;
3. fresh envelope and body timestamps;
4. exact expected deployment generation and healthy transport state.

The instance observer binds only literal loopback. Private automation may
retrieve `http://127.0.0.1:<port>/v1/heartbeat` through an explicitly supplied
`socks5://127.0.0.1:<port>` proxy created by the authenticated transport
workflow. Plain HTTP without that literal-loopback proxy, hostname aliases such
as `localhost`, remote SOCKS proxies and credential-bearing proxy URLs are
rejected before network access. Direct non-loopback heartbeat endpoints remain
HTTPS-only.

The producer-side environment and release contract is documented in
[Ingress observer runtime](ingress-observer-runtime.md).

This public component does not schedule probes or publish a qualification
report. Live provider inventory, external monitors, Keychain retrieval and
evidence correlation stay in private `hexroute-infra`. Publishing this binary
does not activate provider B, automatic failover or any change to Twilight,
AdGuard, Pritunl or current traffic.
