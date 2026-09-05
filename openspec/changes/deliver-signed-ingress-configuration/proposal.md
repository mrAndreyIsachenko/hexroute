# Deliver signed configuration to an ingress, and let it come back

## Why

The provider-B ingress starts only if `/etc/hexroute/runtime/xray.json` already
exists: its unit carries `ConditionPathExists` on that path, and nothing in the
Terraform module puts it there. Runtime files are deliberately outside
Terraform. So today the only way that file arrives, or changes, is a person
opening bounded operator access from a declared `/32`, editing the host, and
closing the access again — the very thing the provider-B specification asks to
be temporary.

That leaves the ingress with no way to be changed safely and no way to be put
back. Roadmap item 5 asks to add MTProto to that host; it cannot be done,
because there is nothing to deliver it with. The roadmap orders 5 before 6, and
that order is wrong: item 6 is the mechanism item 5 needs.

Half of this is already designed and dead. `config_versions` exists in the
schema with a target, a version label, a content digest, a signing key
reference and the lifecycle `staged → active → proven → retired/rejected`;
`deployments` sits beside it; the dashboard already joins them. Nothing writes
a row to either. The model was drawn and never given a producer.

The proving half is already alive, in the other direction. The ingress observer
signs its state with the node key and reports the exact deployment generation.
A version does not need a new mechanism to be called proven — it needs to be
connected to the one that already answers that question.

## What Changes

- Sign a configuration version with the operator key, under user presence, the
  way a policy generation is signed. Neither a daemon nor a cloud component can
  mint one.
- Record the version in `config_versions` and store its bytes in the private
  object store, the shape incident bundles already use: a row that names an
  object and its digest.
- Let the ingress pull, verify and apply on its own. It checks the signature
  against a public key placed at build time and the digest against the bytes,
  and it applies nothing it cannot verify. The cloud gains no node-facing
  surface and issues no instruction; the boundary that says the cloud cannot
  request a local mutation is kept by construction rather than by rule.
- Keep the previous version and return to it when the new one does not prove.
- Establish `proven` from the signed heartbeat already reporting the deployment
  generation, rather than from the delivery having completed.

## Non-Goals

- Delivering anything to the local machine. Policy generations already sign,
  activate and roll back there; a second mechanism beside them would be a
  rebuild of working machinery, and the ingress is the side with nothing.
- Adding MTProto, an Nginx SNI split, or a second provider. Those are item 5,
  and this exists so that they become possible.
- Granting the cloud any authority. It stores and it records; it does not send.
- Removing bounded operator access. This makes it unnecessary for configuration
  changes; recovery from a host that cannot pull is still a person with SSH.

## Impact

The cloud gains a producer for two tables it already has and a dashboard view
that already renders them. The ingress host gains a pull-and-verify agent and a
second configuration slot. No local daemon changes, and no runtime on the
operator's machine is touched.

## Rollback

Two layers, and they are independent. A configuration version that does not
prove is returned by the host itself, which is the feature. The change as a
whole is reverted by removing the agent from the ingress image and reverting
the commit: with no agent, the host reads its configuration exactly as it does
today, from the file a person put there.

## Ownership boundary

This change belongs in public Hexroute for the agent, the version format and
the verification rules. The private infrastructure repository owns the live
bucket, the credential that reads it, the operator public key placed at build
time and the deployment evidence. Twilight is unaffected: it remains the
production owner of its own fleet and is not a consumer of any of this.
