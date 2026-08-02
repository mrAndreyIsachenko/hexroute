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

The `state` directory contains typed transaction records plus two fixed index
names:

```text
prepare-11111111-1111-4111-8111-111111111111.json
commit-11111111-1111-4111-8111-111111111111.json
resolution-11111111-1111-4111-8111-111111111111.json
active.json
audit.json
```

Prepare receipts bind the local domain, bundle and policy generations,
manifest and payload digests, and approval digest. Commit intents embed the
single user-presence signed approval and bind both domain generations and
payload digests. Active pointers embed the same approval and reference the
immutable commit-intent digest. Storage performs strict canonical-JSON and
structural binding checks.

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

Immutable terminal resolution records drive each retention transaction. An
active resolution must exactly match the current active pointer and its durable
prepare receipt and commit intent, and remains with the retained generation. A
non-active resolution must not match the current pointer; after its redacted
audit entry is durable, the resolution and candidate transaction records are
removed. Unknown state filenames, malformed records, duplicate generation
ownership and incomplete active evidence fail closed before retention removes
anything.

Retention is generation-based rather than file-count-based. Each domain keeps:

- the newest 16 active resolved generations and their transaction evidence;
- every prepare receipt that has no terminal resolution and its generation;
- the generation referenced by the current active pointer;
- a canonical redacted audit index covering at most 90 days and 128 entries.

Audit entries contain only domain, terminal state, bundle and policy generation,
manifest and payload digests, timestamp and a bounded reason. They deliberately
exclude transaction IDs, approval signatures, selectors, endpoints, paths and
candidate bodies. For rejected and restart-required candidates, the updated
audit index is atomically persisted and directory-synchronized before full
generation artifacts and transaction records are removed. Retention also
cleans only recognized crash-temporary state files; it never accepts arbitrary
deletion paths.

## Startup Revalidation

`RevalidateActive` is a read-only, domain-local startup boundary. Under one
store lock it derives the active generation from `active.json`, then reads the
matching prepare receipt, commit intent, active resolution, manifest, local
payload, review and signed approval from fixed typed names. It rejects any
missing, malformed or cross-linked evidence before returning policy content.

Manifest, payload, review and approval files must be strict canonical JSON with
no unknown fields or trailing data. Revalidation recomputes manifest, payload,
review, approval and commit-intent digests; verifies the Ed25519 approval
against the statically pinned public key; checks the signed validity window;
requires `activated_at` to be inside that window and not in the future; and
requires exact bundle, domain generation, payload, compiler, policy-schema and
static-configuration compatibility with the installed daemon state. Root and
user stores verify only their own payload while the signed manifest and approval
continue to bind both domain digests.

A successful call returns a typed `RevalidatedActive` containing the verified
manifest and local payload. A failed call returns bounded sentinel errors and
does not rewrite the pointer, run retention, activate a fallback or mutate any
network/process state. Startup recovery may derive dynamic current-generation
fields only after this complete revalidation. If a crash left a valid active
pointer after atomic replacement but before its resolution record, recovery
durably completes that resolution; it never repairs invalid evidence.

## Domain-Local Prepare

`PrepareCandidate` accepts only bounded transaction, generation and digest
identity. It derives all four immutable artifact names from the local store
domain and generation, then repeats canonical manifest/payload/review/approval,
signature, validity, static, compiler and schema validation. It persists a
prepare receipt only after every check succeeds. The method cannot accept a
filesystem path, policy payload, command or runtime mutation callback.

Root `hexrouted` and user `hexroute-userd` expose this boundary through a
dedicated policy IPC handler. The handler is separate from the existing
operator mutation broker. Private static startup configuration pins the daemon
domain, supported schema, static digest, compiler identities and signer public
key. If that configuration is absent, policy status reports `none` and prepare
fails closed.

`hexroutectl policy` coordinates a bounded three-phase commit. Both domains
first persist `stage` intents while leaving their pointers unchanged. Only
after both stage responses match may `activate` replace either pointer. An
unconfirmed pointer reports `domain_mismatch` and the dispatcher rejects new
mutations without stopping existing processes or connections. `confirm` is
sent only after both activate responses match. Repeating the same transaction
after a crash is idempotent and converges the lagging domain forward.

## Monotonic Rollback Candidates

Rollback is forward-only compilation, not an active-pointer rewind.
`hexroute-policy rollback` reads one canonical historical target bundle and the
canonical current bundle, then creates a new unsigned candidate. The target
bundle generation must be lower than current, and both bundles must match the
current policy schema and compiled static safety envelope.

The compiler derives all counters instead of accepting them from operator
source:

- `bundle_generation` is current plus one;
- `parent_bundle_generation` is exactly current;
- an unchanged domain keeps its current policy generation;
- a changed domain advances its current policy generation by one.

The candidate receives new canonical UTC issue, activation and expiry values.
A historical authorization lease is retained only when it is already effective
at the new `not_before` and remains unexpired; its original expiry is never
extended. Composition removes allow rules that no retained lease authorizes.

Credential policy is continuity-protected. The historical and current
effective credential-rule sets must be semantically identical. If a credential
reference or its effect was removed or changed after the target generation,
rollback fails rather than restoring it. An intentional credential change must
use a normal reviewed policy source, not rollback.

The generated candidate still has no activation authority. It must pass the
normal semantic diff, deterministic replay, compatibility, user-presence
signature, installation, prepare and commit gates. The old signature, approval,
lease and active pointer are never reused. Selecting a target from retained
active store history remains a later operator workflow; this compiler path
performs no daemon, store or network mutation.

This storage package is not a live installation or cutover. Candidate prepare,
commit, abort and crash convergence are wired to the root and user daemon IPC
handlers. They mutate only Hexroute's private policy stores. No process, route,
credential or network mutation is enabled, and Twilight remains the production
owner throughout these changes.
