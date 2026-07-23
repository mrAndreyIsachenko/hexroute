# Normalized Twilight Trace Fixtures

The JSONL files under `testdata/traces/v1` preserve the ordering and safety
decisions of representative Twilight behavior while removing all live
identifiers. Absolute timestamps become monotonic offsets; PIDs, interface
names, hostnames, addresses, profile IDs and credentials are omitted.

These traces cover startup, isolated probe loss, sustained ingress loss,
sing-box exit, physical-network recovery, sleep/wake and Pritunl disconnect.
They are inputs for deterministic replay tests, not commands to execute.

Every trace action belongs to a closed allowlist. In particular, there is no
action for AdGuard mutation, arbitrary command execution or Mac-triggered VPS
XRay restart.
