# Design

## What was measured

Nothing here was inferred. The live user store was opened and asked directly:

    RecoverActive(new static, now)          policy approval is outside its validity window
    RecoverActive(old static, now)          policy approval is outside its validity window
    RecoverActive(old static, in window)    <nil>
    RecoverActive(new static, in window)    policy static configuration requires restart

The third line is the important one. Inside its window the expired pointer
passes **everything** — signature, digests, immutable artifacts, receipts,
trusted compiler. The store is not damaged. Two comparisons stand in the way,
and both are about the present rather than about the artifact.

A handler built over the same store reported:

    state=none reason=no_valid_generation
    suspension={Suspended:true Reason:clock_anomaly}
    mutation_allowed=false
    authorize_pritunl_recovery allowed=false reason=inactive_policy

## The defect in one sentence

The installer asks a historical question through a function that answers an
operational one.

`runtimeForInstall` needs to know which generation came before. It asks
`RecoverActive`, whose actual question is "may this generation govern right
now?", and so it inherits the window check and the static-digest check. Both
answers are correct for the question that function asks. Neither is relevant to
the question the installer is asking.

Once stated that way the fix stops being a list of exceptions. Lineage is what
was; compatibility is what may run. The lineage path keeps every check that
proves the artifact — the signature under the pinned key, every digest, the
immutable artifacts, the trusted compiler — and drops the two that compare the
artifact to the present.

## Why not simply reset the store

It works today, with no code at all: move the active pointer aside and the
bootstrap branch opens. It was rejected because the safety envelope changes
whenever a capability is added — `pritunl_recovery` had just added one — so
under a reset-on-static-change rule the chain restarts routinely. A monotonic
history that breaks on every extension is not a history. And the first
production grant of root authority in this system would then arrive in a store
whose counter had been reset by hand to get past a guard.

## Why the relaxed path returns a different type

`RecoverActive` returns `RevalidatedActive`, which carries the manifest, the
payload and the intent — everything needed to authorize. If the relaxed call
returned that type, a future caller could evaluate policy against a lapsed
generation, and the mistake would look like an ordinary function call. Review is
the only thing standing between that and production, and review here is one
person who has already been wrong twice this week.

So the relaxed call returns a record carrying generation numbers, the payload
digest, the schema, and the predecessor's own validity and static digest as
facts. Authorizing from it is not discouraged; it is unwritable.

That decision then made a second one cheap. The daemon has the same problem in
mirror image: with an unrevalidatable pointer, `handler.config.Installed` keeps
whatever the configuration file declared, and `CheckCandidateCompatibility`
requires the candidate's parent to equal it. So the operator would hand-write
generation numbers and a payload digest into a root-owned file — a value typed
by a human into a file for a machine, verified by reading. A wrong character
returns `ErrActivePolicyMismatch`, which reads like a policy problem and sends
the search somewhere else entirely. Because the lineage record cannot authorize,
letting the daemon read it too costs nothing and removes the hand-written value.

## Why expiry stops suspending

The overlay is a fault channel: corruption, bad signature, digest mismatch,
domain mismatch, clock anomaly, IPC ownership. Every one is something that went
wrong and that an operator action clears. Expiry is the system working as
designed, and no local action clears it — only the next generation.

It also contributes nothing. `mutationAllowedLocked` already refuses on
`!hasActive`, which is exactly the expired case; the overlay's only effect there
is to name a cause that is not the cause. Adding a seventh reason instead would
write "reaching your designed end of life is a local fault" into the spec.

The status was already right — `state: none` — so only the reason needed a
value that distinguishes a chain that lapsed from a store that never had one.
That distinction is not cosmetic: one of them means there is something to build
on.

## The silence this creates, and paying for it

Removing the overlay removes the only loud signal that the generation is gone.
Before this change the machine at least showed `suspended=true`; afterwards it
would sit quietly with no active policy. That trade is only acceptable if
something else speaks, which is why the announcement is in this change and not
in a later one.

`expires_at` was surfaced nowhere: not in status, not in the qualification
tooling, not in any script. The only way to know a generation had eight days
left was to read a private manifest by hand. So a passive field alone would not
have prevented this — nobody was looking, because there was nothing to suggest
looking. The channel has to reach a human who is not looking, which the typed
incident path already does.

Two thresholds, not three. Seven days is the ceremony's lead time: a working
signed signer app, the operator physically at this Mac for Touch ID, root for
the root store, and a full compile → diff → replay → sign → install → activate
across both domains. That is an evening, and it cannot be done remotely or
unattended, so the warning must survive a trip or a holiday. Forty-eight hours
is the second. Fourteen days was rejected because nobody acts two weeks out, and
an announcement that is always ignored teaches you to ignore the next one.

The category is new rather than borrowed. `security_validation` would have
routed it to the security-failure template, telling the operator that a security
check failed when nothing failed — the same lie as `clock_anomaly`, introduced
in the change that exists to stop telling it. Non-critical, so the night window
defers it: nobody should be woken at three in the morning for a deadline that is
two days away.

## Why generation 3 grants nothing

The change must install and activate something, or the lock is untested. It
installs a generation with the same all-deny content as the expired one and a
fresh window.

Separating the two questions is the whole point. If one activation both repaired
the lifecycle and granted the first production root capability, a failure would
not say which half was wrong. And if the lineage path still has a flaw, it is
found while nothing at all is granted.

This is the argument the Pritunl change already made about itself — go first
where being wrong is cheap — applied one step earlier.

The compiler accepts it: `EffectiveSemanticSHA256` projects `StaticSHA256`,
`NotBefore` and `ExpiresAt` alongside the rules, so identical content under a new
window and a new static digest is not a semantic no-op. This was checked in the
projection rather than assumed.

## Why nine days

The seven-day threshold on a thirty-day generation is observed twenty-three days
later, and the Pritunl cutover waits for this change to close. Signing
generation 3 for nine days puts the crossing two days out and is the documented
deviation from the thirty-day default, used exactly as the default's escape
hatch intends.

The risk is honest: if the Pritunl cutover misses that window, generation 3
expires. After this change that is no longer a deadlock but a repeat of the
ordinary ceremony — so the worst case of the risk is another demonstration that
the repair works.

## What the offline check is for, and what would ruin it

Writing `policy_control` into the root-owned configuration is the step with the
worst failure mode in this change: an invalid block returns `ErrInvalidConfig`,
the daemon does not start, launchd retries it, and the diagnosis is read from
logs as root on a machine that no longer has a root observer. Nothing in the
system can check that block before it is installed — `read-config` is about
ingress configuration versions and is unrelated.

The check must call `StaticConfig.Runtime`, not restate it. A checker with its
own copy of the rules drifts, and a drifted checker accepts a file the daemon
rejects — which is worse than no checker, because it supplies confidence
immediately before an irreversible step.

Fail-closed startup is kept. A daemon that quietly runs without policy control
is how this machine sat dormant since August without anyone noticing.

## The snapshot

The stranded store is a naturally occurring instance of the defect, and applying
the fix destroys it. It carries real signatures, real artifacts, a two-generation
history and a genuinely superseded static digest — a combination no fixture
reproduces at once. It is copied privately before anything is touched, because
that is a decision available exactly once.

The public regression stays synthetic: the test signs, activates and evaluates
its own generation past its own `expires_at`. Live policy material does not enter
this repository.
