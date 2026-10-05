# Tasks

## 1. The ground

- [x] 1.1 Read on the machine which two operations the route planner has been proposing every cycle, and record them; verified by the operations named in this task rather than by a count.

      Read 2026-09-25. Both are `ensure_host_route` for `wrong_path`, and
      together they swap the two ingress targets: one is carried by the upstream
      VPN and this runtime's configuration prefers the physical interface for it,
      the other is carried by the physical interface and the configuration
      prefers the upstream VPN. Neither is drift in the machine. The live
      arrangement is the one the runtime that owns the tunnel enforces, and this
      runtime's preferences are the inverse of it.

      Applying them would move both ingress paths at once, which is what the
      decision to snapshot and restore rather than reconcile avoids. HEX-11 is
      answered and stays out of this change.

      The reading is a reconstruction — a reader outside the daemon, built from
      the same configuration, the same observer and the same planner — so it was
      checked against the daemon's own count before anything was concluded from
      it: its newest complete decision, 2026-09-25T17:32:05Z, records two planned
      routes, and the reconstruction proposes two.
- [x] 1.2 Read the host routes that point at the tunnel interface, and which of them the tunnel creates for itself; verified by the routing table beside the tunnel's own subnet and gateway.

      Read 2026-09-25. The managed tunnel carries nine entries: seven host routes
      — one corporate, one for a hosted repository, five inherited — and two of
      its own, the tunnel's /30 and the gateway inside it, which the tunnel
      recreates when it starts. The seven are what a rebuild must put back; the
      two are not.

      Nothing else on the machine is ours to touch: the other runtime's own
      tunnel carries thirty-odd prefixes and the upstream VPN carries its split
      default and one host route. A restore that worked by destination rather
      than by the interface it left would reach into both.
- [x] 1.3 Read the signed tunnel configuration version beside the configuration the incumbent runs, and record whether they are still byte for byte the same; verified by comparing digests, not modification times.

      Read 2026-09-25: identical, one digest for both. The version published
      before the first handover is still what the machine runs, so the second
      handover changes who owns the tunnel and nothing about what it is. The
      incumbent's failover keeps its state beside the configuration rather than
      in it, which is why a fortnight of failovers left the bytes alone.
- [x] 1.4 Read the grant this change needs against the installed policy: whether `tunnel_ownership` exists as a root-only capability, what the current generation's expiry is, and what the ceremony will have to produce; verified by the installed policy rather than by the repository's fixtures.

      Read 2026-09-25. The root domain runs bundle generation 4 and policy
      generation 3, active since 2026-09-09, expiring 2026-10-07T14:47:49Z —
      twelve days. Neither domain's authorization is suspended.

      The tunnel question reaches policy and is refused on its merits: of 8,310
      tunnel decisions the archive holds, 57 asked — the cycles that decided to
      act — and all 57 were answered `selector_mismatch`, the newest on
      2026-09-24. No rule selects the action, which is what an ungranted
      capability looks like from here, and is the state item 8 closed in.

      So the ceremony produces a generation whose policy selects this action for
      the root domain, and its validity has to outlast the seven days of
      ownership this change ends with. The present generation does not, by
      itself, leave room for both the work and the week.

## 2. The authorization binds

- [x] 2.1 The evaluator's control-state generation comes from the runtime, not the request; verified by a test that authorizes with a current generation and refuses a stale one, which fails before the change.

      The handler holds the runtime's control state and reads the generation
      from it. The runtime's own holder is the operator controller, which keeps
      the snapshot; it mirrors the generation in an atomic beside it, because
      the controller asks the handler to evaluate a resume while holding its own
      lock and a handler that read back through that lock would deadlock the act
      it was asked about. Every write of the snapshot writes the mirror.
- [x] 2.2 A runtime that cannot read its control-state generation refuses the action and says so; verified by a test driving the read failure, and by the refusal naming the read rather than the policy.

      The refusal is `control_state_unreadable`, and a handler that was never
      given a control state answers the same: it has nothing to compare against,
      and authorizing there is the failure the comparison exists to prevent.
      Four existing tests authorized without one and now refuse, which is what
      made the wiring of both daemons visible rather than assumed.
- [x] 2.3 The three action paths — Pritunl rescue, operator resume, the tunnel question — are each covered; verified by a test per path that a stale generation is refused.

      Listed rather than abbreviated, because the defect was on all three at
      once and a test covering one would have passed while the others
      authorized freely.

      What no test reaches is the wiring inside each daemon's own startup. It
      fails closed — an unwired handler refuses everything rather than
      authorizing it — and the installation reads the machine's own answer to
      the tunnel question, which must stay a policy refusal and not become
      `control_state_unreadable`.
- [x] 2.4 Mutate the comparison, the source of the runtime's generation and the read-failure path; confirm the named tests fail, and record how many were applied and how many were killed.

      Seven applied, seven killed: the request's generation compared with
      itself again, an unreadable state authorizing, a handler with no control
      state authorizing, the mirror left unwritten on an update, the mirror left
      unwritten on a resume, the mirror never seeded, and a controller that is
      not there reporting zero as though it had read one.

      Two of them did not compile or were not unique on the first run and were
      rewritten rather than counted, and the resume-path mutation survived its
      first run — nothing drove a resume through the controller — until a test
      did.

## 3. The executor

- [x] 3.1 A rebuild stops the tunnel this runtime started, starts one from the signed version and puts back the host routes that pointed at the old interface; verified by a test with the new interface differing from the old.

      `internal/tunnelexec`. The rebuild is handed what to replace and reports
      what happened; it decides nothing. The start goes through the handover
      binary rather than through the daemon, because verifying a signed version
      belongs to the delivery path and the gate here keeps the always-running
      daemons off it — see the design. Stopping stays in the daemon: it signals
      a process it is already watching and waits for it to be gone.
- [x] 3.2 Routes that pointed anywhere else, and the routes the tunnel creates for itself, are left alone; verified by a test with routes on three interfaces where only the tunnel's move.

      What is put back is what the cycle observed pointing at the tunnel, and
      nothing else — measured on this machine, the seven host routes of task 1.2
      and not the tunnel's own two, nor the thirty-odd on another runtime's
      tunnel. A host route to somewhere this runtime knows nothing about is not
      restored, because it was never read.
- [x] 3.3 A route that cannot be restored makes the rebuild a failure, named in the record, with the tunnel left running; verified by a test driving the failure.
- [x] 3.4 A rebuild counts as done only when the payload path answers within the bound; verified by tests for both outcomes, including the bound expiring.

      A probe that errors is not an answer: it is waited out, not taken as a
      pass. The clock is injected, so the bound is exercised exactly and a test
      spends no real time.
- [x] 3.5 A suspended cycle records its decision and performs nothing, and the first waking cycle performs; verified by a test over a cycle marked suspended and the cycle after it.
- [x] 3.6 The act is gated on the claim, the grant answered in that cycle, and the rate bound; verified by a test per gate that the act does not happen and the record says which gate stopped it.

      The gates name the least specific one that holds, so a record does not
      send a reader to the policy for a machine that was merely asleep. A claim
      that cannot be read is not a claim, and a rate that cannot be read is a
      bound reached: both fail towards not acting.

      What became of a decision is its own record, `tunnel.execution`, because
      the decision is written before the act and the outcome is known after it.
      It is critical rather than operational: it is the only record that the
      machine was changed, and an account of an act that can be evicted for size
      is not an account.
- [x] 3.7 Mutate each gate, the route restore, the payload wait and the suspended check; confirm the named tests fail, and record the counts.

      Sixteen applied, sixteen killed. The rebuild, nine: a failed stop starting
      anyway, routes put back on the old interface, a failed route passed over,
      done without the payload, a probe that errored counted as a pass, the
      payload asked once, no bound on the wait, an absent interface restored
      onto, and an incomplete rebuilder performing. The gates, seven: each of
      the four not being a gate, suspension outranking the claim, the grant
      outranking suspension, and blocked reading as allowed.

      Two did not compile and one was not unique; they were rewritten rather
      than counted. One hung instead of failing — the bound removed, with a
      clock that only advances when the rebuild sleeps — and the harness now
      bounds the test run so a hang is a death rather than a wait.

## 4. The bound and giving up

- [x] 4.1 The rate bound holds at two in five minutes, six in an hour and fifteen in a day, counted across a restart of this runtime; verified by a test that reopens the store and continues counting.

      The count is a file named in the configuration rather than derived, so
      the daemon and the operator's own command cannot work out the path
      differently. What no bound can still see is forgotten, so it does not grow
      for the life of the machine. Nine mutations, nine killed.
- [x] 4.2 Reaching a bound ends ownership through the release transaction and leaves the runtime observing; verified by a test that the transaction ran and no further rebuild is performed.

      The release runs through the binary an operator would run for it, for the
      same reason the start does. A release that did not complete is recorded as
      one that did not: a runtime that decided to let go and could not is in a
      different state from one that let go.
- [x] 4.3 A lapsed grant ends ownership the same way; verified by a test over an expired generation.

      Standing is read on every cycle, including the ones that decide nothing: a
      generation expires whether or not anything was decided, and a runtime that
      only looked when it wanted to act could hold a tunnel it had no right to
      for as long as it had no reason to touch it.
- [x] 4.4 A payload that fails for three consecutive complete cycles with the outer path reachable ends ownership, and one that fails with the outer path down does not; verified by tests for both.

      The count is the executor's own rather than the rule's, because the rule
      counts every failed payload and this counts only the ones that say
      something about the tunnel. A cycle that did not finish, or one whose
      outer path was down, leaves the count where it was rather than resetting
      it, so a machine alternating between the two still reaches the threshold.
      It lives in memory: a restart forgets, which delays a handback and never
      causes one.
- [x] 4.5 With no other runtime holding a tunnel, giving up stops this runtime's tunnel, records that the machine has none and alerts; verified by a test with no incumbent.

      The release transaction takes the tunnel back when nobody else raises one,
      which is right for an exchange and wrong for a giving-up: a runtime
      reaches one by deciding it should not hold the tunnel, and restoring would
      undo the decision that started it. A release can now be a giving-up, and
      then it stops, lets the claim go and says the machine may have none rather
      than reporting a completed release. The runtime asks for that form and the
      operator's own release keeps the old one.

      The alert is section 5's: the record is written here.
- [x] 4.6 `resume` clears the bound and takes no tunnel; verified by a test that acting is allowed again and ownership is unchanged.

      `hexroute-handover resume-executor`. It clears the count, says how many it
      cleared and who holds the tunnel, and leaves the claim untouched — a host
      with no executor is told so rather than having a path guessed at for it.
- [x] 4.7 Mutate each of the three conditions, the outer-path gate, the counter's persistence and what `resume` clears; confirm the named tests fail, and record the counts.

      Sixteen applied, sixteen killed. The conditions, eight: a runtime owning
      nothing handing back, each of the three conditions not being one, the rate
      outranking the grant, the payload outranking the rate, an unconfigured
      threshold handing back, and the threshold read exclusively. The wiring,
      eight: a failed release recorded as given, the release never running, an
      outer path that is down still counting, an unfinished cycle counting,
      traffic passing not clearing the count, nothing counted, no authority read
      as standing, and the threshold not read from the configuration.

      Two of the wiring mutations survived their first run — the tests that
      would have killed them were outside the harness's filter — which is a
      reminder that a harness naming its own tests can pass by naming too few.

      The giving-up, five applied and four killed: giving up taking the tunnel
      back, every release giving up, a giving-up reporting completion, and the
      runtime asking for an exchange rather than a giving-up. The survivor is
      the command's own assignment of its flag to the transaction — one line,
      reachable only by building the whole transaction a release needs, and left
      uncovered rather than covered by a test of an assignment. What it would
      break is measured on either side of it: the transaction's behaviour under
      both forms, and which form this runtime asks for.

## 5. Alerts

- [x] 5.1 A guard trip, a handback for any reason and a lapsed grant each raise an alert on the local session; Telegram is recorded as owed. Verified by a test per event that the delivery is claimed.

      Measured first, and it changed the task. There is no path from the root
      runtime to an alert: the local notification is dispatched by the
      operator's own session from its own knowledge, Telegram is delivered by
      the cloud from incidents the cloud opens itself by reconciling silence,
      and the only connection between the two daemons runs from the operator's
      session into the root socket.

      So the runtime that gives the tunnel up leaves word in a file the
      operator's session reads — a reason from a closed list, when, and whether
      the tunnel went back — and that session announces it once, as critical:
      the machine may be on somebody else's tunnel or on none, which is worth a
      night. The identity carries the moment, so the announcement is not made
      again by the next process.

      Telegram would need the cloud to open an incident from a host's own event,
      which it does not do today. That is its own change, with a database and
      container tests, and is recorded as owed rather than half-built here.

      Eleven mutations, eleven killed: another schema read as a handback, a
      reason nobody wrote accepted, a moment nobody can read accepted, an absent
      notice read as a failure, a notice with nothing to say written, the notice
      written unreadable by the operator, an unreadable notice announcing a
      loss, the announcement not critical, the announcement forgetting when it
      happened, the next process announcing it again, and no word left at all.
- [x] 5.2 An ordinary rebuild and a single failed rebuild raise no alert and appear in the record and the morning digest; verified by a test that no delivery is claimed for them.

      Nothing but a handback leaves word, so nothing but a handback is
      announced: a rebuild, failed or not, is in the archive and nowhere else.

## 6. The second handover

- [x] 6.1 The preflight refuses while the supervisor holding the tunnel started before its installed script was written, naming both times; verified by a test over both orders.

      `internal/incumbentcheck`. It reads from outside that runtime: which
      process runs the script, when it started, and when the script was last
      written. The start time is asked for as a moment rather than as an age,
      because an elapsed time is a duration from now and comparing it with a
      file's timestamp would be comparing two clocks' opinions of one moment.

      Equal times are not fresh: a script is written and then a process starts,
      so a process no younger than its script is the older of the two by
      everything but the filesystem's resolution.

      Running it on the machine found a defect no test had: two consecutive
      preflights read different processes and reported opposite answers. Several
      processes carry that script in their command line — a shell subshell
      inherits its parent's arguments, and that supervisor forks one for every
      command substitution it makes — so the table holds the supervisor, a
      long-lived child, and whatever momentary subshell existed when the listing
      was taken. One of those was three seconds old and read as fresh. The
      supervisor is now the one nothing else running that script fathered, and
      the oldest where several qualify.
- [x] 6.2 The handover restarts that supervisor as a process, and the preflight then passes only on the new process; verified on the machine, with the times read before and after.

      Before, 2026-09-26T08:30Z: `REFUSED previous owner is current — pid 95451
      started 2026-09-14T08:14:02Z, before its script was written
      2026-09-14T13:07:26Z`. Twelve days on bytes replaced five hours after that
      process began.

      After `launchctl kickstart -k system/com.twilight.supervisor`: `ok — pid
      11216 started 2026-09-26T08:37:02Z, after its script was written; it
      announces a 1m0s health interval`. Every one of the eight preconditions
      then held.

      The reading also exercised 6.1 on live processes rather than on fixtures.
      Three processes carried the script — the supervisor launchd fathered, its
      long-lived child, and a subshell forked two and a half minutes later — and
      the one reported is the one nothing else running that script fathered.
- [x] 6.3 The preflight confirms the restarted supervisor announces the settings the handover expects; verified by the announced health interval read from its log rather than from the script's default.

      The preflight reports the interval the supervisor announced at start. It
      is read rather than assumed because the script's own default is not what
      it runs with — measured 2026-09-25, ten seconds in the script and sixty in
      what it announced, because its settings come from a file it reads at start
      rather than from its launch definition. The last announcement is the
      running process's, because a restart appends.

      A log that cannot be read is an error rather than an announcement of
      nothing: a preflight reporting no announcement for a log it never read
      would be reporting on a supervisor it never saw.
- [x] 6.4 The runtime can run the release transaction itself, with the same phases and the same completion on traffic; verified by a test that the runtime-initiated release and the operator's take the same path.

      Done in section 4: the same transaction, asked for as a giving-up rather
      than an exchange. The difference is one flag and it is the difference
      between a machine that keeps its tunnel and one that is left without, so
      what this runtime asks for is readable without running anything.
- [x] 6.5 Mutate the preflight's comparison, the restart requirement and the settings check; confirm the named tests fail, and record the counts.

      Eleven applied, eleven killed: an older supervisor read as fresh, a
      supervisor nobody found read as fresh, the comparison made inclusive, the
      script's time not read, a script nobody can stat passing, the first
      announcement taken instead of the last, an unreadable log announcing zero,
      an elapsed time asked for instead of a start time, any process taken for
      the supervisor, a failed listing read as no supervisor, and an unreadable
      line taken for one.

      Three survived their first run and were closed rather than excused: the
      not-found case needed a start time later than the script's, the inclusive
      comparison needed two equal times, and the unreadable log needed a log
      that exists and cannot be read.

      Three more for choosing between processes, all killed: a subshell taken
      for the supervisor, the newest taken instead of the oldest, and nothing
      chosen at all. The first survived its own first run too — the case that
      catches it is a child listed before its parent with the same start moment,
      which is exactly what the machine prints.

## 7. Gates and evidence

- [x] 7.1 `make check` passes, with `secret-test` covering the new records and documents; verified by its exit status.

      Run after every piece of this change. One commit went in red — the
      reachability gate refuses a package no binary reaches, and the executor's
      was not wired yet — and the next was green; nothing was pushed in between.
- [x] 7.2 Strict OpenSpec validation passes for this change and the baseline; verified by `openspec validate --strict`.
- [x] 7.3 The runbook says how the ceremony is performed, how the tunnel is taken and given back, and what to do when it comes back on its own; verified by an operator following it without reading the code.

      `docs/macos/tunnel-executor.md`. Taking the tunnel stays where it was
      written; this is the runtime that holds one: what it needs to be able to
      act, what the grant is and how to read that the machine has it, what it
      does every cycle, the three things that end its ownership and where it
      leaves word, how to resume it, both rollbacks, and what is not covered.

      Verifying it by following it is the handover's own step, and waits for it.
- [x] 7.4 The rollback is stated and rehearsed: `release` by hand, and a generation without `tunnel_ownership`; verified by the rehearsal the transaction already supports.

      Both are in the runbook, in the order of how much they undo. The second
      needs no code and no restart — a generation without the capability leaves
      the executor unable to act while the daemon goes on deciding, which is the
      state the rule was proved in — and it is the one to reach for first.

      The rehearsal is the transaction's own: `check-release` asks what a
      release would find, and `rehearse` runs every phase of a handover except
      the two that change anything. Running them is part of the handover's step.

## 8. The ceremony and the handover

- [x] 8.1 Publish the generation granting root-only `tunnel_ownership`, with user presence, and confirm the runtime's question is answered by the policy rather than refused as malformed; verified by the recorded answer on the machine.

      Generation 5 is active in both domains as of 2026-09-26T00:15:59Z, to
      2026-10-25T22:45:57Z. The runtime's question is answered
      `True/authorized`, where under generation 4 the same question was answered
      `selector_mismatch`; the execution that followed was stopped by the claim
      gate — `performed=False blocked=not_owned` — which is where it should stop
      until 8.3.

      It took HEX-20 twice. The daemons refused to start at all (2026-09-25),
      and then, once they started, refused the successor as a downgrade because
      they reported the generation they held without holding it
      (2026-09-26). Both are recorded in `start-when-the-static-authority-moved`.

      The capability entered the compiled safety envelope on 2026-09-13, and the
      signer application on this machine was built six days earlier: its
      compiler refuses a source granting what its envelope does not carry. The
      envelope's digest is a source's `static_sha256`, so a rebuilt signer is a
      new static authority — a reviewed configuration installation and a guarded
      restart, by the runbook's own rule.

      That sequence deadlocks. With the old static digest installed, a candidate
      carrying the new one is refused as `restart_required`. With the new one
      installed, neither daemon starts: the stored generation fails
      `RecoverActive` with that same error, which is not among the failures a
      handler survives, so both refused and launchd restarted them every ten to
      twenty seconds. Activation goes through their sockets. Rolling both
      configurations back and restarting restored generation 4 in both domains
      within thirty seconds.

      What is prepared and waiting: the rebuilt signer
      (`1240a684c911…`, `git.0a052e59ed54`), and generation 5 compiled, diffed,
      replayed and signed — one newly allowed plan, root `tunnel_ownership` on
      target `tunnel`, with every lease ending when the generation does. It sits
      uninstalled in the operator's workspace.

      The Pritunl leases were extended to the new generation's expiry while
      preparing it: copying the source unchanged would have left them ending on
      2026-10-07 while the generation ran to 10-25, quietly dropping an
      authority the machine has today. A lease's interval is part of a domain's
      semantics, so the user domain's policy generation advances with it.
- [x] 8.2 Install the executor while the tunnel is still the incumbent's, and confirm it performs nothing without a claim; verified by a reading of the records after installation.

      Installed 2026-09-25 21:34Z: the execution block added to the root
      configuration beside everything it already had, the daemon asked to
      refuse it before anything was installed, then both binaries and a restart.

      The first reading showed decisions and nothing else, because an execution
      is recorded only where a decision decided to act and no cause had held
      since the restart. Inducing the one cause that can be induced without
      changing the network's shape — stopping the previous owner's tunnel
      process, which it restarts within seconds — produced what the installation
      was meant to show:

      - `rebuild_tunnel ['process_gone']` with the policy answering
        `selector_mismatch`, which is the ungranted capability and also the
        proof that the handler has its own control state: an unwired one answers
        `control_state_unreadable`.
      - `performed=False blocked=not_owned`: the claim gate stopped the act.
      - no handback: a runtime holding no tunnel gives none back.

      The previous owner recorded `process_missing` at 21:43:21Z and was healthy
      again by 21:43:39Z, as it had through the whole soak.
- [x] 8.3 Restart the incumbent supervisor, run the preflight, and take the tunnel; verified by the transaction completing on traffic.

      2026-09-26: the supervisor restarted (task 6.2), eight preconditions held,
      and `begin` completed — `handover-1790413378`, phase `proven`, two proofs.
      The claim named this runtime, one tunnel ran from the signed content, and
      the incumbent stood down with `HANDED_OVER reason=tunnel_claimed`.

      It was given back the same day, because of what the twenty minutes in
      between showed. `check-release` held on all eight, including the two about
      the other runtime; `release` completed — `release-1790414420`, phase
      `proven`, two proofs — and Twilight has the tunnel, the claim is gone, and
      this runtime's decisions are blocked `not_owned` again.

      Five defects, every one of them found by performing this step and none of
      them visible from the code alone. They are recorded here and owed a change
      of their own; 8.4 and 8.5 wait on it, because in this state they would
      measure the defects rather than the rule.

      1. **The handover makes the next cycle decide a process was lost.** The
         grounds say it: `09:02:38 replaced=false`, then `09:03:36
         replaced=true, running=true` and `rebuild_tunnel ['process_gone']`
         performed. The cycle before the handover saw the incumbent's process,
         this runtime stopped it and started its own, and the next cycle read
         the exchange as the loss the rule rebuilds for. It rebuilt the tunnel
         it had been given thirty-eight seconds earlier.
      2. **The root daemon ended at about 09:04:00 with no `daemon_stopped`,**
         and the tunnel its rebuild had started was gone with it: `09:04:05
         running=false, complete=false, tick_gap=0`. `runs = 23` under launchd,
         nothing above `info` in either log. The cause is not established and is
         the first thing the change must establish.
      3. **A fresh daemon's first cycle cannot ask policy, and the gate calls
         that unauthorized.** Control generation is 0 in the first cycle, so no
         question is asked — the decision record says `not asked` — and the gate
         reads `Authorized=false` and records `blocked=unauthorized`. At 09:04:05
         the tunnel was down, this runtime had both the reason and the grant, and
         it refused itself while naming the grant. The machine went about a
         minute with no tunnel and no incumbent covering, because the claim was
         held. It is the same mistake as the malformed question of 2026-09-14,
         one layer down: the decision record learned to say "nothing could be
         asked" and the gate did not.
      4. **The handback cannot give the tunnel back.** `releaseArguments` passes
         `--config` and `--relinquish` only, and `release` builds its starter
         from `--tunnel-version`, `--target-key`, `--sing-box` and `--content`
         before it touches anything, so it exits 2. Measured four times:
         `HANDBACK reason=rate_bound given=False`, once per cycle while the bound
         held. The failure is safe — nothing is stopped — and the valve for a
         lapsed grant, a reached bound and a dead tunnel is decorative.
      5. **Two stop paths, two signals, two windows.** The executor sends
         SIGTERM and waits ten seconds, and stopped the tunnel in about 2.5
         seconds twice. The handover sends `os.Interrupt` and waits the
         possession window, and did not stop this runtime's own tunnel in thirty
         seconds; the release completed at ninety. Whether SIGINT needs more than
         thirty seconds or the second attempt finished what the first began is
         not separated by this reading.

      Not a defect, but owed a reading: the restarted supervisor announces
      `wake-watchdog: threshold=180s`, and the soaked rule's
      `wake_threshold_seconds` was read from what the supervisor logged before
      2026-09-14. The two values have not been compared since the restart.
- [x] 8.4 Induce one rebuild per cause and read the outcome of each: a process loss, a wake gap and a carrier change; verified by the records naming the cause, the routes restored and the payload passing.

      The five defects of 8.3 are closed, and the first cause is read.

      **A process loss**, 2026-09-26T16:28Z, with the tunnel this runtime's:
      `rebuild_tunnel ['process_gone']` on grounds `running=false,
      replaced=false`, answered `authorized`, and
      `performed=true reason=none routes=0 elapsed=2453ms` — done on traffic,
      which is the only thing that counts as done. The cycle after it decided
      `none`: no second rebuild, because the cycle was told about the
      replacement this runtime made.

      `routes=0` is the right answer for this cause and not a gap in the
      reading. A lost process takes its interface and the routes on it away, so
      there is nothing pointing at the old interface to put back; the new
      process brings its own up. Routes are what the other two causes exercise,
      where the process lives and the interface is replaced.

      **A wake gap**, 2026-09-29T16:00:01Z: `rebuild_tunnel ['wake_gap']` on a
      gap of 1,330,715 ms with 1,266,949 ms of it slept — the wall clock and the
      steady clock disagreeing by the sleep, which is what the cause is read
      from — answered `authorized`, and `performed=true reason=none routes=7
      elapsed=3015ms`. Seven host routes that pointed at the interface the old
      process held were put back on the new one, and traffic passed inside three
      seconds.

      This is the cause that exercises the routes. A lost process takes its
      interface away with its routes; a wake leaves the process alive and the
      interface replaced, so there is something to restore and the count is not
      nought.

      A second wake half an hour later read the other way, and is 9.9 working:
      `performed=true reason=outer_path_absent routes=7 elapsed=33374ms`. The
      routes were restored again and the proof could not be taken, because the
      machine had not found its network yet. The record says so rather than
      naming the payload.

      Closing the lid does not reliably produce this. Measured 2026-09-28,
      thirty-seven minutes with the lid shut ran a cycle a minute with nothing
      slept, every one marked suspended: the machine dozed rather than slept,
      the gap never reached the threshold, and no cause could hold. The sleeps
      above were induced with `pmset sleepnow`.

      **A carrier change**, 2026-09-29T18:10Z to 18:23Z, induced by switching the
      upstream the machine runs over off and back on. Three changes of the
      signature, three rebuilds, seven routes restored by each:

      - 18:10:27 `['carrier_changed']`, authorized, `performed=true reason=none
        routes=7 elapsed=2850ms`
      - 18:16:31 the signature back to what it was, `reason=none routes=7
        elapsed=7767ms`
      - 18:22:35 changed again, `reason=outer_path_absent routes=7
        elapsed=38463ms` — the upstream was still coming back, and 9.9 says so
        rather than naming the payload

      Six minutes were left between the switches on purpose. Two rebuilds inside
      five minutes reach the bound this runtime keeps on itself, and the handback
      that follows would have ended forty-two hours of ownership to measure a
      cause.

      The carrier is three entries: the upstream probe and two ingress routes,
      two of which went over the tunnel interface the upstream raised and one
      straight out. It is the same shape the previous owner announces as its own
      baseline, which is what the rule was written to reproduce.

      An earlier attempt at the same cause, at 14:46Z, reported
      `payload_did_not_pass` after the full thirty-second bound. It was honest:
      the path was flapping — the payload failed in four of the twenty cycles
      before it and in most of those after — and a rebuild is done only when
      traffic passes. That reading is kept because it is what the rule says
      about a rebuild on a path that carries nothing, and because the dead
      tunnel it led to is what proved 9.2 on the machine.
- [x] 8.5 Hold the tunnel for seven days with no guard trip and no rebuild the payload did not pass; verified by a reading of the records at the end, and by the operator's own use of the machine.

      Running. The claim has been this runtime's continuously since
      2026-09-28T00:05:06Z, on the binaries built at `68a776a` and installed
      2026-09-27T23:49Z — 9.2 through 9.10, with the executor's own handback
      threshold of three.

      What the reading at the end has to account for, because it is not in what
      is installed: 9.11 is in the repository and not on the machine, so a
      restart in this window can still be recorded as `operator_socket_ended` on
      the error stream rather than as an ordinary stop. That is a record about
      the runtime's own ending and says nothing about the tunnel, but a reader
      counting failures would count it. Installing it was deliberately left
      until after the seven days.

      **Passed.** The claim was this runtime's from 2026-09-28T00:05:06Z to the
      reading of 2026-10-05T03:02Z without a break: 171 hours against the 168
      asked for, over 10,129 cycles.

      No guard tripped. Nought handbacks, and nought executions stopped by a
      gate — no `rate_bound`, no `unauthorized`, no `not_owned`. The runtime
      acted six times, each one a cause induced for 8.4 on 2026-09-29, and never
      again of its own accord through the remaining five days:

      - four rebuilds done on traffic, at 3.0, 2.9, 7.8 and 24.4 seconds
      - two that could not take their proof and recorded `outer_path_absent`, at
        33.4 and 38.5 seconds
      - seven host routes restored by every one of the six

      **No rebuild blamed the payload.** `payload_did_not_pass` appears nowhere
      in the window. The two that did not prove traffic name the path, which is
      9.9 doing its work on a machine whose network takes minutes to come back
      after a sleep.

      The threshold of 9.10 is what made the week possible, and the margin is
      measured rather than assumed: 758 of 10,036 cycles with the outer path up
      did not traverse — 7.6% — and the longest run that counts against
      ownership was **two**, against a threshold of three, across 10,063
      complete cycles. Before that number was separated, ownership ended three
      times in two days.

      The runtime neither started nor stopped once in the whole window, so the
      caveat about 9.11 not being installed never arose. The operator used the
      machine throughout, and the tunnel it uses is the one this runtime
      raised: at the close its process had run five days and eight hours, and
      traffic traversed in 0.21 seconds.

      66 of the cycles ran while the machine was suspended. They decided nothing
      and performed nothing, which is the answer owed for a dozing machine.

## 9. What the second handover found

- [x] 9.1 The executor's two archive exits say what ended the loop, under a name of their own; verified by the vocabulary's gate and the loop's own tests.

      `say-why-a-runtime-stopped` gives the loop five names and deliberately has
      none for the event archive: every other use of it reports its own failure
      and carries on. This change is what makes writing it fatal — an execution
      and a handback are acts, and a runtime that acted and could not say so is
      worse than one that stopped — so the name is this change's to add.

- [x] 9.2 The handback gives the tunnel back; verified by a test over what this runtime asks the handover binary for, and by a handback that releases on the machine.

      `releaseArguments` passes `--config` and `--relinquish` only, and `release`
      builds its starter from `--tunnel-version`, `--target-key`, `--sing-box`
      and `--content` before it touches anything, so it exits 2. Measured four
      times on 2026-09-26: `HANDBACK reason=rate_bound given=False`, once per
      cycle while the bound held. The failure is safe — nothing is stopped — and
      the valve for a lapsed grant, a reached bound and a dead tunnel is
      decorative.

      The test reads the arguments rather than running the binary, for the same
      reason that function exists: what this runtime asks for can be read
      without running anything, and a test that ran it would prove the harness.

      Done in code: the delivery arguments are carried although a giving-up
      does not restore, because `release` builds its starter before it touches
      anything and refuses without them.

      Proven on the machine 2026-09-26T16:34:32Z, and not by a situation set up
      for it: `HANDBACK reason=tunnel_carries_nothing given=true`. An induced
      process loss had been rebuilt on a path that was flapping, the payload
      then failed to the threshold with the outer path up, and the valve fired.
      The claim was released, this runtime's tunnel stopped, and the previous
      owner raised its own within the minute.

      `given` was still read from the release command's exit status then — 9.7
      came later — so a true there says the command exited nought, which is to
      say it ran the whole transaction. Two flags out of six could not have.

      The handback of two hours earlier, 14:57:37Z, is the other side of the
      same evidence and was at first written down as the proof. It reported
      `given=false`, and that is what this change's own code did before the fix
      was installed.
- [x] 9.3 One stop path, one signal, one window; verified by a test that both callers reach the same stop, and by the release completing inside its default.

      Two paths send different signals on different windows. The executor sends
      SIGTERM and waits ten seconds, and stopped the tunnel in about 2.5 seconds
      twice; the handover sends `os.Interrupt` and waits the possession window,
      and did not stop this runtime's own tunnel in thirty seconds. The release
      completed at ninety. Whether SIGINT needs more than thirty seconds or the
      second attempt finished what the first began is not separated by that
      reading, and one path removes the question.

      `internal/tunnelstop`, its own package because the two callers cannot
      share one otherwise: the handover verifies a signed configuration version
      and the daemons are kept off that path, so nothing they import may reach
      it.

      Writing the test found two facts about this host rather than about the
      code. A child nobody has waited for is still there to signal, so a stop
      measured against one of the test's own children measures parenthood — the
      tunnel is started through another binary and is nobody's child here. And a
      process already gone refuses the signal; reported as a failure that would
      fail a rebuild for a tunnel that died between the cycle observing it and
      the stop, so it is the answer the caller wanted.
- [x] 9.4 A cycle that could not ask records that, and not a refusal naming the grant; verified by a test over the gate and the record.

      Control generation is 0 in a runtime's first cycle, so no question is
      asked — the decision record says so — and the gate reads `Authorized=false`
      and records `blocked=unauthorized`. Measured 2026-09-26T09:04:05Z: the
      tunnel was down, this runtime had both the reason and the grant, and it
      refused itself while naming the grant. The machine went about a minute
      with no tunnel and no incumbent covering, because the claim was held.

      It is the malformed question of 2026-09-14 one layer down: the decision
      record learned to say "nothing could be asked" and the gate did not.

      The gate has a fifth name, `authorization_unasked`, and it sits before
      `unauthorized` because "nothing could be asked" is about this runtime
      having no state to ask with while "the grant refused" is a claim about what
      the grant says — and a runtime that never asked is in no position to make
      it. What the executor is given is the answer itself rather than a bool, so
      an absent one cannot look like a refusal; that is what a bool did.
- [x] 9.5 A change of ownership is not a process loss; verified by a test over the first cycle after the claim changes hands, and by a handover that draws no rebuild.

      The grounds said it: `09:02:38 replaced=false`, then `09:03:36
      replaced=true, running=true` and `rebuild_tunnel ['process_gone']`
      performed. The cycle before the handover saw the incumbent's process, this
      runtime stopped it and started its own, and the next cycle read the
      exchange as the loss the rule rebuilds for. It rebuilt the tunnel it had
      been given thirty-eight seconds earlier, and a second rebuild a minute
      later reached the rate bound.

      The rule is not what changes. A cycle has no previous process to compare
      with when ownership changed since the last one, exactly as the first cycle
      of a process has none — and the executor's own rebuild is a replacement
      this runtime made rather than one it found.

      Ownership is read from what the cycle already had: the configuration the
      tunnel's owner runs, which is this runtime's while it holds the claim and
      the previous owner's otherwise. A process under a different configuration
      than the one last seen is the other runtime's, or this runtime's where the
      last was the other's, and neither is a loss anybody watched.

      The rebuild's own replacement is told to the cycle in memory, not written
      down, for the reason the rest of that memory is not: a process replaced
      while this runtime was not running is not one it watched go. `Replaced` is
      on the cycler interface because the memory a loss is decided from belongs
      to the cycle, and an act that replaced the process has to reach it.

      Proven on the machine 2026-09-26. The tunnel was taken a third time —
      `handover-1790433151`, phase `proven`, two proofs, claimed at 14:32:31Z —
      and every cycle since has decided `none` with `replaced=False`: nought
      rebuilds, where the same exchange three hours earlier drew two inside
      ninety seconds and reached the bound. Traffic traverses the tunnel this
      runtime now holds.

      The preflight refused once before that, on the payload, and was right to:
      the probe had failed in four of the twenty cycles before it, and a
      handover needs two consecutive proofs. It was taken ten clean cycles
      later. The flapping coincided with the previous owner's own selector
      timing out against one of its upstreams.
- [x] 9.6 Mutate each of the four; confirm the named tests fail, and record the counts.

      Nine applied, nine killed: the release asking with two flags again, the
      stop sending an interrupt again, a process already gone read as a failure,
      an unasked question read as a refusal, the gate reading the answer without
      the question, a change of hands read as a loss, the owner never remembered,
      a rebuild this runtime made read as a loss, and the loop not saying what it
      replaced.

      Two survived their first run and both were tests that did not exist: the
      signal, and the loop telling the cycle. The second needed more than one
      cycle to reach, because a runtime's first cycle has no control state to ask
      the grant with — 9.4's defect, standing in the way of 9.5's test.

      The signal's survival was worth more than the mutation. The test that was
      meant to tell SIGTERM from an interrupt sent the signal before the shell
      had installed its own `trap`, so it measured that race: under the mutation
      one case passed because its process died before it could ignore anything,
      and another passed because its process had not yet stopped ignoring. The
      process says when it is ready now, and the mutation dies.

- [x] 9.7 What the record says about giving the tunnel back is read from the claim, not from the release command's exit status; verified by a test over both halves disagreeing, and by the reading that found it.

      Found by 9.2's own proof. The tunnel was given back and the word left for
      the operator said `given=false`, because a giving-up does not take the
      tunnel back: after releasing the claim it waits for the previous owner's
      tunnel to carry traffic, and on that flapping path it could not prove two
      traversals. The command reported a failure for a release that had
      released.

      Whether the tunnel was given back is a fact on disk. An unreadable claim
      is not an answer either way and says nothing was given, which is why this
      is not the negation of owning — that reads an unreadable claim as the
      other runtime's, and must not read it as proof this one let go.

      Three mutations, three killed: the exit status again, an unreadable claim
      counting as given, and given as the negation of owning.

- [x] 9.8 A payload that passed clears the count against ownership, whatever the outer path was doing; verified by a test per case and by the reading that found it.

      The executor keeps its own count, and it diverged from the planner's
      ground in one place: a cycle that could not judge was skipped before a
      passing payload was looked at, so a success in a cycle whose outer path
      read as down cleared the ground and left the count standing. There were
      several such cycles through 2026-09-26, and the count climbed across them
      while the ground did not.

      This was found while reading a handback of 2026-09-26T16:34:32Z as having
      fired a failure early, on the strength of a threshold of three taken from
      this repository's own fixtures. The installed configuration says
      `payload_failures: 2`, read on 2026-09-27, so that handback fired exactly
      on its threshold and the earliness was mine. The divergence above is real
      without it — a success clearing one count and not the other is visible in
      the code and in the tests — and the reading that suggested it was not.

      A failure with the outer path down still says nothing about the tunnel and
      is still left standing. A success says the tunnel carried traffic, which
      is the thing being counted against, and clears it. A cycle that did not
      finish carries the last answer rather than one of its own and does
      neither: a suspended machine does not probe.

      Three mutations, three killed: a success with the outer path down clearing
      nothing, an unfinished cycle clearing the count, and a failure with the
      outer path down counting.

- [x] 9.9 A rebuild does not blame the tunnel for a network that is not there; verified by a test per case and by the reading that found it.

      Measured 2026-09-27T16:45Z. A wake of eleven minutes was rebuilt in two
      seconds and seven routes were restored, and the thirty seconds the rebuild
      then had for its proof ran out while the machine was still finding its
      network. Traffic passed three minutes later. The record said
      `payload_did_not_pass`, which sends a reader to the tunnel for something
      the tunnel did not do.

      It is 9.8's principle applied to the rebuild's own proof rather than to the
      count against ownership: a payload that does not pass while the outer path
      is down says nothing about the tunnel. Time without a path is not spent
      from the bound, and a proof that ran out of wall clock without ever having
      a path is recorded as `outer_path_absent`.

      The wait is no longer than it was. The wall clock still ends it at the
      bound, so a machine with no network costs exactly what it cost before;
      what changes is what the record says. Making the rebuild wait for the
      network instead would have stopped the runtime observing for as long as the
      network took, which is worse than a record that has to be read carefully.

      The outer path is read through the same observer the cycle reads it
      through, so the two cannot disagree about what it means. A runtime
      configured with no such endpoint cannot say the path is away and does not
      claim it.

      Four mutations, four killed: the bound spent whatever the path does, an
      absent path still naming the payload, a link that cannot be read counting
      as a path, and a runtime with no link reading the path as away.

      Proven on the machine 2026-09-29T16:34:12Z: a rebuild after a
      thirty-three-minute sleep restored seven routes and then could not take its
      proof, and recorded `outer_path_absent`. Half an hour earlier, after a
      shorter sleep, the same cause recorded `reason=none` in three seconds — so
      the two readings are told apart by what the network was doing rather than
      by how long the runtime waited.

- [x] 9.10 The threshold that ends ownership is its own number; verified by a test over a configuration that names one and one that does not, and by the measurement that chose it.

      One number answered two questions. `payload_failures` was chosen when a
      failing payload was a cause to rebuild — which costs about two and a half
      seconds — and the same number now decides whether to stop holding the
      tunnel at all. It is not compared against anything in the planner: the
      payload became a ground that decides nothing when the rule was narrowed to
      the three causes the soak proved, so its only live use was this.

      Read from the machine on 2026-09-27 rather than from this repository's
      fixtures: `payload_failures: 2`. Over the eight hours before it, 478 of
      480 cycles were complete with the outer path up, 62 of them did not
      traverse, and the longest run was two. So the shared number was set exactly
      where this path lives, and ownership ended three times in two days, while a
      run of three did not happen once.

      `execution.handback_payload_failures` is the executor's own, and the
      number beside it stands where a configuration does not name one — so a
      runtime installed before this behaves as it did. Two mutations, two killed:
      the shared number always winning, and a zero of its own ending ownership at
      once.

      Installed 2026-09-28T23:49Z: `handback_payload_failures: 3` beside the
      shared `payload_failures: 2`, written by the file's own owner with the mode
      preserved and the previous file left beside it.

      What it bought is measured. Ownership had ended three times in two days,
      the longest stretch tens of minutes. From the handover of
      2026-09-27T23:55Z the tunnel has been this runtime's continuously — through
      a night, through thirty-seven minutes of a dozing machine, and through two
      sleeps and the rebuilds that followed them.

- [x] 9.11 A socket that ended because the runtime was asked to stop is recorded as the ordinary ending; verified by a test over both ways the server can end, and by the records that found it.

      This repairs `say-why-a-runtime-stopped`, which is archived and whose
      requirement is right: an ending nobody asked for and one that was requested
      are told apart by the result of the record. The implementation decided it
      by a coin.

      The operator socket's server runs on the loop's own context, so cancelling
      it ends the server too, and both cases of the select are then ready. Go
      chooses between ready cases at random. Measured 2026-09-27: two restarts
      wrote `daemon_stopped result=ok` in the journal and two wrote
      `daemon_stopped result=degraded reason=operator_socket_ended` on the error
      stream, for the same signal and the same act. They looked like records that
      had gone missing, which is how this was noticed at all.

      The socket branch now asks whether the context is already cancelled, and a
      server that ended because the runtime was asked to stop takes the ordinary
      ending. Both runtimes answer the same way, and the record itself is written
      in one place so two callers cannot disagree about it.

      Four mutations, four killed. Two survived their first run and both were
      the tests' fault rather than the code's: they searched the whole log for
      `"result":"ok"`, which a start and a cycle also say, so a mutation making
      every requested stop `degraded` passed them. They read the stop's own
      record now.

- [x] 9.12 Every route role the configuration carries has a log event, and a role without one costs the record rather than the runtime; verified by a test per role, by a test over a role nobody named, and by the outage that found it.

      The `inherited` role had no event. `emitSummary` returned an invalid
      runtime for it, which ended the loop, and the planner proposes an
      inherited operation whenever there is no tunnel to put the routes back on.

      Measured 2026-10-05, after the three restarts that verified 9.11. The
      restarts took this runtime's tunnel with them; the planner then proposed
      the routes; the daemon stopped with `invalid_runtime`; launchd restarted
      it, and it stopped again. For twenty minutes the machine had no tunnel at
      all, with the claim still held — so the previous owner stood down
      (`HANDED_OVER`, then `ROUTE_DEGRADED` complaining the TUN was not there)
      and the one runtime that could have rebuilt never finished a cycle. The
      operator's `start-tunnel` put it back.

      The defect predates this change, and 9.11 is what made it legible: the
      record said `daemon_stopped degraded invalid_runtime` on the error stream,
      where before there was `return 1` and silence. Looking for the cause in
      the executor would have been the obvious mistake, and the record did not
      allow it.

      Both halves are fixed. The role has an event — the name says what is known
      and no more, as the role's own does — and a role nobody has named yet is
      reported as a degraded cycle and skipped, which is the answer the read
      model's own failures already get. A runtime that stops because it cannot
      name a log line is worse than one that logs less.

      Three mutations, three killed: an unnameable role ending the loop again,
      the inherited role losing its event, and an unnameable role passed over
      without a word.

- [x] 9.13 The process table's size cannot refuse the observation the tunnel's loss is decided from; verified by a test over a table past the cap, by a test over a line past what an observation keeps, and by the blindness that found it.

      This is why the morning's outage lasted twenty minutes. The runtime was
      not deciding wrongly — it could not look at all. Every cycle from 03:24 to
      03:59 recorded `process_observed=false`, and `process_gone` cannot hold
      without an observation, so a machine with no tunnel drew `none` from a
      runtime holding the claim that kept the previous owner from starting one.

      The cause, read by running the daemon's own observation by hand:
      `observation command output exceeds limit`. The whole process listing had
      passed the 256 KiB cap on a command's output. Any user on the machine can
      enlarge that table at will, so a larger cap is a later outage rather than a
      fix.

      It is the third time this family has cost something. On 2026-09-14 two
      unrelated processes with command lines of 5,758 and 6,109 bytes refused a
      whole listing, and the per-line bound was removed for it; the bound on the
      listing stayed, and this is it.

      The observation no longer depends on the listing's size: the runner reads
      it line by line and keeps only the lines that could be a tunnel, and a
      line longer than what an observation keeps is skipped rather than refusing
      the listing it is part of. What the keeper wants and the limit cannot hold
      is still a refusal rather than a shorter answer — a listing with lines
      missing can say something is absent when it is there, and absent is one of
      the two answers a cause is decided from.

      Three mutations, three killed. One survived its first run because the fake
      runner in the process tests applied no limit while the real one did: a
      fake that cannot refuse what the machine refuses agrees with whoever wrote
      it. It applies the limit now.

      Proven on the machine 2026-10-05T04:10:42Z: the first cycle on the
      installed fix recorded `process_observed=true, process_running=true`, and
      the four after it the same, where the fifty cycles before had all recorded
      false with the same tunnel running.

## 10. Close

- [x] 10.1 Sync the delta into the baseline, validate, archive.

      Three capabilities. `tunnel-supervision` takes the rule becoming
      performable and everything the executor is: the rebuild, what counts as
      done, the suspended cycle, the rate bound, giving the tunnel up, a
      replacement this runtime caused, a record it cannot write, and the tunnel
      staying observable whatever else the machine runs.
      `tunnel-ownership-handover` takes the preflight's refusal of a supervisor
      running bytes it no longer has, and the runtime's own release.
      `local-control-plane-foundation` takes an authorization comparing this
      runtime's own control state, and modifies what a runtime says about its own
      ending — a requirement that was right and was being decided by a coin.
