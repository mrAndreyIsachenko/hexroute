# Immutable Policy Storage

Hexroute keeps policy generations in two privilege-separated stores:

- root: `/Library/Application Support/Hexroute/policy-root`
- user: `~/Library/Application Support/Hexroute/policy-user`

The user home and primary UID/GID are resolved from the system account record;
the `HOME` environment variable cannot redirect the store.

Both roots contain private `generations` and `state` directories. Store
directories must be owned by the exact daemon UID/GID with mode `0700`. Every
path component is opened directory-relative with `O_NOFOLLOW`; a symlink in
the store path or an artifact name is rejected. An open store binds all three
directories to their device and inode and rejects path replacement.

Generation files use names derived only from typed bundle generation, policy
generation, domain and artifact kind:

```text
bundle-00000000000000000007-root-00000000000000000003-manifest.json
bundle-00000000000000000007-root-00000000000000000003-payload.json
bundle-00000000000000000007-root-00000000000000000003-review.json
bundle-00000000000000000007-root-00000000000000000003-approval.json
```

The review artifact is retained because its canonical digest is bound into the
signed approval statement; later domain verification can therefore recheck the
complete approval chain without consulting compiler output outside the store.

Files are created with `O_EXCL`, synchronized, changed to mode `0400` and
accepted only when they are single-link regular files owned by the exact store
UID/GID. The API never accepts a caller-supplied artifact path and never
replaces an existing generation file.

The `state` directory contains only names derived from typed transaction IDs:

```text
prepare-11111111-1111-4111-8111-111111111111.json
commit-11111111-1111-4111-8111-111111111111.json
active.json
```

Prepare receipts bind the local domain, bundle and policy generations,
manifest and payload digests, and approval digest. Commit intents embed the
single user-presence signed approval and bind both domain generations and
payload digests. Active pointers embed the same approval and reference the
immutable commit-intent digest. Storage performs strict canonical-JSON and
structural binding checks; complete signature, manifest, payload, static and
validity revalidation remains the daemon verification responsibility in
OpenSpec task 4.4.

State writes use one fixed sequence:

1. write and protect a private temporary regular file;
2. `fsync` the file;
3. atomically rename it to the typed final name;
4. `fsync` the `state` directory.

Prepare receipts and commit intents use no-replace rename and are immutable;
byte-identical retries are idempotent and conflicting retries fail. A commit
intent requires a matching durable local receipt, and an active-pointer update
requires the matching durable commit intent. Active pointers may be replaced
only by a higher bundle generation. A crash at any side of every file-sync,
rename or directory-sync boundary leaves either the old complete record or the
new complete record; retry converges without overwriting immutable evidence.

This storage package is not a live installation or cutover. Signed active
pointers, prepare receipts, commit intents and crash-point persistence are now
implemented, but are not wired to either daemon. Retention and rejected-payload
cleanup belong to task 4.3, and full signature/digest/startup revalidation
belongs to task 4.4. Twilight remains the production owner throughout those
changes.
