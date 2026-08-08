# Local Operator Surface

`hexroutectl` reads root and user daemon state through separate authenticated
Unix sockets. The protocol is versioned, frame-bounded and allowlisted. It has
no field for executable paths, shell text, endpoint values, profile IDs or
credentials.

Build the local client:

```sh
make build-ctl
```

Read both daemon states:

```sh
bin/hexroutectl status
bin/hexroutectl diagnostics
bin/hexroutectl safe-mode
```

Use `--scope root` or `--scope user` to query one ownership domain. A partial
or rejected request returns non-zero while preserving any available typed
result. Transport errors are reduced to a generic local-endpoint error and are
not copied into diagnostic output.

Diagnostics contain only daemon role, mode, lifecycle state, generation,
recovery counters, monotonic ticks and an allowlisted reason. They never read
raw daemon logs or configuration.

Status also carries the matching redacted policy projection: bundle and domain
generations, manifest digest, lifecycle state, activation time, bounded reason
and authorization overlays. The complete compile, signing, typed activation
and monotonic rollback workflow is documented in
[`policy-operations.md`](policy-operations.md).

Before any activation, require both domains to be available and either both to
report `none/no_valid_generation` for an initial rollout or both to report the
same confirmed bundle for an update:

```sh
bin/hexroutectl policy status
```

Root and user policy generations advance independently, so a valid status may
show one unchanged domain generation under a newer shared bundle. A mismatch,
unconfirmed pointer or authorization suspension blocks the transaction. An
active policy snapshot does not by itself grant production mutation authority;
the separately recorded shadow qualification gate and executable capability
boundary still apply.

An explicit resume requires the exact generation shown by status and a target
owned by that socket:

```sh
bin/hexroutectl resume \
  --scope user \
  --target pritunl \
  --generation 12
```

Resume is accepted only while that target is in `SAFE_MODE`. A stale
generation, a healthy/degraded target, or a target from the other ownership
domain is rejected. Resume clears the exhausted budget into `DEGRADED`; it
does not directly restart a process or reconnect a service.

The root socket is fixed at `/var/run/hexroute-observe/hexrouted.sock`. Its
file is readable only by the configured operator UID, while the root-owned
parent is non-writable. The user socket is fixed beside the private user state
snapshot. Socket peer credentials are checked by the receiving daemon.
