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
- [x] 1.3 Close the gap: give this runtime the five destinations only the supervisor routes, and prove the comparison is empty.

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

      Adopted 2026-09-13 by a script that runs on the machine, so the addresses
      stayed there: five added as `inherited-01` through `inherited-05`, twenty-one
      routes in all, and the runtime accepts the configuration. The comparison
      that found the gap now reports none.

      It reported five disagreements on that same run, and they were not real.
      The comparison lived in a scratch directory and did not know the role that
      had just been added to the planner, so it could not place those five on
      either side of the tunnel and said they differed. It lives in the
      repository now, and `tests/route_coverage_roles_test.sh` fails when the two
      lists of roles part company — which is the fault it would have caught on
      the run that was meant to prove the handover safe.
- [ ] 1.4 Read the day's watch and decide whether the inherited destinations stay.
- [x] 1.2 Publish the first signed configuration version from the bytes running today, and prove it is byte for byte identical.

      The sequence is written down at `docs/macos/tunnel-configuration-version.md`.
      It is the operator's to run: the key requires user presence in their own
      session, and a signer that did not would be a key anything running as root
      could use.

      One exposure is named there rather than left to be discovered. The
      configuration lives under root and carries the tunnel's identities;
      signing needs those bytes in the session that can reach the Keychain item.
      So they are readable by the operator's account between the copy and its
      removal, in a `0600` file in the operator's own storage, and by nothing
      else.

      The proof is a digest comparison rather than a diff, so the bytes stay off
      the terminal: the content the signature covers must equal the file the
      supervisor still runs. Anything but identical means the first version is
      not what the machine runs, and the handover would be changing two things
      at once.

      A preflight checks everything the ceremony reads before it touches the
      Keychain, because a run that fails halfway leaves the configuration
      readable by the operator's account and nothing signed.

      It found that this document named the wrong key. There has been more than
      one signer on this machine, and the one with the name the document used is
      not the one the host pins — the path came from an example in
      `policy-signing.md` rather than from the machine. A wrong key is
      discovered at the moment of signing, after user presence.

      So the key is found by the fingerprint the host pins, by
      `scripts/ops/find-pinned-signer.sh`, which prints one path or nothing and
      refuses when two match rather than choosing. The preflight itself was
      wrong first as well: run under sudo it looked in root's home for files
      that live in the operator's, and reported both missing; and it reported the
      signer application absent because it searched four directories deep where
      that application lives nine.

      Run on 2026-09-13. `verify-key` returned the fingerprint the host pins.
      The content digest taken before signing and the `content_sha256` the
      signature covers are the same value, and the installed version's content
      is byte for byte the file the supervisor still runs.

      One thing the installer does not know about: the version is placed beside
      the daemon's configuration by hand, and `observe-root-launchd.sh` neither
      installs nor removes it. A reinstall leaves it alone, which is right, and a
      machine that lost it would need the ceremony again.

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
- [x] 3.6 Take the tunnel from its holder before starting one.

      Found by reading the code rather than by running it, and the rehearsal
      could not have found it: a rehearsal skips exactly the two phases
      involved. `Run` placed the claim and started sing-box. The supervisor,
      seeing the claim, stops *restarting* the process — it does not stop the
      one already running. So a real handover would have put two sing-box
      processes on one tunnel address.

      The order is claim, then take, then start. The claim is what makes the
      process this runtime's to stop; stopping first would be one runtime ending
      another's job without having said so on disk. Waiting for the previous
      owner to notice on its own tick was rejected: sixty seconds against a
      hundred-and-twenty-second deadline is half the budget spent waiting for
      somebody else to read a file.

      No phase of its own. The undo for "the claim is placed" and "the holder is
      stopped" is the same act — releasing the claim, after which the previous
      owner restarts what it finds missing — and a phase whose undo is another
      phase's undo is not a phase.

      Which process counts as the tunnel comes from the observer the root daemon
      decides `process_gone` with, so the two cannot part company. Parentage is
      deliberately not asked for: the daemon passes its own pid to tell its child
      from a stranger's, and the handover is asking about the stranger's, so a
      filter on parentage would report nothing running in exactly the case there
      is something to take.

      A holder that will not go, one that cannot be signalled, and a reading that
      fails all abort before anything is started. The last of those is the case
      where a second process is most likely.

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

- [x] 5.1 Every phase except the claim and the start, reported as a rehearsal.
- [x] 5.2 Run it on the machine and record what it found.

      Ran 2026-09-14 as `handover-1789336113`: completed at phase `proven` on two
      proofs, and left nothing behind — neither the claim nor the session record
      was on disk afterwards.

      What it proved is the session record, the proving and the clearing. What it
      could not prove is the claim and the start, which it skips by design — and
      that is where task 3.6's defect was living. The rehearsal is worth what it
      costs and is not a substitute for reading the two phases it does not run.

## 6. Gates and evidence

- [ ] 6.1 Mutate the claim, the completion rule, the abort and the verification; confirm the named tests fail.

      Possession, 2026-09-14: nine mutations applied, all nine killed. Taking the
      tunnel after starting instead of before; never refusing at the bound;
      ignoring the signal failure; treating a failed reading as an absent holder;
      possessing during a rehearsal; allowing a nil holder; making the bound
      optional; filtering the holder by parentage; signalling pid zero.

      The signal-failure mutation survived its first run and the test was the
      weak half: the abort happens anyway when the bound is reached, so counting
      the outcome could not tell "would not die" from "was not allowed to ask".
      The test now reads the reason.
- [ ] 6.2 `make check` green.
- [ ] 6.3 Run the real transaction and record what the tunnel did.

## 7. Close

- [ ] 7.1 Sync the delta into the baseline, validate, archive.
