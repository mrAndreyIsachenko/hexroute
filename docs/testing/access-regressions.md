# Access Regression Fixtures

The fixtures under `testdata/access/v1` encode the minimum paths that every
observe-only comparison and active-control release must preserve:

- normal Codex access remains on its current path;
- Codex fallback installs only verified scoped routes through Twilight;
- ChatGPT Chromium proxying remains application-local;
- GitLab HTTPS can use a Twilight-owned scoped route;
- GitLab SSH remains on the physical path;
- Pritunl reconnect requires verified outer readiness;
- `Active` plus a client address remains connected even if an optional inner
  diagnostic probe fails.

All fixtures use `.example` hostnames. Implementations must reject fixtures
that request a system proxy change or any AdGuard action.
