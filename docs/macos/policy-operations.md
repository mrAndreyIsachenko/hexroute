# Policy Generation Operations

This runbook describes the local compile, review, signing and typed activation
workflow. Hexroute remains pre-cutover: Twilight owns the production data path,
and the only policy-enforced capability in this change is the state-only
`operator_resume`. Nothing in this workflow authorizes a route, process,
tunnel, credential or AdGuard mutation.

## Artifact Boundary

Real operator YAML, compiled candidates, signed approvals, trust fingerprints,
activation receipts and qualification evidence are private local artifacts.
Keep them in mode-`0700` directories outside the public repository. The
repository ignore and release gates reject common live policy filenames, but
that is a backstop rather than a storage mechanism.

The dynamic policy workflow cannot change static authority. Daemon role,
UID/GID, socket and launchd identity, policy-store roots, executable identity,
supported schema range, compiled safety envelope, signer public key and signer
fingerprint are installed static configuration. A candidate with a different
static digest returns `restart_required`; it must not be forced through dynamic
activation. Changing static authority requires a separately reviewed binary or
configuration installation and guarded daemon restart.

`hexroute-policy` is an unprivileged offline compiler. `hexroutectl` carries
only typed transaction identities over fixed authenticated sockets. It has no
field for a source path, payload, command, endpoint or credential. The public
repository intentionally does not provide a generic privileged copy command;
installation of immutable candidate files into both policy stores remains a
separate deployment boundary.

## Prepare The Workspace

Use the Developer-signed signer app documented in
[`policy-signing.md`](policy-signing.md). The same app executable must provision,
verify and use the user-presence Keychain key.

Define private paths and identities in the current shell without putting secret
values in Git, argv logs or shell history:

```sh
POLICY_BIN='<signed app>/Contents/MacOS/hexroute-policy'
WORK_ROOT='<private mode-0700 work root>'
SOURCE='<private operator YAML>'
CURRENT='<private current canonical candidate>'
CANDIDATE="$WORK_ROOT/candidate-next"
DIFF="$WORK_ROOT/diff-next"
REPLAY="$WORK_ROOT/replay-next"
SIGNED="$WORK_ROOT/signed-next"
```

The operator source is strict YAML. Duplicate keys, aliases, anchors and
unknown fields fail compilation. The candidate output directory must not exist;
the compiler creates it privately and never replaces an existing artifact.

## Compile, Diff And Replay

Compile the complete effective snapshot. For the first generation omit
`--current`; every later generation supplies the current canonical candidate.

```sh
"$POLICY_BIN" compile \
  --source "$SOURCE" \
  --current "$CURRENT" \
  --out "$CANDIDATE" \
  --compiler-version '<installed compiler version>' \
  --compiler-sha256 '<installed compiler sha256>' \
  --signer-fingerprint '<pinned signer sha256>'
```

Compilation emits canonical `manifest.json`, `root.json` and `user.json`.
Conflicting selectors, compatibility violations, safety-envelope expansion and
semantic no-ops fail before output becomes eligible for signing.

Generate and inspect the semantic authorization diff:

```sh
"$POLICY_BIN" diff \
  --current "$CURRENT" \
  --candidate "$CANDIDATE" \
  --out "$DIFF"
```

For an initial policy use `--initial` instead of `--current`. Review every
newly allowed, newly denied and changed plan, with particular attention to an
authorization expansion.

Run deterministic replay over synthetic invariants and eligible redacted local
traces:

```sh
"$POLICY_BIN" replay \
  --candidate "$CANDIDATE" \
  --cases '<private synthetic cases JSONL>' \
  --root-trace '<private redacted trace JSONL>' \
  --out "$REPLAY"
```

`--root-trace` is repeatable. A conflict, unsafe diff or failed replay blocks
the next stage; do not bypass it by editing generated JSON.

## Sign With User Presence

Signing revalidates the candidate, semantic diff, replay report, installed
root/user compatibility and pinned public key before requesting Touch ID or
other macOS user presence:

```sh
"$POLICY_BIN" sign \
  --current "$CURRENT" \
  --candidate "$CANDIDATE" \
  --diff "$DIFF/diff.json" \
  --replay "$REPLAY/replay.json" \
  --compatibility '<private installed compatibility JSON>' \
  --public-key '<private pinned public-key file>' \
  --keychain-service '<private service>' \
  --keychain-account '<private account>' \
  --out "$SIGNED"
```

For an initial policy omit `--current`. The result is canonical `review.json`
and `approval.json`. The approval binds the exact manifest, both domain
payloads, review result and validity interval. The private Ed25519 seed never
leaves Keychain.

## Install Then Activate

Do not hand-copy candidate files into a live store. A privileged installer must
derive immutable filenames from the signed manifest, place only the expected
manifest, local payload, review and approval in each fixed domain store, and
apply the ownership, mode, no-symlink and `fsync` rules in
[`policy-storage.md`](../architecture/policy-storage.md). Installation does not
change an active pointer.

For the initial bundle the installer validates against the static bootstrap
compatibility anchor. For every later bundle it first revalidates the confirmed
active pointer, signature and immutable artifacts from the local domain store,
then derives the current parent and domain generation from that evidence. It
never trusts a caller-supplied current generation or rewrites an existing store
to make a candidate fit.

After both stores contain the same signed bundle, derive one bounded
transaction identity from canonical artifacts:

- a new transaction UUID;
- bundle, root-policy and user-policy generations from `manifest.json`;
- SHA-256 of canonical `manifest.json`, `root.json`, `user.json` and
  `approval.json`.

Check both domains before mutation:

```sh
bin/hexroutectl policy status
```

An optional prepare-only check persists matching domain receipts but changes no
active pointer:

```sh
bin/hexroutectl policy prepare \
  --transaction-id '<uuid>' \
  --bundle-generation '<n>' \
  --root-generation '<n>' \
  --user-generation '<n>' \
  --manifest-sha256 '<sha256>' \
  --root-payload-sha256 '<sha256>' \
  --user-payload-sha256 '<sha256>' \
  --approval-sha256 '<sha256>'
```

Activation uses the same identity. `commit` re-runs prepare, stages durable
commit intents in both domains, activates both pointers and confirms only after
both activation responses match:

```sh
bin/hexroutectl policy commit \
  --transaction-id '<same uuid>' \
  --bundle-generation '<same n>' \
  --root-generation '<same n>' \
  --user-generation '<same n>' \
  --manifest-sha256 '<same sha256>' \
  --root-payload-sha256 '<same sha256>' \
  --user-payload-sha256 '<same sha256>' \
  --approval-sha256 '<same sha256>'
```

If prepare or stage fails before a pointer changes, abort the same identity:

```sh
bin/hexroutectl policy abort <the same identity flags>
```

If one pointer has activated, do not invent a new transaction and do not rewind
the other domain. Both daemons report `domain_mismatch` and block new mutations
while existing connectivity remains untouched. Restore the unavailable daemon,
then repeat `policy commit` with the exact same transaction identity so the
lagging domain converges forward.

## Interpret Status

Both `hexroutectl status` and `hexroutectl policy status` expose only domain,
bundle and policy generations, manifest digest, lifecycle state, activation
time, bounded reason and the authorization/existing-state overlays.

| State | Meaning | Operator response |
|---|---|---|
| `none` | No locally valid generation | Keep observe-only `SAFE_MODE`; install a valid signed generation |
| `prepared` | Candidate verified and receipt durable | Verify both receipts before commit |
| `active` | Signed pointer confirmed for this domain | Require matching bundle generation in both domains |
| `rejected` | Candidate failed a bounded validation gate | Correct the source and compile a newer candidate |
| `restart_required` | Dynamic candidate disagrees with static authority | Use a separately reviewed static installation; never force activation |
| `domain_mismatch` | The active pointer is not confirmed. Despite the name this does **not** mean the domains disagree — an unconfirmed pointer reports it even when both domains hold the same bundle. Read both pointers before concluding anything | Repeat `policy commit` with the **same** transaction identity so the confirmation completes |
| `expired` | A generation reached its own `expires_at`. Not a fault: the clock is correct and the chain is intact, so the successor can be installed on top of it | Compile, sign and activate the next generation |
| `authorization_suspended` | Local corruption, signature/digest, clock or IPC ownership guard narrowed authority | Preserve connectivity, correct the local fault and revalidate |

Status and telemetry never contain selectors, endpoints, source paths, leases,
credential references or credential values. Cloud availability is irrelevant
to compile, prepare, commit, suspension, safe mode and `operator_resume`.

## Rebuilding The Signer Changes What Must Be Trusted

The signed application carries the compiler, so a change to the compiler is not
in effect until the application is rebuilt from it — a candidate the new rules
would accept is still refused by the binary on disk.

Rebuilding changes the application's digest, and a candidate is refused unless
its `compiler_sha256` is in the installed `trusted_compiler_sha256`. So a
compiler change means: rebuild, add the new digest to both daemon
configurations, check them offline, install and restart — and only then compile.

Keep the previous digests in the list. The lineage read still checks the
compiler that produced the predecessor, and dropping its digest makes the
generation already on disk unreadable.

## Validity

Sign for the full thirty days unless there is a reason not to, and write the
reason down beside the source when there is. A shorter window buys nothing: it
does not narrow the authority, which is what the rules and leases inside the
generation are for, and it does not speed up revocation, which is a rollback
rather than a wait. All it buys is a more frequent ceremony with user presence,
and therefore more chances to miss one.

The first generation signed here took fourteen days for no recorded reason, and
the machine spent a month locked out of its own policy control plane afterwards.

## When A Generation Has Lapsed

An expired generation reports `state: none` with reason `expired`, raises no
suspension, and leaves its chain intact. Install the successor on top of it in
the ordinary way: the parent is derived from the store, and the derivation does
not require the predecessor to still be in force.

`no_valid_generation` is the different case — no generation was ever installed,
so there is no chain to continue.

## Do Not Improvise Around A Commit

`policy commit` runs prepare, stage, activate and confirm itself. Running
`policy prepare` separately first leaves a prepared identity in the daemon that
a later commit with a different identity cannot match, and the refusal reads as
`precondition_failed`, which says nothing about the cause.

`policy abort` is for a transaction that has **not** activated. Issued after the
activation phase it writes a rejected resolution for a transaction whose pointer
says active, and startup validation requires those two to agree. The records are
immutable, so the contradiction cannot be edited out afterwards — the store has
to be restored from a copy taken beforehand.

Read both active pointers before deciding anything. Every mistake in this
paragraph was made in one afternoon by acting first.

## Monotonic Rollback

Rollback never rewinds an active pointer and never reuses an old signature or
expired authorization lease. Compile historical effective content into a new
higher bundle generation:

```sh
"$POLICY_BIN" rollback \
  --target '<private historical canonical candidate>' \
  --current "$CURRENT" \
  --out '<private new rollback candidate>' \
  --compiler-version '<installed compiler version>' \
  --compiler-sha256 '<installed compiler sha256>' \
  --signer-fingerprint '<pinned signer sha256>' \
  --issued-at '<canonical UTC>' \
  --not-before '<canonical UTC>' \
  --expires-at '<canonical UTC>'
```

Run the normal diff, replay, user-presence signing and immutable installation
steps against that new candidate. Then use `hexroutectl policy rollback` with
its new transaction identity; the coordinator intentionally executes the same
prepare, stage, activate and confirm protocol as `policy commit`. Credential
policy changes and expired or revoked leases fail rollback rather than being
revived.

After activation, re-run both status commands and retain only redacted bounded
evidence outside public Git. A policy rollback does not stop or reconfigure
Twilight, AdGuard, Pritunl or established sessions.
