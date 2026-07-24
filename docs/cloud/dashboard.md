# Read-Only Dashboard

The Hexroute dashboard is server-rendered and exposes only current operational
state. Its PostgreSQL connection uses `hexroute_dashboard`, which has `SELECT`
on normalized node, incident, deployment, worker, alert and SLO data and no
access to event payloads or passkey credential records. The router defines
only:

- dashboard and login `GET` pages;
- same-origin CSS and JavaScript assets;
- WebAuthn login, registration and logout `POST` ceremonies.

There is no restart, route, deployment, acknowledgement, configuration or
provider-management endpoint. Unknown paths return `404`, and unsupported
methods return `405`.

WebAuthn validation uses `go-webauthn` pinned in `go.mod`. The relying-party ID
and exact HTTPS origin are runtime configuration bound to the final personal
domain. User verification and discoverable credentials are required.
Challenges and authenticated sessions are random, bounded, server-side,
one-process records. Challenge lifetime is five minutes; login lifetime is 12
hours. Browser cookies use the `__Host-` prefix with `Secure`, `HttpOnly`,
`SameSite=Strict` and no domain attribute.

Every auth `POST` requires the exact configured `Origin`. The first passkey can
be added only to an existing enabled principal with a runtime bootstrap token
of at least 32 bytes and only while that principal has no credential. The
bootstrap token is not a login mechanism and stops working after first
registration. Further passkeys require an existing WebAuthn-authenticated
session. A consumed ceremony cannot be replayed.

Passkey writes use a separate `hexroute_dashboard_auth` role. It can read only
principal/passkey records, insert a credential and update the sign counter,
credential state and last-authenticated timestamps. It cannot read incidents,
events or SLOs and cannot change schema. The dashboard role cannot read
passkey records. This keeps rendering and authentication privileges disjoint.

Credential IDs, COSE public keys, transports, counters, AAGUID and WebAuthn
state are stored; private credential keys never leave the authenticator.
Clone-warning login results are rejected. Database and WebAuthn errors are
returned as fixed authentication/unavailable responses without details.

All HTML responses set a restrictive Content Security Policy, HSTS,
`no-store`, `nosniff`, frame denial and no-referrer. Templates escape all
database content. The browser script is served from the same origin and uses
only the standard WebAuthn API.
