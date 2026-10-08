# Tasks

## 1. The ground

- [x] 1.1 Read the live fact and the configured roles together, so the quantity is explained rather than suspected; verified by the two readings agreeing arithmetically.

      Read 2026-10-08T21:25:13Z. `scoped_routes` is `degraded`,
      `probe_failed`, `{configured: 21, conflicting: 14, installed: 7}`, and
      every other component is `ready`: physical network, default path, managed
      transports, relay ingress, user access, session expiry.

      The configured roles are 1 corporate, 1 `gitlab_https`, 5 inherited, 2
      ingress and 12 `codex_fallback`. `desiredPath` sends the first three to
      the tunnel — 7 — and the rest elsewhere or nowhere — 14. The fact's two
      numbers are exactly those two groups, so `conflicting` counts correct
      placement.

- [x] 1.2 Establish that no other mapper does the same thing, so the change is sized by a reading; verified by what each one compares.

      `MapScopedRoutes` is the only mapper given an interface to compare
      against. `MapDefaultPath` reports a path class, `MapTransports` counts
      processes, `MapRelays` counts reachability. The live facts agree: all
      three read `ready` on the same cycle this one read `degraded`.

- [x] 1.3 Establish that the plan already distinguishes the states the fact needs, so the rule is not restated; verified by the planner's own vocabulary.

      `ReasonMissingRoute`, `ReasonWrongPath`, `ReasonFallbackRequired`,
      `ReasonFallbackRestored`, across `OperationEnsureHostRoute` and
      `OperationRemoveOwnedHostRoute`. The fact can be a count of the plan's
      output.

      `plannerIntents` already carries the plan to the read model but flattens
      it to two booleans for the whole component, so nothing per-route survives
      the trip today.

## 2. The quantity

- [ ] 2.1 Add `missing` to the payload and narrow `conflicting` to a route on the wrong link; verified by a test over a plan holding one of each.

- [ ] 2.2 Keep `configured` meaning the routes the configuration declares; verified by a test that it is unchanged while the other three move.

- [ ] 2.3 Count a route the configuration does not ask for as neither present nor diverging; verified by a test with twelve such routes and a payload that counts none of them.

- [ ] 2.4 Count a route pending withdrawal as conflicting; verified by a test over `remove_owned_host_route`.

      Reasoned rather than measured: it does not occur on this machine. The live
      plan holds two operations, both `ensure_host_route` with `wrong_path`, and
      withdrawal needs a route this runtime owns, which in `observe-only` it
      does not.

## 3. Ready is reachable

- [ ] 3.1 Make the ready condition `conflicting == 0 && missing == 0`; verified by a test over a configuration whose roles span three links and whose routes are all where they belong.

- [ ] 3.2 Prove the live arrangement still reads degraded, for two routes and not fourteen; verified by a test built from this machine's role counts.

- [ ] 3.3 Report `unknown` rather than a count of zero when the cycle reached no plan; verified by a test over a cycle that failed before planning.

      A count of zero would read as "nothing diverges", which is the opposite of
      "nothing was measured".

## 4. Carrying the plan

- [x] 4.1 Put the role-to-link rule where both the planner and the read model may read it; verified by the planner's whole existing suite passing unchanged and by a test holding the two in correspondence.

      Rewritten twice, each time by an invariant already in the repository.

      It first said to carry the plan's per-route verdicts to the read model.
      `Evidence` forbids that in its own words: nothing in it is derived,
      because the read model is meant to reach its own conclusions from the same
      readings rather than inherit the daemon's.

      It then said to export the rule from `routeplan`. The boundary guard in
      `internal/connectivityreduce` forbids that: `routeplan` is a mutation
      package — one that "can actually change the host or mint the authority to
      do so" — and nothing holding a reconciliation proposal may import one. The
      read model holds proposals. The guard's own comment says it becomes
      load-bearing when the daemon integration lands, which is now.

      So the decision moved below that line, into `internal/routeplace`:
      **deciding where a route belongs is not the authority to put it there.**
      `routeplan` imports it, keeps type aliases so its four callers in
      `rootdaemon` are untouched, and `Build` consumes `routeplace.Decide`. The
      read model imports `routeplace` and nothing that can change the host.

      Behaviour is unchanged: the planner's whole test suite passes as written,
      and `TestThePlanAndTheDecisionAgreeStateForState` holds the
      correspondence — every state the decision calls conflicting or missing has
      an operation, every state it calls installed or unasked has none, over a
      fixture producing four states at once.

      `tests/route_coverage_roles_test.sh` read the roles from the moved file
      and refused with "this gate is not looking at anything" rather than
      passing on an empty read. It now points at `routeplace`.

- [x] 4.2 Give `Evidence` the configuration the judgement needs; verified by the read model reaching its own judgement from it.

      `Evidence.Routing` carries the inputs the cycle assembled for the rule:
      the targets with their roles and preferred links, the three links, the
      routes the host has, and the two Codex probe results. Configuration and
      readings, not a conclusion — `ConfiguredRoutes` was already there on that
      footing. The cycle keeps the value it planned against, so the fact and the
      plan judge the same inputs rather than two cycles' worth.

      `Reader.placement` calls `routeplace.Place` on it. A cycle that did not
      reach the inputs yields `ErrNotJudged` rather than an empty judgement.

- [x] 4.3 Prove the rule is not restated: the collector holds no role-to-link decision of its own; verified by its signature.

      `MapScopedRoutes(placement, err)` is given a judgement and no route, no
      interface, no role and no target. It decides a lifecycle and a reason from
      the counts and can decide nothing about where a route belongs.

      `TestEachRoleIsSentToTheLinkItAsksFor` holds the rule itself: every role
      goes to the link its configuration names, the ingress roles never to the
      tunnel, and nine of this machine's twenty-one targets are asked for at
      all.

## 5. What an operator reads

- [x] 5.1 Say in the connectivity read-model reference what each of the four quantities means, when `conflicting` narrowed and what it counted before; verified by reading, because the gate cannot check this.

      The verification in this task was wrong and is corrected rather than
      weakened quietly. `tests/connectivity_documentation_test.sh` checks
      component names, component states, authorization values, diff
      classifications, diff reasons, proposal classes, sources and installed
      arguments. It does not check payload field names, so a new quantity can be
      added without the document following — the same hole this change is about,
      one level up.

      Extending it is not this change's work, because **20 of the 31 payload
      fields are explained in neither document**: `has_carrier`, `link_up`,
      `path_class`, `reachable`, `sessions` and fifteen more. That is a debt of
      its own, recorded in 7.5.

      And a naive extension would be a weak gate. The check is `grep -qF` for a
      backtick-quoted word anywhere in the document, and vocabularies collide:
      `missing` already appears as a **diff reason** — "policy asks for it and
      it is not there" — so a payload field named `missing` would have passed
      for the wrong reason. I measured it passing that way before noticing.

      The reference now carries a `### The scoped routes payload` section with
      the four fields, the ready condition, what changed on 2026-10-09 and what
      `conflicting` counted before it, and that a reader comparing across that
      date is comparing two different measurements.

- [x] 5.2 Record that two routes on the wrong links are the tunnel owner's arrangement and not this runtime's to correct, so a reader does not take the remaining 2 for a new fault.

      In the same section: a residue of 2 is expected, it is the arrangement the
      runtime that owns the tunnel enforces, read on 2026-09-25 and unchanged
      since.

## 6. Mutation discipline

- [x] 6.1 Mutate the four counts, the classification and the ready condition; verified by every survivor closed by a test or recorded with the reason it was left.

      Fifteen mutations over `internal/routeplace` and the fact's mapper, run
      against six packages with a 180-second bound, each file restored and
      compared byte for byte before the next. **15 killed, 0 survived.**

      Five over the classification: a route asked for nowhere reading as
      installed, an owned unwanted route going unnoticed, the link not compared
      at all, misplaced reading as absent, and absent reading as misplaced. The
      last two matter most — they are the pair the fourth quantity exists to
      keep apart.

      Five over the counting: an unasked route counted installed, an unwanted
      one not counted, absent ones not counted, the ambiguity check dropped and
      the inputs not validated.

      Five over the fact: the ready condition back to requiring every declared
      route, ready ignoring absent routes, ready ignoring misplaced ones, an
      unjudged cycle reading as a clean result, and the absent count not
      published.

      One did not compile and was rewritten rather than counted: adding
      `StateUnasked` to the installed case left the later case duplicated. As
      one edit over the whole switch it compiles, and it dies.

## 7. Close

- [ ] 7.1 Run the full gate and report any gate that did not run and why; verified by the gate's exit status, not by reading its output.

- [x] 7.2 Prove the rollback: the previous binary reads a payload carrying the new field; verified by running it against one, since the payload has a validator and this cannot be assumed.

      It does not read it. Measured 2026-10-09 by encoding a fact with
      `missing: 1` from this branch and decoding it with `origin/main`'s own
      `connectivity.Decode` in a worktree:

      ```
      REFUSED: connectivity fact encoding is invalid: json: unknown field "missing"
      ```

      That is by design, not by accident: the decoder refuses unknown fields
      because two encodings of one fact would carry two digests, and the digest
      is the identity the aggregate deduplicates on. On the archive's read path
      such a record becomes `ErrInvalidField`.

      So rollback leaves records written after this change unreadable to the
      previous binary. They are not lost, and roll-forward reads them again. The
      cross-domain publication path is untouched: `scoped_routes` is root-owned
      and the user daemon never emits it.

      A fact schema version was considered and not taken; design.md records why,
      including that it would make the refusal less informative than the field
      name already is.

- [ ] 7.3 Read the fact on the machine after an install; verified by a reading that gives all four quantities and the lifecycle.

- [ ] 7.4 Sync the delta into the baseline, validate and archive; verified by the drift gate.

- [ ] 7.5 Record what this leaves open.
