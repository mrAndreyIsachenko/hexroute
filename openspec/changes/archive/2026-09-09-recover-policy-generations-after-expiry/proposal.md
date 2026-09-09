# Let a policy generation end without ending the system

## Why

The active generation expired on 2026-08-22 and the machine has been locked out
of its own policy control plane since. The installer refuses to place the
successor generation because it cannot revalidate the expired predecessor, and
the predecessor cannot be revalidated because time passed. Every other reason
this refusal exists has an operator action that clears it — corruption is
reinstalled, a bad signature is re-signed, a wrong clock is corrected. Expiry
has none: the only cure is the next generation, and the next generation is what
is refused. A guard that blocks its own remedy is broken independently of how
carefully the calendar is kept.

The same fact is treated two opposite ways in one system. Both daemons tolerate
the expired pointer — they suspend mutations, keep the data plane, keep
answering. The installer treats it as fatal. Neither behaviour is written down
as a requirement, so neither is wrong on purpose.

## What Changes

- Separate the historical question from the operational one. Deriving which
  generation preceded this one is a question about what *was*; it is currently
  asked through the function that answers whether a generation may govern
  *now*, and so it inherits every comparison against the present. Lineage keeps
  the cryptographic proof — signature under the pinned key, every digest, the
  immutable artifacts, the trusted compiler — and drops the two comparisons
  that are about the present: the validity window and the installed static
  digest.
- Give lineage a type that cannot authorize. It carries generation numbers, the
  payload digest, the policy schema and the predecessor's own validity and
  static digest as facts. It carries no manifest, no payload and no approval, so
  evaluating policy against a lapsed generation is not a discipline to maintain
  but a program that does not compile.
- Stop reporting expiry as a fault. An expired generation reports
  `state: none, reason: expired` and raises no authorization suspension.
  Mutations remain refused, because they are already refused by the absence of
  an active generation; what disappears is a `clock_anomaly` on a machine whose
  clock is correct.
- Announce the end of validity before it arrives. `expires_at` joins the status
  both daemons already report, and the user runtime raises a bounded
  `policy_expiry` incident at seven days, again at forty-eight hours, and again
  once a generation has lapsed and not been replaced. Nothing announced expiry
  before; that is why nobody saw this one.
- Let a prepared daemon configuration be checked before it is installed, by the
  same function the daemon validates it with rather than a second copy of the
  rules.
- **BREAKING** for the closed suspension-reason set only in the direction of
  narrowing: `clock_anomaly` stops being raised for expiry. No consumer gains a
  value it must handle; one stops being produced for a cause that was not it.

## Capabilities

**New Capabilities**

- `policy-generation-continuity` — a generation's end of validity is a scheduled
  event rather than a fault: announced before it arrives, ending authority
  without ending lineage, and recoverable by installing the successor.

**Modified Capabilities**

- `atomic-policy-generations` — the suspension overlay stops covering expiry,
  and the separation between what proves lineage and what permits operation is
  stated rather than assumed.

## Impact

`policystore` gains a lineage read and the narrow type it returns.
`policyinstaller` and both daemons derive the current generation from that read
instead of failing or trusting a declared value. `policycontrol` stops
suspending on expiry and reports a new bounded status reason. `notification`
and `event` gain one incident category and one template. `hexroute-policy`
gains an offline configuration check. The policy control plane, dormant on this
machine since 2026-08-09, is armed again on both daemons — which grants nothing:
the only generation in either store is an all-deny one, and authorization is
deny-by-default without a matching allow and a live lease.

## Non-Goals

- **Preventing expiry.** Time passes; the change makes expiry visible and
  survivable, not impossible.
- **Changing the thirty-day validity bound.** The bound is sound. What was
  missing is a default inside it and a signal near its end.
- **Relaxing anything for the candidate being installed.** A candidate is still
  refused for a wrong static digest, an untrusted compiler, a bad signature or a
  window it is outside. The relaxation is confined to reading the predecessor.
- **The Pritunl recovery grant.** This change activates a generation that grants
  nothing. The authority arrives in the next one.
- **A second notification channel.** The typed incident path exists and is used.

## Rollout

Four steps, in order. The offline check and the lineage read land first, because
every later step depends on them. Then both daemons are configured and restarted
under the guarded procedure, with the previous configuration kept beside the new
one. Then generation 3 is compiled, signed and activated: same content as the
expired generation 2, no new authority, and a deliberately short nine-day window
so the seven-day announcement is observed in two days rather than twenty-three.
That short window is the recorded deviation from the thirty-day default, and its
reason is this sentence.

## Rollback

The prepared configuration is a whole file, not an edit, so restoring the
previous one and restarting returns both daemons to the dormant control plane
they have run since August — which is the state this change starts from, and a
state the machine has demonstrably survived for a month. Generation 3 grants
nothing, so there is no authority to withdraw; if it is wrong it is replaced the
way any generation is replaced, by a higher one. The code change is revertible
on its own because nothing it installs depends on it: a bundle placed through
the lineage read is byte-identical to one placed through the strict path, since
the two differ only in which comparisons they refuse on.

## Ownership boundary

Public Hexroute owns the lineage read, the status and suspension semantics, the
incident category, the offline check and the documented default. The private
repository owns the prepared daemon configurations, the operator source, the
signed artifacts and the snapshot of the stranded store. Twilight is untouched.
