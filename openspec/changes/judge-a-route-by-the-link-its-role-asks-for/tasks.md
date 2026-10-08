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

- [ ] 5.1 Say in the connectivity read-model document what each of the four quantities means, when `conflicting` narrowed and what it counted before; verified by the documentation gate.

- [ ] 5.2 Record that two routes on the wrong links are the tunnel owner's arrangement and not this runtime's to correct, so a reader does not take the remaining 2 for a new fault.

## 6. Mutation discipline

- [ ] 6.1 Mutate the four counts and the ready condition; verified by every survivor closed by a test or recorded with the reason it was left.

      A mutation that does not compile is rewritten, not counted.

## 7. Close

- [ ] 7.1 Run the full gate and report any gate that did not run and why; verified by the gate's exit status, not by reading its output.

- [ ] 7.2 Prove the rollback: the previous binary reads a payload carrying the new field; verified by running it against one, since the payload has a validator and this cannot be assumed.

- [ ] 7.3 Read the fact on the machine after an install; verified by a reading that gives all four quantities and the lifecycle.

- [ ] 7.4 Sync the delta into the baseline, validate and archive; verified by the drift gate.

- [ ] 7.5 Record what this leaves open.
