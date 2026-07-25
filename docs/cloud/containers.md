# Cloud Container Contract

Hexroute packages the cloud API and worker from one immutable source tree and
one `hexroute-ingest` binary. The deployment selects the `api` or `worker`
component while retaining the same build provenance.

The image contract is:

- the Go builder image is versioned and pinned by a multi-architecture digest;
- the runtime stage is `scratch` and contains only the root-owned, non-writable
  static binary and public CA certificates;
- the process runs as numeric UID and GID `65532`;
- the root filesystem is read-only;
- all Linux capabilities are dropped and privilege escalation is disabled;
- `/tmp` is the only writable path, mounted as a bounded `noexec`, `nosuid`,
  `nodev` tmpfs;
- no provider credentials, deployment values or source repository metadata are
  copied into the build context or image.

`deploy/container/compose.contract.yaml` is a local executable contract, not a
live deployment manifest. DigitalOcean App Platform does not expose Docker
`read_only`, capability-drop, `no-new-privileges`, or tmpfs controls. The
deployment therefore preserves the non-root `scratch` image and root-owned
mode-0555 binary so the process cannot modify image content, but this is not
equivalent to a container-engine read-only mount. The remaining platform gap is
recorded explicitly instead of claiming an unavailable control.

The image has no Docker `CMD`. With no arguments and no component environment,
the binary runs the non-mutating startup check and exits. App Platform leaves
the image entrypoint intact and sets the allowlisted `HEXROUTE_COMPONENT` value
to `api` or `worker`; a custom run command is forbidden because App Platform
would override the `scratch` image entrypoint. Both modes validate their
complete configuration and database-role boundary before serving or scheduling
work.

Build and verify the image locally:

```sh
make container-build
make container-test
```

`container-test` creates independent API and worker containers with the
hardened runtime options, inspects their effective Docker configuration, runs
the non-mutating startup check and verifies that the image contains neither a
shell nor a package manager.

The image does not own any local or VPS recovery action. Cloud processes remain
telemetry-only and cannot reach macOS routes, Pritunl credentials or AdGuard.
