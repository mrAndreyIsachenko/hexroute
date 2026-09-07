# Let a capability that has two halves be granted to both of them

## Why

The Pritunl recovery cutover cannot be activated, because the generation its own
runbook describes does not compile. Granting `pritunl_recovery` on the `pritunl`
target to both domains returns `cross_domain_ownership` and the compiler refuses
the candidate.

Four places agree with each other and disagree with the detector. The runbook
says the generation grants the capability "to the user domain on the `pritunl`
target and to the root domain on the same". The root runtime asks for exactly
that pair with the target written as a constant, and the user runtime asks for
its own. The safety envelope lists the capability under both domains and lists
`pritunl` under root's targets with a comment explaining that naming the service
is more honest than folding it into `runtime`.

The detector is applied to every selector kind uniformly, and its only test uses
a credential selector — one key claimed by two domains, which is a real
violation. An action capability is not that: the envelope, which the baseline
makes the owner of action allowlists, assigns capability and target per domain
and may assign to both.

Nothing caught it because no test has ever compiled a generation that grants
this capability. It appears in one test file, which evaluates authorization
against a ready-made payload and never reaches the compiler.

## What Changes

- Cross-domain overlap stops being a conflict for action selectors, and stays
  one for credentials, routes and endpoints. The line between them is what the
  overlap contradicts: a credential has one owner, a route one path, an endpoint
  one host, and claiming them twice contradicts a fact about the machine. A
  capability has no such single owner — the envelope hands out capability and
  target per domain, and `ValidateAgainstEnvelope` has already agreed by the
  time conflicts are looked for.
- Tie the envelope to the compiler with a property rather than a list: a
  capability the envelope permits in both domains must compile in both.
- Cross the boundary that hid this. A compiled payload is fed to the evaluator
  and asked to authorize the act in both domains, so that what the compiler
  emits and what the evaluator accepts are tested as one path rather than two.

## Capabilities

**Modified Capabilities**

- `atomic-policy-generations` — selector ambiguity rejection states what a
  cross-domain overlap means for each kind of selector, rather than treating
  every kind as an ownership violation.

## Impact

`internal/policy/conflict.go` stops short-circuiting on domain for action
selectors. Nothing else changes: the envelope, both runtimes and the runbook
already describe the behaviour this permits. `cut-pritunl-recovery-to-hexroute`
is unblocked at tasks 4.1 and 4.2.

## Non-Goals

- **Relaxing the rule for other selector kinds.** Credentials, routes and
  endpoints keep it, because each of them is singular on this machine and
  claiming one twice is a contradiction rather than an arrangement.
- **Splitting the capability in two.** Naming the halves separately would give
  each a single owner and need no rule change at all, and it was rejected: the
  design made this one capability so that revoking it removes both halves at
  once. Two capabilities can be revoked one at a time, leaving root able to
  restart the service after the user runtime has lost the right to reconnect —
  which is the drift a single grant exists to prevent.
- **Moving root's target to `runtime`.** It would compile and it would make the
  name less honest to satisfy a check. The envelope says why `pritunl` is named.
- **The cutover itself.** Compiling, signing, installing and activating
  generation 4 belongs to the change this unblocks, and is proven there.

## Rollout

Code and specification only. The next generation compiled after it can grant the
capability to both domains; every generation already signed is unaffected,
because none of them grants it.

## Rollback

Revert. No artifact changes shape, no store is touched, and the generation
active on the machine grants nothing either way.

## Ownership boundary

Public Hexroute owns the compiler, the conflict rule and the tests. Nothing
private is involved: no generation that exists today grants this capability.
