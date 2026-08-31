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

## Bounds, and where they are decided

Three layers hold a bound: the acceptor caps retained gap ranges per source,
the reducer caps retained conflict records per source, and the checkpoint store
caps one persisted record. They have to agree, because a restart rebuilds the acceptor
from the snapshot the reducer produced and the store wrote. A snapshot holding
more holes than the acceptor accepts is a checkpoint the host can never resume
from — a lossy source would break its own lineage.

So the acceptor is the single authority on stream integrity. Gaps, overflow,
baseline debt and the conflict count are decided once, there, and the reducer
adopts the decision rather than deriving its own. There is no second opinion to
diverge, and an overflow the acceptor recorded cannot be lost on the way to the
operator or the cloud.

The conflict bound is per source rather than global, so a stream that refuses
constantly cannot crowd out another's evidence. A global bound would evict in
one fixed order, which means it would systematically drop one privilege
domain's conflicts in favour of the other's.

Reaching a bound is reported. Dropped holes, evicted conflict evidence and the
number of streams still owing a restatement all appear in the operator view and
as counts in the cloud projection, because a bound reached in silence reads
exactly like a bound never approached.

## What a baseline closes

A source sequence numbers a source, not a component, and `root.network` speaks
about both physical network and default path. So a hole in that stream leaves
both unaccounted for: nothing in the facts that survived says which of them the
lost ones described.

A baseline for one component therefore settles only its own share. The hole
closes when every component the source speaks about has restated itself in
full, and until then the snapshot names which ones are still owed. The earlier
behaviour — any one baseline closing the whole hole — let a component read
`ready` on the strength of a restatement that was never about it.

## Startup, sleep and recovery

Startup returns only a checkpoint it can prove. Where it cannot, the answer is
unrecoverable and the operator sees unknown state — a plausible healthy
snapshot is exactly what must not be selected. Lineage eviction is bounded and
recorded, so a missing ancestor can be told from one that was dropped.

A full wake or a reboot withdraws the assumption that time-sensitive
observations survived, and only a complete baseline puts it back. Scoped routes
are the exception: a route table does not stop being installed because the
machine slept.

## What the cloud gets, and what it can do with it

The projection travels as an ordinary signed event and lands in the same
`events` table as everything else. A worker pass folds it into a per-node read
model that the dashboard renders; nothing else reads it, and no host ever does.

Two things make that more than an intention. The cloud read model may not
import the acceptor, the reducer or the checkpoint store, so it can only store
what a host concluded and never recompute it — a dependency test asserts this
and would fail if the import appeared. And the stored schema constrains every
text column to the projection's own bounded-token alphabet, so an address, a
path or a digest is unstorable there even if an encoder regressed.

Ordering is the part worth stating plainly. A projection describing an earlier
host position never replaces a stored later one: the pass does not select it,
and the write is guarded besides. A host that could not recover its lineage
restarts its snapshot generation, and that case is stored as a lineage reset
rather than either being refused forever or quietly smoothed over.

Losing the cloud — the API, PostgreSQL, the worker, the dashboard — is one
event from the host's side: an upload that does not complete. Reduction keeps
advancing, proposals keep being produced, and the undeliverable projection
waits in the bounded spool at a priority below retained evidence, so a lost
sample never evicts a baseline.

## Asking what it holds

```
hexroute-connectivity-replay --state --store <root>
```

It reads the newest provable checkpoint and prints the summary, every
component, and every source watermark. It opens no socket and starts nothing,
which is the point: the moment this question matters most is the moment the
daemon is not answering, and every diagnosis worth making on this host has
been made against a store whose daemon was refusing to start.

When nothing can be proven it prints the resume verdict and its reason and
stops there. A zeroed summary would print as no component failing and nothing
stale, which reads as a healthy host rather than as no answer at all.

## Rollback

Stop passing the arguments. The runtime then opens no store, holds no state and
refuses every entry point; the path that ran before this change runs exactly as
it did. Nothing needs to be undone on the network, because nothing was done to
it.

The sequence below depends on nothing that is being rolled back: it edits a
property list and reloads a daemon, and it works whether or not the read model
is functioning, corrupt or wedged.

```
sudo launchctl bootout system/com.hexroute.observe.hexrouted
sudo /usr/libexec/PlistBuddy \
  -c "Delete :ProgramArguments:13" -c "Delete :ProgramArguments:12" \
  -c "Delete :ProgramArguments:11" -c "Delete :ProgramArguments:10" \
  -c "Delete :ProgramArguments:9" -c "Delete :ProgramArguments:8" \
  /Library/LaunchDaemons/com.hexroute.observe.hexrouted.plist
sudo launchctl bootstrap system \
  /Library/LaunchDaemons/com.hexroute.observe.hexrouted.plist
```

The indices are deleted from the end so the earlier ones do not move: eight and
nine are the read-model flag and its root, ten through thirteen the
qualification chain and its session. A daemon installed without a session has
only eight and nine, and the two extra deletions then fail harmlessly — but
confirm what is there before running it rather than after:

```
/usr/libexec/PlistBuddy -c "Print :ProgramArguments" \
  /Library/LaunchDaemons/com.hexroute.observe.hexrouted.plist
```

Nothing else changes. The observation cycle, the heartbeat, the operator socket
and the policy surface are all argument-for-argument what they were, and the
state the read model wrote stays on disk: rolling back is not deleting the
evidence, and a later roll-forward resumes from it or records that it could
not.

Twilight, AdGuard and both Codex paths are untouched by construction rather
than by this procedure. The read model cannot run a command or open a socket —
an architectural test refuses the imports that would let it — so there is
nothing it could have changed about them to undo.
