# Cloud Container Contract

Hexroute packages the cloud API and worker from one immutable source tree and
one `hexroute-ingest` binary. The deployment selects the `api` or `worker`
component while retaining the same build provenance.

The image contract is:

- the Go builder image is versioned and pinned by a multi-architecture digest;
- the runtime stage is `scratch` and contains only the static binary and public
  CA certificates;
- the process runs as numeric UID and GID `65532`;
- the root filesystem is read-only;
- all Linux capabilities are dropped and privilege escalation is disabled;
- `/tmp` is the only writable path, mounted as a bounded `noexec`, `nosuid`,
  `nodev` tmpfs;
- no provider credentials, deployment values or source repository metadata are
  copied into the build context or image.

`deploy/container/compose.contract.yaml` is a local executable contract, not a
live deployment manifest. The private infrastructure repository must express
the same restrictions for both App Platform components and provide their
separate database identities and runtime secrets.

The image defaults to the non-mutating `--check` command and exits. API
deployments must explicitly run `hexroute-ingest api`; worker deployment
remains blocked until its runtime mode composes the maintenance packages with
graceful shutdown. A deployment must never treat the default check command as a
long-running service.

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
