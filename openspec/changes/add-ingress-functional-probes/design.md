## Context

The provider-B deployment needs four independent observations. A socket can be
reachable while the decoy TLS path, authenticated transport or instance runtime
is broken, so one generic HTTP health check cannot qualify an ingress. The
public repository must define reusable behavior without live addresses,
provider identities or transport material, while private infrastructure owns
secret retrieval, scheduling and evidence correlation.

## Goals / Non-Goals

**Goals:**

- produce bounded, machine-readable results for TCP, TLS fallback,
  authenticated transport and signed heartbeat probes;
- distinguish network, TLS, dependency, authenticated-transport, heartbeat
  authenticity, heartbeat freshness and deployment-generation failures;
- exercise VLESS/Reality through the installed `sing-box` implementation and a
  temporary loopback-only SOCKS listener;
- keep endpoints and all transport material out of arguments, output and logs;
- guarantee process termination and removal of secret-bearing temporary files.

**Non-Goals:**

- implement VLESS, Reality or SOCKS protocol logic in Hexroute;
- create a TUN, change routes/DNS, restart XRay or mutate any provider;
- retrieve live values from Keychain or a cloud secret store;
- schedule probes, correlate qualification evidence or enable failover;
- replace external monitoring or claim that provider B is production-ready.

## Decisions

### One stdin request and one stable result envelope

`hexroute-ingress-probe <kind>` reads one size-bounded, strict JSON request from
standard input. Even non-secret fields use this channel so SNI, UUID and target
details never migrate into process arguments. Standard output contains only a
stable schema, probe kind, pass/fail state, category and elapsed milliseconds;
standard error is generic. Returning raw library errors was rejected because
future dependency messages can include endpoints or configuration values.

### Native TCP and public TLS fallback checks

TCP uses `net.Dialer` with a deadline. TLS fallback uses the standard Go TLS
client, an explicit server name, TLS 1.2 minimum and normal certificate
verification. It proves that an unauthenticated client can negotiate the public
decoy/fallback path; it does not claim authenticated Reality success. Combining
these checks was rejected because it would hide listener-versus-TLS failures.

### Authenticated transport delegates to sing-box

Hexroute renders a minimal `sing-box` configuration containing only a loopback
SOCKS inbound and one VLESS/Reality outbound. It writes that configuration to a
0700 temporary directory and 0600 file, launches `sing-box` with only the file
path in argv, discards child output, waits for loopback readiness and fetches one
bounded HTTPS canary through SOCKS. Context cancellation terminates the child
and deferred cleanup removes the directory. Importing or reimplementing the
transport stack was rejected because protocol behavior and security updates
belong to the proven transport engine.

### Signed heartbeat is a separate trust signal

The observer response contains a raw JSON heartbeat body and the existing
Hexroute signed-envelope type. The probe validates response size and schema,
Ed25519 authenticity against the expected active node/key, envelope and body
freshness, exact deployment generation and healthy runtime state. TLS alone was
rejected as instance identity because a fallback or intermediary can terminate
TLS without proving the expected observer is alive.

### Probe code has no mutation authority

The standalone binary has no dependency on local daemon IPC, Keychain, route
commands or cloud credentials. Private automation supplies input over stdin and
owns any secret retrieval. A failed probe reports evidence only: it cannot
restart XRay, alter local recovery or request a cloud/local mutation.

## Risks / Trade-offs

- [Temporary configuration contains credentials] -> private directory/file
  modes, no child output, no secret-bearing argv and unconditional cleanup.
- [Process is killed between file creation and cleanup] -> private scheduling
  uses an OS-private temporary root; stale-file scanning remains a private
  operational control and credentials remain rotation-capable.
- [Loopback port selection races] -> readiness is bounded and a bind failure is
  reported as authenticated-transport failure; callers retry on their schedule.
- [TLS fallback is healthy while Reality is broken] -> results remain separate
  and qualification requires both fallback and authenticated probes.
- [A signed but old heartbeat is replayed] -> envelope and body timestamps must
  both fall within the configured bounded freshness window.
- [Cloud or provider B is unavailable] -> probes fail without touching Twilight,
  AdGuard, Pritunl or either existing Codex path.

## Migration Plan

1. Publish the unused public package, CLI and synthetic tests.
2. Pin that exact public commit in private policy without running live probes.
3. Later private tasks supply Keychain-backed requests and external scheduling,
   then correlate all four signals before qualification.
4. Before private adoption, rollback reverts the public commit. After adoption,
   rollback pins the preceding public commit and removes private scheduling;
   neither path changes local networking.

## Open Questions

The live canary URL, freshness interval, `sing-box` artifact digest and probe
schedule remain private deployment decisions for later qualification tasks.
