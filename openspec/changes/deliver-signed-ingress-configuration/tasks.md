# Tasks

## 1. Publish A Version

- [ ] 1.1 Add a configuration version format whose canonical content and digest are signed together, so a signature cannot be moved onto different bytes.
- [ ] 1.2 Sign with the operator key under user presence, reusing the policy signing path. Assert that no daemon, worker or build can produce a version, and that the refusal is for want of the key rather than a fallback to something unsigned.
- [ ] 1.3 Store the bytes privately and record the version in `config_versions` with target, label, digest and signing key. Both tables have existed without a producer since the schema was written; this is the producer.

## 2. Receive, Verify, Apply

- [ ] 2.1 Add the ingress-side agent that obtains a version by its own action. The cloud gains no node-facing route, so the boundary holds by construction.
- [ ] 2.2 Verify signature against the public key placed at build time and digest against the received bytes, before applying. Assert that bytes from the expected location with no valid signature are refused, so the delivery path is never accepted as evidence of authenticity.
- [ ] 2.3 Retain the version currently running, and apply the new one so that returning to the retained one needs no network.
- [ ] 2.4 Continue serving the current configuration when a version is refused, and record which check failed.

## 3. Prove Or Return

- [ ] 3.1 Establish `proven` from the signed heartbeat reporting that deployment generation healthy for a bounded window. Assert that clean application and reachability do not establish it.
- [ ] 3.2 Return to the retained version when the window passes without a healthy heartbeat for the new generation, and record the reason it was not proven.
- [ ] 3.3 Record the deployment in `deployments` so the dashboard view that already joins it renders something.

## 4. Keep The Boundary

- [ ] 4.1 Assert that the cloud gains no route by which it can tell a node to change, and that a node offline during a publish is left with no queued instruction.
- [ ] 4.2 Assert that no live bucket name, credential, host address or operator public key enters the public repository.
- [ ] 4.3 Assert that nothing on the local machine changes: no policy generation path, no daemon and no runtime configuration is touched by this change.

## 5. Verify

- [ ] 5.1 Run `make check` and `make postgres-test` and resolve every failure.
- [ ] 5.2 Run `openspec validate deliver-signed-ingress-configuration --strict` and keep proposal, design, specs and tasks consistent with what was built.
- [x] 5.3 Swap items 5 and 6 in the roadmap with the reason, and record what the grill ruled out so the next reader does not re-derive it. Done when the change was planned rather than after it was built: the roadmap must not state an order the project has already decided against.
- [ ] 5.4 Sync the delta into the baseline specs and archive the change.
