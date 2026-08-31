# What the connectivity read model borrowed, and what it refused

Four projects were read while designing the read model. Each is pinned to the
exact commit that was reviewed, because "informed by NetBird" is not a claim
anyone can check a year from now, and the mechanisms below were read out of
code that has since moved on.

None of them is a dependency. Nothing here is vendored, linked or called; what
was taken is a shape, and in every case the shape was changed at the point
where Hexroute's safety properties differ from theirs.

| Project | Reviewed at | What was read |
| --- | --- | --- |
| [firezone/firezone](https://github.com/firezone/firezone/blob/2b4ffb54ec248ca26cb327af2717f7f8801e3b2f/rust/gateway/src/eventloop.rs) | `2b4ffb54` | the policy-driven gateway event loop |
| [netbirdio/netbird](https://github.com/netbirdio/netbird/blob/f2318a8fef230219110c9eeb58ca7f60e247ad98/client/status/status.go) | `f2318a8f` | the normalized client status model |
| [microsoft/agent-framework-go](https://github.com/microsoft/agent-framework-go/tree/421d96b86baf8f0e307a64ce4c63fc1d5b06cd18) | `421d96b8` | the durable checkpoint mechanism |
| [Layr-Labs/chain-indexer](https://github.com/Layr-Labs/chain-indexer/tree/7d774750b49b0d8b527edc2124bb6f248f56d006) | `7d774750` | bounded recovery to a valid ancestor |

## Firezone: the event loop, without the mutation

**Adopted.** Policy drives what the loop is trying to achieve, rather than each
event being interpreted on its own terms.

**Refused.** The direct step from event to network update. A conventional loop
turns each authorization, relay or probe event into an imperative change, which
makes behaviour depend on arrival order and on whatever partial state happened
to be in memory. Hexroute puts a deterministic reduction in between: events
enter an accepted order, the reducer folds them into a snapshot, and what comes
out is a proposal that nothing in this change can execute. The refusal is
enforced by the import boundary — the read model cannot reach `os/exec`, `net`
or any command, plan, lease or credential package — rather than by there
happening to be no caller.

## NetBird: one overview, without one verdict

**Adopted.** A single normalized view an operator can read in one place,
instead of asking each subsystem separately.

**Refused.** Collapsing that view into a single verdict. Every component stays
individually visible with its own state, and the three ways of having no
current answer — `unknown`, `stale`, `conflict` — stay distinct rather than
becoming one "not ok". They call for different actions: nothing was ever said,
what was said has expired, and two things were said. An aggregate that merges
them reads as a host with one problem when it has three different ones.

## Agent Framework: durable checkpoints, bound to a policy generation

**Adopted.** The shape of a durable checkpoint: an immutable identity, a parent
link, and enough recorded input that a later process can resume rather than
begin again.

**Refused.** A checkpoint as a convenience for resuming work. Here it is
evidence, so it binds its parent's digest, the prior input snapshot digest, the
consumed host-sequence range and source watermarks, the exact policy
generations and manifest digest, the reducer identity and version, and the
canonical digests of everything it produced. A checkpoint that cannot be
reproduced from retained facts is a finding, not a slow path — which is the
whole reason the lineage exists.

## Chain Indexer: bounded ancestor recovery, that cannot rewrite authority

**Adopted.** When the newest record does not validate, search backward within a
bound for the newest fully valid ancestor and deterministically replay forward.

**Refused.** Letting that recovery move authority. Ancestor recovery applies to
this observe-only read model and nothing else: it never moves the atomic-policy
active pointer backward and never selects an older authorization generation.
Policy recovery stays monotonic forward convergence.

The other refusal is the fallback. Missing ancestry, a journal gap, a
policy/reducer mismatch, depth exhaustion or an output-digest mismatch all
yield visible `unknown`/`conflict` state. There is no guessed healthy
checkpoint to fall back to, because a plausible healthy snapshot is exactly
what must never be selected — it is indistinguishable from a correct one at the
moment it matters most.

## The shared difference

All four refusals are the same one stated four ways. These are products that
act on what they conclude, and their designs are shaped by needing to act
quickly and to keep acting. Hexroute concludes and stops there, so it can
afford to answer "I do not know" and is required to, because the thing it
protects is a connection that a wrong confident answer would take down.
