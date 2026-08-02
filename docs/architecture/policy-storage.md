# Immutable Policy Storage

Hexroute keeps policy generations in two privilege-separated stores:

- root: `/Library/Application Support/Hexroute/policy-root`
- user: `~/Library/Application Support/Hexroute/policy-user`

The user home and primary UID/GID are resolved from the system account record;
the `HOME` environment variable cannot redirect the store.

Both roots contain a private `generations` directory. Store directories must
be owned by the exact daemon UID/GID with mode `0700`. Every path component is
opened directory-relative with `O_NOFOLLOW`; a symlink in the store path or an
artifact name is rejected.

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

An open store remains bound to the root and `generations` directory device and
inode. Every operation reopens the fixed path without following symlinks and
rejects a renamed or replaced store before reading or writing through the held
directory descriptors.

This storage package is not a live installation or cutover. Signed active
pointers, prepare receipts, commit intents and crash-point transactions belong
to OpenSpec task 4.2. Retention and rejected-payload cleanup belong to task 4.3,
and full signature/digest/startup revalidation belongs to task 4.4. Twilight
remains the production owner throughout those changes.
