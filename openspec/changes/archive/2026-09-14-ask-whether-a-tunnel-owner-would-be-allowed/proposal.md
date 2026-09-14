# Ask whether a tunnel owner would be allowed

## Why

This runtime decides what a tunnel owner would do and records it. Nothing asks
whether it would be permitted to do it, because no capability in the policy
model covers owning a tunnel: the two that exist are `operator_resume` and
`pritunl_recovery`.

The switch this prepares for cannot be made on a generation nobody has exercised.
A grant first read at the moment another runtime is booted out is a grant read
where being wrong costs the network — which is the same argument that put the
decision rule through a soak before it was allowed to act.

## What Changes

- The policy model gains a capability for owning the tunnel, allowed to the root
  domain and to no other, over the targets the safety allowlist already names.
- The runtime asks, on every cycle that decides to act, whether that capability
  would authorize it — and records the answer beside the decision it already
  records.
- Nothing is performed. There is no executor, and this change adds none.

## Impact

- Affected specs: `tunnel-supervision`, `atomic-policy-generations`
- Affected code: `internal/policy`, `internal/policycontrol`, `internal/event`,
  `internal/rootdaemon`
- Not in scope: compiling or installing a generation that grants it. The grant
  is a ceremony with an operator's password and its own evidence, and bundling
  it with the code that introduces the capability would make one act of two.

## What this buys before the switch

Under the generation now active the answer is no, and that is worth recording:
it proves the question is asked, reaches the policy handler, and is refused for
the stated reason. When a generation granting it is installed, the same records
turn to yes without a line of code changing — and if they do not, that is learned
while this runtime still owns nothing.
