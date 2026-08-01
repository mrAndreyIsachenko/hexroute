# Ingress Observer Runtime

`hexroute-ingress-observer` is the restricted instance-side producer for the
provider-neutral signed heartbeat contract. It has no AWS, DigitalOcean,
Cloudflare, Terraform, route, DNS or recovery authority.

## Runtime Contract

The systemd service reads these exact variables from its root-provisioned
environment file:

| Variable | Contract |
| --- | --- |
| `HEXROUTE_OBSERVER_LISTEN_ADDR` | Canonical literal IPv4 loopback `IP:port` used only inside the host |
| `HEXROUTE_OBSERVER_XRAY_ENDPOINT` | Canonical literal IPv4 loopback XRay listener endpoint |
| `HEXROUTE_OBSERVER_OUTBOUND_ENDPOINT` | Canonical public unicast literal IPv4 `IP:port` for the bounded egress check |
| `HEXROUTE_OBSERVER_NODE_ID` | Canonical UUID matching the signing key |
| `HEXROUTE_OBSERVER_GENERATION` | Bounded immutable deployment-generation reference |
| `HEXROUTE_OBSERVER_KEY_FILE` | Absolute path to an existing mode-private Hexroute Ed25519 key file |

Configuration is fail-closed. Hostnames, wildcard listeners, private outbound
targets, relative key paths, malformed generations and a key for another node
are rejected before listening. The key file is runtime material and must never
enter Git, Terraform, process arguments or logs.

`GET /v1/heartbeat` performs bounded TCP checks against the local XRay listener
and the outbound dependency. A completed response is always signed and reports
only node ID, generation, canonical observation time and the combined health
boolean. Dependency addresses and raw errors are never returned.

## Private Retrieval

No observer port is public. Private qualification starts a temporary
loopback-only sing-box SOCKS listener for the authenticated VLESS/Reality
transport and supplies that SOCKS URL to `hexroute-ingress-probe heartbeat`.
The probe permits plain HTTP only when both the SOCKS proxy and observer target
are literal loopback endpoints. Without that proxy, heartbeat endpoints remain
HTTPS-only.

This path provides a separately signed runtime-generation signal without a
second public port or a control-plane heartbeat dependency. It does not replace
the independent external TCP/TLS monitor or authenticated HTTPS canary.

## Reproducible Release

Build an immutable Linux AMD64 archive with an exact numeric version:

```sh
scripts/build-ingress-observer-release.sh 0.1.0 dist
```

The builder emits one `tar.gz` containing only
`hexroute-ingress-observer` and a matching `.sha256` file. Go paths, VCS build
metadata, build ID, archive timestamps and ownership are normalized. The
release test builds twice and requires byte-identical archives before a release
may be pinned by private infrastructure.
