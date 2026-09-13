# Tasks

## 1. Prove the ground before moving anything

- [x] 1.1 Compare this runtime's targets against what both of the supervisor's route scripts apply, and record any destination only the supervisor knows.

      Compared on the machine 2026-09-13, against the installed configuration of
      both runtimes rather than either repository.

      The supervisor applies seven destinations through the tunnel, none around
      it by address, and none by name — so the route-by-name gap this change
      feared does not exist on this machine. This runtime plans sixteen by
      address.

      Five of the supervisor's seven are unknown to this runtime, and all five
      go through the tunnel. After a handover they would stop being routed. That
      is the gap, and it is the one thing that must close before the transaction
      can run.

      The other direction is safe: fourteen are known only to this runtime —
      twelve `codex_fallback`, which are routed only while the normal Codex path
      is down, and two ingress. It does more, not less.

      The two both know are `corporate` and `gitlab_https`, and they agree: both
      runtimes put them through the tunnel.

      The first reading said those two disagreed, and it was wrong. The
      instrument read `preferred_link` from the configuration, where the planner
      decides by role — `routeplan.desiredPath` sends corporate and GitLab HTTPS
      to the tunnel unconditionally and `validateTarget` refuses a preference for
      them at all. So it invented a leak of corporate traffic out of the tunnel.
      The comparison asks the planner's rule now.
- [ ] 1.3 Close the gap: give this runtime the five destinations only the supervisor routes, and prove the comparison is empty.

      What they are could not be established from the machine, and the attempt
      is recorded because it bounds what anyone else could learn later.

      None of the five appears in any commit of the supervisor's repository, on
      any branch: they were added by hand to the installed environment. No other
      variable names them and none resolves to them. Four are 151.101.2.133,
      151.101.66.133, 151.101.130.133 and 151.101.194.133 — Fastly anycast, one
      service behind four edge addresses — and an anycast address cannot be
      attributed from a host, because it serves every customer that CDN has.
      The fifth is a single host. All five route through the tunnel, confirmed
      against its address rather than its interface number.

      Two attempts to identify them by probing were abandoned, and the reason is
      worth keeping. A completed TCP connection through this tunnel proves the
      tunnel accepted, not that a host answered, so the port scan returned three
      different answers on three consecutive runs. This repository already has a
      requirement saying a reachable socket is not qualification; the probe was
      built on the evidence that requirement rejects.

      So they are carried across rather than resolved. A new role, `inherited`,
      says what is known — routed through the tunnel by the previous owner,
      purpose unrecorded — instead of calling them corporate, which would be a
      guess written into every record that mentions them. The handover changes
      who owns the tunnel and nothing about what it carries; changing both at
      once would make any later breakage impossible to attribute.

      Whether they are still needed is left to evidence rather than to silence:
      three minutes of watching saw no packet, which is what a route used once a
      day also looks like, so the watch runs for a day.
- [ ] 1.4 Read the day's watch and decide whether the inherited destinations stay.
- [ ] 1.2 Publish the first signed configuration version from the bytes running today, and prove it is byte for byte identical.

## 2. The claim

- [x] 2.1 Write the claim: its shape, where it lives, who may write it.
- [x] 2.2 Have the supervisor read it every tick and before every start, and not start the process while it is held.

      Four acts consult it, and they are the ones that rebuild the tunnel:
      starting sing-box, the process going missing, the carrier changing, and
      the payload path failing. The restart on a restored outer path consults it
      too, though it is switched off on this machine.

      An earlier draft skipped the whole tick while the claim was held, which
      would have dropped the six behaviours the supervisor keeps — the ingress
      selection among them, at 91 changes in 61 days. Found by reading it back
      before running it.
- [x] 2.3 Test that a supervisor restarted while the claim is held does not take the process back.

      The test exercises the supervisor's own function rather than a copy, and
      it caught two faults in itself before it caught anything in the change: it
      scanned a function body to an indented closing brace, where one of these
      closes at column zero, and it looked for a behaviour by name where the
      name also appears in its own declaration. Both let a mutation through
      unnoticed.

## 3. The transaction

- [ ] 3.1 The durable session record: phase, the previous owner's state, what this runtime started.
- [ ] 3.2 Refuse to start a transaction beside one already in flight.
- [ ] 3.3 Complete on two consecutive proofs of traversal inside the deadline.
- [ ] 3.4 Abort on the deadline: remove the claim, stop what was started, let the supervisor take it back.
- [ ] 3.5 A later invocation can abort what it finds.

## 4. Starting the tunnel from a signed version

- [ ] 4.1 Verify at every start, against this host, without reaching the network.
- [ ] 4.2 Refuse to start on a version that does not verify, and return ownership.

## 5. The rehearsal

- [ ] 5.1 Every phase except the claim and the start, reported as a rehearsal.
- [ ] 5.2 Run it on the machine and record what it found.

## 6. Gates and evidence

- [ ] 6.1 Mutate the claim, the completion rule, the abort and the verification; confirm the named tests fail.
- [ ] 6.2 `make check` green.
- [ ] 6.3 Run the real transaction and record what the tunnel did.

## 7. Close

- [ ] 7.1 Sync the delta into the baseline, validate, archive.
