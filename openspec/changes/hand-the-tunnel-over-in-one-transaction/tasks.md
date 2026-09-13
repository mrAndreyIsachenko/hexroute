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

- [x] 3.1 The durable session record: phase, the previous owner's state, what this runtime started.

      The phase is recorded before the act it names, not after. A transaction
      that fell over between acting and recording would leave the machine
      changed by a phase no record mentions, and the abort that came later would
      not undo it.
- [x] 3.2 Refuse to start a transaction beside one already in flight.
- [x] 3.3 Complete on two consecutive proofs of traversal inside the deadline.

      Consecutive rather than cumulative. A path that traverses, fails and
      traverses again has not been shown to hold, and counting cumulatively
      would complete the handover on exactly the evidence the link cause was
      completing on when it was wrong six times in half an hour.
- [x] 3.4 Abort on the deadline: remove the claim, stop what was started, let the supervisor take it back.
- [x] 3.5 A later invocation can abort what it finds.

      `hexroute-handover abort` reads the phase a closed terminal left behind and
      undoes exactly it: the claim if one was placed, the process if one was
      started. Aborting when nothing is in flight is not an error, because an
      abort runs when the state is uncertain and must be able to run twice.

## 4. Starting the tunnel from a signed version

- [x] 4.1 Verify at every start, against this host, without reaching the network.

      At every start rather than once at installation: verifying once would let
      anyone able to write the file afterwards choose the bytes that carry every
      packet this machine sends. The verified content is rewritten from the
      artifact each time, so a file edited between starts is replaced rather
      than obeyed.

      Nothing reaches the network to do it. A host needs its tunnel
      configuration exactly when it has none, and a verification that had to
      fetch a key would fail in the one case it exists for.

      The key is the one this host already pins for policy. Signing a
      configuration a host will run is the same authority as signing a policy
      generation, and a second key would be a second thing to keep safe for no
      gain — which is why `configversion.Signer` is the policy signer's method
      set and not a type of its own.
- [x] 4.2 Refuse to start on a version that does not verify, and return ownership.

      The refusal names which check failed. "It did not verify" is the answer
      that sent a reader looking at the wrong half of a system more than once in
      this repository's history.

      Refused: signed by another key, meant for another host, content changed
      after signing, not a version at all, and absent. In every case nothing is
      started and nothing is written for a process to read, so a refusal leaves
      no half-prepared configuration behind for the next attempt to find.
- [ ] 4.3 Publish the first version from the bytes running today, and prove it byte for byte. Signed with the operator's key; the one step nobody else can take.

## 5. The rehearsal

- [ ] 5.1 Every phase except the claim and the start, reported as a rehearsal.
- [ ] 5.2 Run it on the machine and record what it found.

## 6. Gates and evidence

- [ ] 6.1 Mutate the claim, the completion rule, the abort and the verification; confirm the named tests fail.
- [ ] 6.2 `make check` green.
- [ ] 6.3 Run the real transaction and record what the tunnel did.

## 7. Close

- [ ] 7.1 Sync the delta into the baseline, validate, archive.
