# Cloud API Runtime

`hexroute-ingest api` is the long-running telemetry-only API mode. It validates
all configuration and database role memberships before opening its listener.
It never exposes a node, route, process, provider or credential control
endpoint.

## Required Configuration

The process reads these runtime environment variables:

| Variable | Purpose |
| --- | --- |
| `HEXROUTE_PUBLIC_ORIGIN` | Exact final HTTPS dashboard origin |
| `HEXROUTE_WEBAUTHN_RP_ID` | DNS name equal to the origin hostname |
| `HEXROUTE_BOOTSTRAP_SECRET` | Initial passkey bootstrap value, at least 32 bytes |
| `HEXROUTE_INGEST_DATABASE_URL` | Login that is exclusively a member of `hexroute_ingest` |
| `HEXROUTE_DASHBOARD_DATABASE_URL` | Login that is exclusively a member of `hexroute_dashboard` |
| `HEXROUTE_AUTH_DATABASE_URL` | Login that is exclusively a member of `hexroute_dashboard_auth` |
| `HEXROUTE_WORKER_NAME` | Readiness heartbeat name; defaults to `primary` |
| `HEXROUTE_HTTP_ADDR` | Listen address; defaults to `:8080` |
| `PORT` | Port-only fallback when `HEXROUTE_HTTP_ADDR` is absent |

The three database URLs must identify distinct login users. Startup also asks
PostgreSQL to prove that each login belongs to exactly its expected group role
and none of the other Hexroute application roles. Configuration and dependency
errors are logged only as fixed reason codes; URL and bootstrap values are
never included.

## HTTP Surface

| Method and path | Behavior |
| --- | --- |
| `GET /livez` | Bounded process liveness |
| `GET /readyz` | PostgreSQL plus fresh worker heartbeat readiness |
| `POST /v1/ingest/batches` | Signed bounded telemetry ingestion |
| `GET /`, `/login`, `/assets/*` | Server-rendered read-only dashboard |
| `POST /auth/*` | Origin-bound WebAuthn ceremonies and logout |

Every other path is not found. Requests with a `Host` different from the final
configured origin are rejected before routing.

### Signed Batch Wire Format

The ingest body is the canonical gzip batch with:

```text
Content-Type: application/vnd.hexroute.ingest-batch+gzip
X-Hexroute-Signed-Envelope: BASE64URL_NO_PADDING(JSON)
```

The decoded header is the strict `hexroute.signed-request.v1` envelope and
signature. The compressed body is capped at 1 MiB. Success returns the strict
`hexroute.ingest-ack.v1` JSON acknowledgement. Rejections and dependency
failures return fixed JSON status values without reflecting request content or
database details. Redirects are not followed by the provided client transport,
and cleartext HTTP is accepted only for loopback tests.

On `SIGTERM`, API mode stops accepting work, waits at most ten seconds for
in-flight requests and closes all three pools.
