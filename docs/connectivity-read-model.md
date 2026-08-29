# Observable connectivity read model

The read model turns what the daemons already observe into one causal view of
host connectivity. It describes; it cannot act. There is no executor in this
change, and the boundary is asserted by tests rather than by intent.

## What the gate requires

`connectivityruntime` refuses to start unless four policy-foundation contracts
are established. They are passed in as explicit claims rather than probed, so
enabling the read model is a decision someone recorded, not something that
happened because a check passed at startup.

| Precondition | What it means | Where the evidence lives |
| --- | --- | --- |
| `AtomicPolicyStartup` | Startup revalidates the active policy generation before anything reads it | `openspec/specs/atomic-policy-generations`, startup revalidation requirement |
| `DomainMismatch` | A domain mismatch is refused rather than resolved | `internal/policy` action authorization: `PolicyDomainMismatch`, `ReasonDomainMismatch` |
| `Suspension` | An authorization suspension is honoured by every consumer | `internal/policy` `AuthorizationSuspension`, `PolicyAuthorizationSuspended` |
| `RedactedStatus` | Local status output is already bounded and redacted | `openspec/specs/cloud-telemetry`, telemetry-only authority requirement |

A false precondition names exactly which contract is missing. A disabled
runtime asks for none of them, because it does nothing.

## What it cannot do

The whole read model — every `internal/connectivity*` package — is forbidden
from importing `os/exec`, `net`, `net/http`, and the command, action-lease,
action-plan, route-plan, Pritunl-plan, Pritunl-rescue, resume-executor and
credentials packages. AdGuard, Codex, Pritunl and Twilight can only be changed
by running something or opening a socket, so this is a stronger statement than
checking their state afterwards: it holds for paths nobody thought to check.

It also cannot reach the policy store. It consults an already-compiled
descriptor, so recovering an older snapshot can never become authorization of
an older policy.

A test asserts that any new `internal/connectivity*` package joins that
boundary list, so the guard cannot be escaped by adding a package.

## What it does not observe

Two gaps are recorded rather than hidden, because every component is always
present in the snapshot and a silent one reads as `unknown`:

- **DNS.** Hexroute has no DNS observer at all. The component has a declared
  owner and no way to speak, so it reports `unknown`.
- **Session expiry.** Nothing observes how long a session has left. The user
  collector reports session presence and never claims `expiring` or `expired`;
  saying otherwise would be inventing a measurement.

## Startup, sleep and recovery

Startup returns only a checkpoint it can prove. Where it cannot, the answer is
unrecoverable and the operator sees unknown state — a plausible healthy
snapshot is exactly what must not be selected. Lineage eviction is bounded and
recorded, so a missing ancestor can be told from one that was dropped.

A full wake or a reboot withdraws the assumption that time-sensitive
observations survived, and only a complete baseline puts it back. Scoped routes
are the exception: a route table does not stop being installed because the
machine slept.

## Rollback

Set the gate to off. The runtime then opens no store, holds no state and
refuses every entry point; the path that ran before this change runs exactly as
it did. Nothing needs to be undone on the network, because nothing was done to
it.
