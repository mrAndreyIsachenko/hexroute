# Design

## Why this comes before item 5

Item 5 asks to add MTProto to the provider-B ingress. The grill found that it
cannot be started, not because MTProto is hard but because the host has no way
to receive a changed configuration. Its unit refuses to start without
`/etc/hexroute/runtime/xray.json`, and Terraform deliberately does not write it.
The single existing path is a person with temporary SSH.

So the roadmap's order is inverted, and the correction is cheap to state: item 6
is what item 5 needs. Doing 5 first would mean delivering it by the mechanism
this project is trying to retire, and without a way back.

## What was already there, and what it changed

`config_versions` and `deployments` exist in the schema with the full vocabulary
— target, label, digest, signing key, `staged → active → proven →
retired/rejected`, activation and proving timestamps — and the dashboard already
joins them. Nothing has ever written a row.

That is the fourth thing this project has found designed and never given a
producer. It is worth saying that the model survived the review: nothing in it
had to be redrawn to carry this change, which is some evidence the original
design was right and merely unfinished.

The proving half was already alive in the other direction. The ingress observer
signs its state with the node key and reports the exact deployment generation,
and the probe validates that generation is the expected one. So `proven` needed
no new mechanism, only a connection. That is why this change does not add a
notion of success: it borrows the one that already exists and refuses to accept
a weaker one.

The connection turned out to need one thing the fleet did not have. The
observer reported the generation it was built with, which is fixed for the life
of the host, so every version would have proved the moment the host was
reachable. The agent now records the version it applied and the observer
reports that, and an observer that cannot read it answers nothing at all — a
heartbeat naming the build-time generation would prove the wrong thing, and
there is no safe fallback from not knowing what you are running.

## Why the node pulls

The alternative was a cloud endpoint that serves versions to nodes. It was
rejected for a reason stronger than taste: Hexroute's boundary says the cloud is
telemetry-only and cannot request a local mutation, and the cloud currently has
exactly one node-facing route, which is inbound. Adding an outbound one would
leave that boundary intact only as long as the rule is remembered.

With a pull from private storage, the boundary holds because there is nothing to
send with. The cloud keeps a ledger of what exists; it has no way to tell a host
to use it.

## Why the operator key

The same property is wanted as for a policy generation: only a present human may
change what runs. Policy already states it and enforces it — the key lives in
the Keychain, requires presence, and daemons may not hold it or mint
generations.

A separate configuration key was considered. It buys domain separation and costs
a second key to rotate, store and lose, and there is no threat here that the
separation answers: whoever can sign a policy generation can already change what
the local machine enforces. The signing rule is not weakened to make delivery
convenient, which is the failure mode a build-time signing key would introduce.

## What is deliberately not decided here

How the node authenticates to the store, how it learns a version exists, and
what its bounded proving window is are implementation choices inside this
change, not commitments in the specification. The requirements name what must
be true — verified before applied, proven by the running generation, returned
when it does not prove — and leave the mechanism free, so that a later provider
whose store or credentials differ does not require the specification to change.

## Boundary

Public Hexroute owns the agent, the version format and the verification rules.
The private infrastructure repository owns the bucket, the read credential, the
operator public key placed at build time and the deployment evidence. Twilight
is untouched and is not a consumer: it remains the production owner of its own
fleet, and this changes nothing it runs.
