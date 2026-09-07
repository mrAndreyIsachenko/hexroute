# Design

## What was measured

    FindConflicts, root allow pritunl_recovery→pritunl + user allow the same
      → [cross_domain_ownership]
    the user rule alone
      → []
    different targets, root=runtime and user=pritunl
      → []

So `ComposeEffectiveSnapshot` fails with "effective policy contains conflicting
selectors" and `hexroute-policy compile` refuses the candidate. The generation
the cutover runbook describes cannot be produced.

## Four in agreement, one apart

The runbook says the generation grants the capability "to the user domain on the
`pritunl` target and to the root domain on the same, and grants nothing else".
`rootdaemon/run.go` asks for `DomainRoot, "pritunl"` with the target written as a
constant, and `userdaemon/recovery.go` asks for the user's own. The safety
envelope lists the capability under both domains, and lists `pritunl` under
root's targets with a comment saying that naming the service is more honest than
folding it into `runtime`.

The detector disagrees with all four. When four independent places agree and one
refuses, the refusal is the thing to examine.

## What the rule was written for

`classifyConflict` short-circuits before any semantics are considered:

    if !selectorsOverlap(left.Selector, right.Selector) { return "", false }
    if leftDomain != rightDomain { return ConflictCrossDomain, true }

and for action selectors, overlap is simply the same capability and the same
target.

Its only test is `TestCrossDomainSelectorOwnershipConflict`, and it uses a
**credential** selector: one key claimed by two domains. That is the case the
rule exists for, and it is a real violation — the privilege boundary this whole
system is built around keeps root's network and process authority apart from the
user's Keychain access, and a credential with two owners denies it.

The rule was then applied to every selector kind alike.

## The line between the kinds

What matters is what an overlap contradicts.

A credential reference, a route and an endpoint are each singular on the host.
One key has one owner, one prefix one path, one host and port one listener. Two
domains claiming one contradicts a fact about the machine, and no arrangement of
policy makes both true.

An action capability has no such singular owner. The envelope hands out
capability and target per domain and may hand the same pair to both — and the
baseline makes it the owner of that decision: "Static configuration SHALL own …
credential ownership, action allowlists". `ValidateAgainstEnvelope` runs before
conflicts are looked for, so by the time the detector sees a rule the envelope
has already agreed the domain may name that pair.

Nor is there ambiguity to reject. `EvaluateActionAuthorization` takes one
domain's payload; two domains' rules are never candidates for one evaluation.
The requirement the detector serves is called selector *ambiguity* rejection,
and across payloads there is none.

## Why differing effects across domains still compile

The narrower repair was to fall through to the same-domain check, where actions
conflict when their effects differ. It was rejected for what it would forbid:
root allowing and user denying the same pair is exactly the state a partial
rollback produces — the user half withdrawn, root's left standing. Making that a
compile error would refuse a legitimate intermediate step at the moment it is
most needed.

## Why not two capabilities

Naming the halves separately gives each a single owner and needs no rule change:
the user runtime submits a credential, the root runtime restarts a service, and
those are different acts.

It was rejected because the design made this one capability deliberately, so
that both halves ask the same question and one rollback removes both. Two
capabilities can be revoked one at a time, leaving root able to restart the
service after the user runtime has lost the right to reconnect. That drift is
what a single grant prevents, and it is worth more than the tidiness of one
owner per name.

## Why not move root's target

`runtime` is in root's allowlist and would compile today. The envelope explains
why `pritunl` is there instead: a reader of a granted generation should see which
service the grant is about. Renaming it to satisfy a check would make the
generation less honest in exactly the place an operator reads it.

## How this was allowed to happen

`CapabilityPritunlRecovery` appears in one test file. That test evaluates
authorization against a `DomainPayload` written by hand, and never reaches
`ComposeEffectiveSnapshot`, `CompileBundle` or `FindConflicts`.

So the capability was defined, permitted by the envelope, asked for by both
runtimes, documented in a runbook, and never once compiled. Every part was
proven; the path between them was not — the payload crosses from the compiler to
the evaluator, and nothing tested the crossing.

That is why the tests here are not only about the conflict rule. One derives its
cases from the envelope, so that a capability permitted in both domains must
compile in both — for the next capability as much as for this one. Another
compiles a granting candidate and hands the result to the evaluator, so the two
halves are exercised as one path.
