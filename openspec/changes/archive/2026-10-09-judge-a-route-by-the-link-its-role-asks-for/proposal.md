# Judge a route by the link its role asks for

## Why

The `scoped_routes` fact counts a route as conflicting whenever it is not on the
managed tunnel. The configuration assigns routes to **three** links by role, and
two of its roles must never be on the tunnel, so the fact counts correct
placement as conflict.

Read from the machine 2026-10-08T21:25:13Z:

```
scoped_routes  degraded  probe_failed  {configured: 21, conflicting: 14, installed: 7}
```

Every other component is `ready` — physical network, default path, managed
transports, relay ingress, user access, session expiry. Only this one is not,
and the numbers say why. The 21 configured routes are 1 corporate, 1
`gitlab_https`, 5 inherited, 2 ingress and 12 `codex_fallback`. `desiredPath`
sends corporate, `gitlab_https` and inherited to the tunnel — **7** — sends
ingress to the physical interface or the upstream VPN and never to the tunnel,
and asks for no `codex_fallback` route at all while normal Codex is reachable.

So `installed: 7` is exactly the routes whose role asks for the tunnel, and
`conflicting: 14` is exactly the routes whose role asks for something else. The
quantity is not a measure of divergence. It is a count of how the roles are
distributed, published under a name that reads as a fault.

And it cannot be anything else: `ready` requires `installed == configured`, so a
configuration with any non-tunnel role can never reach it. The component has
reported `degraded` in every cycle it has ever run, including the cycles the
runtime reports as sound, and a reader comparing component health against cycle
health finds them permanently disagreeing for no reason.

This is not the conflation that `tell-pending-work-from-a-fault` removed. There,
the logic was right and the label lied. Here the quantity is computed against an
expectation the configuration never set.

## What Changes

- A route's placement SHALL be judged against the link its own role asks for,
  not against one link chosen for all of them.
- A route the configuration does not ask for at all SHALL NOT be counted as
  either placed or conflicting. `codex_fallback` is the case: while normal Codex
  is reachable the planner asks for no such route, and twelve of them are
  presently counted as conflicts.
- `ready` SHALL be reachable for a configuration whose roles span several links.
- **Not fixed here:** the host's two ingress routes stand on the opposite links
  from their `preferred_link`. That is the tunnel owner's arrangement, recorded
  since 2026-09-25, and after this change it will be visible as what it is —
  two routes misplaced out of twenty-one, rather than hidden inside fourteen.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `observable-connectivity-state-machine` — it owns what a connectivity fact
  describes and what the snapshot preserves. A fact about a configured
  component gains the requirement that it is computed against the expectation
  that component's own configuration sets.

## Impact

- `internal/connectivitycollect/root.go` — `MapScopedRoutes`, the only mapper
  that compares against a single interface. The others count processes,
  reachability or path class, so this change is confined to it.
- `internal/connectivityhost/host.go` — what it passes to that mapper: the
  managed tunnel's name today, the roles and their links after.
- `internal/routeplan` — read for the role-to-link rule, not changed. The rule
  is `desiredPath`'s and must not be restated.
- `internal/connectivity` — the payload may need a third quantity for routes
  the configuration asks for and the host does not have, which today is
  indistinguishable from a route that is simply absent.
- The read model's aggregate, which reports `degraded` on this component alone,
  and `connectivity-watch.json`, which reports the same.
- No route moves, nothing is installed, and the runtime stays `observe-only`.

## Not in this change

`open_gaps` in `connectivity-watch.json` read 0 on 2026-10-05 and 2 on
2026-10-08. Nothing here explains that and nothing here touches it; it is
recorded so the next reader does not take it for a consequence of this work.
