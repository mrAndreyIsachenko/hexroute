# Tasks

## 1. Record What The Fleet Is For

- [x] 1.1 Add the `ingress-fleet-purpose` capability with the four requirements: recorded purpose per host, redundancy counted in failure domains, provider comparison requiring comparable measurement, and lifecycle documentation distinguishing record from world.
- [x] 1.2 Record each current host's purpose in public documentation without publishing an address, a provider identity or a region that would identify one. Purposes are stated; placements stay private.
- [x] 1.3 Record the purpose that no host serves, so that a gap is owed work rather than an inference from what happens to exist.

## 2. Correct What Is Claimed

- [x] 2.1 Correct the provider-B lifecycle state in `docs/architecture/provider-b-ingress.md` so the current state is the deployed one, keeping the distinction between what the public record proves and what is deployed, and publishing nothing that proves it.
- [x] 2.2 Rewrite roadmap item 4. Its deployment half is delivered; its bake-off half has no second candidate and no written criterion. Name what remains instead, and record what was ruled out so the next reader does not re-derive it.
- [x] 2.3 Record that no Hexroute-side evidence attributes ingress availability to a provider, and why the measurement that exists is not admissible for it.

## 3. Keep The Boundary

- [x] 3.1 Assert that this change provisions nothing: no Terraform root, module, launchd unit or runtime configuration is added or altered, and no host is created. The diff is three documents, one gate and one Makefile line; the count of Terraform, launchd and installer files it touches is zero.
- [x] 3.2 Assert that no live address, provider identity, region or deployment evidence enters the public repository, including in the documentation added by section 1. `tests/ingress_documentation_test.sh` enforces it and caught the first violation in my own text: the adoption paragraph used a provider-specific word for a compute instance.
- [x] 3.3 Confirm the roadmap drift gate still passes: the corrected roadmap must continue to agree with the open changes and the unwired list.

## 4. Verify

- [x] 4.1 Run `make check` and resolve every failure.
- [x] 4.2 Run `openspec validate record-ingress-fleet-purpose --strict` and keep proposal, design, specs and tasks consistent with what was written.
- [x] 4.3 Sync the delta into the baseline specs and archive the change.
