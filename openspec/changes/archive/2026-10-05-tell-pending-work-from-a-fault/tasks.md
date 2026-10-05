# Tasks

## 1. The ground

- [x] 1.1 Record what was already known and what is new, so this change does not claim a reading it inherited; verified by the earlier task that holds it.

      `hold-the-tunnel-under-a-grant` task 1.1 read on 2026-09-25 that the two
      standing operations swap the two ingress targets, and concluded that
      neither is drift: the live arrangement is the one the runtime owning the
      tunnel enforces, and this runtime's preferences are its inverse. That
      reading is inherited, not made here.

      What is new is the consequence, which no task records: because the
      proposal is right and standing, the cycle's health can never be sound, so
      the health reports the same value forever and reports nothing by it.

- [x] 1.2 Record the state the health was in before this change, from the machine; verified by a reading that names the duration and the counters together.

      Measured 2026-10-05: root `DEGRADED` unbroken from 04:12:45Z to 11:36Z,
      `consecutive_failures: 0`, `attempts: 0`, `last_reason: probe_failed`,
      with `link_present: true`, `link_failures: 0`, `payload_failures: 0` and
      the kernel agreeing with every carried route the runtime had observed.
      One `observation_cycle` record covers the whole stretch because the
      change gate suppresses a result that has not changed.

- [x] 1.3 Read how many operations stand unapplied right now and from which roles, so the quantity this change publishes has a value to be checked against on the machine.

      Two in the root domain and none in the user domain, read 2026-10-05 after
      the install as `pending_operations: 2` and `0`. The two are the ingress
      routes standing on the opposite links from their configured
      `preferred_link`, which `hold-the-tunnel-under-a-grant` task 1.1 had
      already read on 2026-09-25 and found to be the tunnel owner's
      arrangement.

      The read model counts the same divergence differently — `scoped_routes`
      reports `{configured: 21, conflicting: 14, installed: 7}` — because it
      counts conflicting destinations and the plan counts operations. Both are
      right about different things, and neither is the other's check.

## 2. Health answers soundness

- [x] 2.1 Make the root cycle sound when it has no failure, whatever its plan holds; verified by a test that gives the cycle a non-empty plan and no failure and demands a sound result.

      `TestCycleIsSoundWhileHoldingAProposalItMayNotApply` fails before the
      change — `a standing proposal graded the health: degraded` — and passes
      after. `TestCycleReportsScopedProposalWithoutApplyingIt` carried the old
      expectation and now asserts the proposal is recorded, not applied, and
      does not grade the health.

- [x] 2.2 Establish whether the user cycle has the same defect, and record the answer rather than changing it; verified by the existing tests that pair a plan's action with its machine state.

      It does not. Its health is the state the machine in `internal/control`
      reached through its own transitions, and the clause requiring
      `ActionNone` is redundant with it: `result` pairs every action with the
      snapshot current when it was produced, and an action is produced only
      after recovery was approved, which leaves the machine recovering.
      `internal/pritunlplan/planner_test.go:434` and `:524` observe an action
      beside `StateRecovering`, and `:455` observes `StateHealthy` beside
      `ActionNone`. No case pairs a healthy state with an action.

      The clause is therefore left in place. No test fails without removing it,
      which is the test of whether removing it is work. The proposal's claim
      that the same conflation was written here is withdrawn, and the
      difference is recorded instead: the root daemon synthesizes its state
      from the cycle, which is how a condition about workload reached it.

- [x] 2.3 Prove a failure still makes a cycle unsound, with a plan both empty and not; verified by tests covering all four combinations of failure and standing work.

      Four combinations across three tests, not one:
      no failure and no work by `TestCycleObservesHealthyBaselineWithoutMutation`,
      no failure with work by `TestCycleIsSoundWhileHoldingAProposalItMayNotApply`,
      and both failure cases by
      `TestCycleIsUnsoundOnFailureWhetherWorkStandsOrNot`, which runs with and
      without standing work.

## 3. A reason names a failure

- [x] 3.1 Carry the cause of a cycle's first failure in configuration order on the summary; verified by a test where several observations fail and the cause kept is the one the fold met first.

      `Summary.Cause` is written by `summary.fail(cause)`, which replaces all
      eleven increments of the failure counter and keeps the first cause only.
      The cycle observes in configuration order, and the endpoint probes — the
      one place several failures accumulate — run together and are folded in
      configuration order, so the fold decides rather than which answer arrived
      first.

      `TestCycleKeepsTheCauseOfItsFirstFailure`: two probes that cannot run and
      an outer path that answers not ready. Three failures, two causes, and the
      one kept is `endpoint_unreadable`, met in the fold before the other.

      The eleven causes are `power_unreadable`, `process_unreadable`,
      `tunnel_absent`, `physical_network_unready`, `tun_unreadable`,
      `managed_tun_absent`, `route_unreadable` (twice), `endpoint_unreadable`,
      `probe_failed` and `plan_refused`.

- [x] 3.2 Stop deriving the reason from the health result; verified by a test that a degraded cycle whose failure was not a probe does not report `probe_failed`.

      This is the record that sent a reader after a probe that never failed.

      `rootOperatorReason` now takes the summary and reports the recorded cause
      in preference to anything the state implies.
      `TestCycleNamesAFailureThatWasNotAProbe`: a route the cycle could not read
      publishes `route_unreadable`.

      A degraded summary with no recorded cause reports `none` rather than
      borrowing one. That combination is not reachable from the cycle, only
      from a summary built without it, which is what test fixtures do.

- [x] 3.3 Keep `probe_failed` meaning a probe that failed; verified by a test that a failing probe still reports it.

      The word is kept for the one site where it is true: probes ran, answered,
      and no outer path was ready. `TestCycleReportsAFailedProbeAsAFailedProbe`.
      A probe that could not run at all is `endpoint_unreadable`, which is a
      different claim.

      A twelfth name, `outer_path_absent`, was added and then removed: it would
      have left `probe_failed` with no site that used it, and an unused entry in
      a closed vocabulary is a liability rather than a reserve.

- [x] 3.4 Stop calling a physical network the cycle could not read an intentional sleep; verified by a test over the suspended-with-failure path.

      Found while enumerating the failure sites, not before. The cycle both
      increments its failure counter and suspends when the physical network is
      unreadable or not ready, and the reason published for a suspended cycle
      was `intentional_sleep`. A runtime that could not read the network
      reported that it had chosen to rest.

      Reporting the recorded cause in preference to the state fixes this
      without a rule of its own: the cause is `physical_network_unready` and
      the state stays suspended. `TestCycleDoesNotCallAnUnreadableNetworkASleep`
      holds both halves.

## 4. Standing work is a quantity

- [x] 4.1 Publish `pending_operations` in the `ctl status` and `ctl diagnostics` payloads, computed from the cycle that produced the reported health; verified by a test over the payload with a known plan.

      `ipc.Status` carries the field, and `Diagnostics` embeds `Status`, so one
      field answers both commands. `Controller.Update` gained the count as a
      third argument rather than a setter of its own: the state and the count
      are written by the same call per cycle, so the two cannot drift apart.

      The root domain reports `len(summary.Plan.Operations)`. The user domain's
      planner proposes a single act, so `pendingActs` reports one or nothing
      rather than a count.

      `TestControllerReportsPendingWorkWithoutGradingTheHealth` reads the field
      from both payloads.

- [x] 4.2 Prove no persisted schema changed: `SnapshotSchemaVersion` is still 1 and a snapshot written before this change still loads; verified by a test that loads a fixture written at the current version.

      The user daemon treats an unreadable snapshot as fatal, so this is what
      keeps both the install and its rollback able to start.

      `TestPendingWorkIsNotPersistedInTheSnapshot` asserts the constant is still
      1 and decodes a snapshot in the shape written before this change,
      including a non-zero failure counter and attempt, then checks it against
      the package's own validity rule.

- [x] 4.3 Prove the count does not decide health; verified by a test where the count is non-zero and the health is sound.

      Held at both levels. In the cycle by
      `TestCycleIsSoundWhileHoldingAProposalItMayNotApply`, and in the payload
      by `TestControllerReportsPendingWorkWithoutGradingTheHealth`, which
      reports two operations standing beside a healthy state.

## 5. A quantity the path does not keep

- [x] 5.1 Omit from a role's reported payload the fields that role's path does not maintain, starting with `attempts` on the root path; verified by a test asserting the field is absent rather than zero.

      Four fields, not one. `nextRootOperatorSnapshot` writes the state, the
      failure counter, the tick and the generation, and nothing else:
      `attempts`, `recovering_since`, `next_action_at` and `safe_until` were all
      structurally zero on the root path and all published.

      They are pointers in `ipc.Diagnostics` now, nil when the path maintains
      none. A controller is told which at construction —
      `operator.KeepsNoRecovery` for the root path, `operator.ReportsRecovery`
      for the user path, whose snapshot is the recovery machine's own — so the
      declaration sits at the call site rather than being inferred from a role.

      `TestRootPathReportsNoRecoveryBudgetAtAll`.

- [x] 5.2 Prove the paths that do maintain it still report it; verified by a test over the recovery path's payload where a spent attempt appears.

      `TestRecoveryPathReportsTheBudgetItKeeps` reads a safe-mode snapshot's
      spent attempt back out of the payload. `TestAMeasuredZeroIsNotAnAbsentQuantity`
      holds the distinction the pointer exists for: a zero the path measured and
      a quantity the path does not keep are different claims.

- [x] 5.3 Hold the two shapes together; verified by the payload refusing a mixture.

      Mechanical rather than by inspection: the four quantities come from one
      machine, so `Diagnostics.valid()` accepts all four or none and refuses any
      mixture. A future field added to one path and not the other fails the
      gate instead of being published as a zero.
      `TestRecoveryQuantitiesAreReportedTogetherOrNotAtAll` covers none, one,
      all four, and a negative tick.

      This replaces the design's suggestion of a test asserting field-by-field
      which path writes what. An invariant the payload itself enforces is
      stronger than a test enumerating today's fields.

## 6. What an operator reads

- [x] 6.1 Say in `docs/macos/root-observe.md` how the health reads while the daemon runs, so a degraded cycle sends a reader to its named failure and a standing proposal to `pending_operations`; verified by the documentation gate.

      The task assumed a section that misdirected the reader. There was none:
      the document covered only why a runtime stopped, and what sent a reader
      after a probe that had not run was the `last_reason` field itself. So this
      is a section added, not rewritten — "Reading how it is while it runs",
      beside "Reading why it stopped".

      It carries the three fields and what each answers, the ten causes a cycle
      can now name, the rule that the first failure in configuration order is
      the one reported, and why four quantities are absent rather than zero. It
      also records the seven hours and that the two ingress routes standing on
      the wrong links are the owner's arrangement, not this runtime's to
      correct.

- [x] 6.2 Do the same for `docs/macos/user-observe.md`.

      Shorter, because this domain's health was already the machine's. It says
      which quantities this domain reports and the root one omits, that
      `pending_operations` is one or nothing here, and why the distinction was
      only accidentally safe on this side: the redundant clause requiring no
      standing act sits beside a state the machine reaches, while the root
      domain wrote its own state from the cycle.

- [x] 6.3 Record that a health which never changed was not a health; verified by the paragraph naming the seven hours and what they did not say.

      In the root document, under the standing-proposal rule: seven unbroken
      hours of `DEGRADED` with no failure of any kind, the tunnel present, its
      payload answering, and the kernel agreeing with every route the runtime
      had observed.

## 7. Mutation discipline

- [x] 7.1 Mutate the health condition, the reason selection and the payload assembly; verified by every survivor closed by a test or recorded with the reason it was left.

      A mutation that does not compile is rewritten, not counted. Sixteen
      mutations, none uncompilable, run against `internal/rootdaemon`,
      `internal/operator`, `internal/ipc` and `internal/userdaemon` with a
      180-second bound, each file restored and compared byte for byte before the
      next.

      First run: 13 killed, 3 survived.

      The health condition took four — always sound, sound only on failure,
      never sound, and the old clause counting the plan — and all four died. So
      did all three against the cause the cycle keeps: the last failure winning,
      the cause never recorded, and the counter not incremented. Both payload
      mutations against which path reports the recovery quantities died, as did
      both against the all-four-or-none rule.

      The three survivors were all claims made in prose that no test held:

      - a degraded summary with no recorded cause reporting `probe_failed`
        instead of `none`. Not reachable from the cycle — a degraded cycle has
        failed, and a failure records a cause — but it is what a summary built
        without the cycle carries, and the document states the behaviour. Closed
        by `TestADegradedSummaryWithNoCauseBorrowsNone`, which also holds the
        two states that have a reason of their own and the precedence of a
        recorded cause over all three.
      - the user domain's count always one, and always nothing. Closed by
        `TestPendingActsCountsOneActOrNone` over both actions the planner can
        propose and over none.

      Second run: 16 killed, 0 survived.

## 8. On the machine

- [x] 8.1 Install the built runtime and read the health in the first cycle after it starts; verified by a reading that gives the health, the reason and `pending_operations` together.

      Installed 2026-10-05T18:56Z, built in the same command. Read at 19:00Z:

      ```
      root  state=HEALTHY  pending=2  reason=probe_succeeded  attempts=ABSENT
      user  state=HEALTHY  pending=0  reason=probe_succeeded  attempts=0
      ```

      The root daemon had read `DEGRADED` for seven unbroken hours before this.

      The divergence was worse than the task recorded. A full field-by-field
      comparison against the installed configuration found **ten settings that
      installing the checkout would have removed** — the whole of
      `tunnel_supervision.execution`, which is the executor that change 3 built:
      its thresholds, its state paths, its handover binary, `sing_box` and the
      target key, plus a second trusted compiler digest — and two values that
      differ, `static_sha256` and `wake_threshold_seconds` (180 installed
      against 90 in the checkout). The user domain diverges too, by one digest
      and `static_sha256`.

      So no configuration was installed at all. Both installers keep the live
      file when the path given is the installed one, and both printed
      `configuration is already in place; keeping it`. The repository's plist was
      compared with the installed one first and is identical, so reinstalling it
      could not reduce the job's arguments.

- [x] 8.2 Prove the route divergence is still visible after the health stopped grading it; verified by the proposal events and the count in the same reading.

      `pending_operations: 2` in the same reading as `HEALTHY`, and
      `ingress_route_proposed` published at 18:56:46.710406Z and again at
      19:04:04.363972Z. The divergence did not become invisible; it stopped
      being called ill health.

- [x] 8.3 Prove a real failure still reads as unsound on this machine. Nothing was induced: two occurred on their own within eight minutes of the install, and natural occurrences are what the rule will meet.

      `observation_cycle result=degraded` at 18:57:47.30782Z and
      19:02:54.788408Z, between cycles that read `ok`.

      The cause is recoverable after the fact, which the task did not assume. It
      is not in the cycle's own record — that carries the result and no reason —
      but the event archive keeps the per-component facts, and at the same
      microsecond as each degraded cycle:

      ```
      18:57:47.307944Z  relay_ingress  failed  probe_failed  {configured:1, reachable:0}
      19:02:54.788714Z  relay_ingress  failed  probe_failed  {configured:1, reachable:0}
      ```

      The single configured outer path was unreachable. That is `probe_failed`
      in the one sense this change kept for it: the probes ran, answered, and
      left no outer path ready. The natural occurrence confirms the mapping
      rather than a forced one.

      Left unread: the other nine causes. Each would need its own occurrence,
      and nothing about this reading says what those will look like.

- [x] 8.4 Read the health again after a restart, to prove nothing about it was carried in a file; verified by a reading that follows a `kill -TERM` and the daemon's own restart.

      `kill -TERM` rather than `kickstart -k`, which leaves no stop record.
      `daemon_stopped result=ok` at 19:03:32.578192Z — an asked-for ending, not
      a failure — and `daemon_started` at 19:04:01.269787Z under `KeepAlive`.

      The reading after it: `state=HEALTHY`, `pending=2`, `attempts` absent, and
      generation **5**. The generation counts from zero at every start, so the
      health was reached again from observation rather than restored, which is
      the whole reason the count was kept out of the persisted snapshot.

## 9. Close

- [x] 9.1 Run the full gate and report any gate that did not run and why; verified by the gate's exit status, not by reading its output.

      `make check` returned 0. `make policy-qualification-status` returned 0
      with `lifecycle: complete` and `failed_evidence: false`, run because this
      change installed both daemons on this machine.

      Three gates did not run, and none of them is applicable to this diff,
      which touches `internal/control`, `internal/ctl`, `internal/ipc`,
      `internal/operator`, both daemons, two documents and this change:
      `postgres-test` (no migration or persistence change), `container-build`
      and `container-test` (no Dockerfile or ingest change), `terraform-test`
      and `terraform-state-test` (no module or fixture change). Docker and the
      Terraform CLI are both present on this machine, so they were skipped for
      being out of scope rather than unavailable.

- [x] 9.2 Sync the delta into the baseline, validate and archive; verified by the drift gate.

      The delta added one requirement, that a cycle's health answers soundness
      and not workload, and modified the observe-only output requirement to say
      that a proposal this plane cannot execute is not a divergence from health.
      Both are in the baseline capability `local-control-plane-foundation`.

- [x] 9.3 Record what this change leaves open.

      **HEX-11's remaining question.** The planner proposing the same two
      operations every cycle is correct for a runtime with no authority to apply
      them, and this change stops calling it ill health. Whether a standing
      proposal should be re-derived every cycle at all is untouched.

      **The same conflation one level down.** The read model's `scoped_routes`
      component reports `degraded` with `probe_failed` in every cycle, including
      the cycles that read `ok` — measured at 18:56:46, 18:58:44 and 19:04:04 on
      2026-10-05, with `{configured: 21, conflicting: 14, installed: 7}`. It is
      permanently degraded for the same reason the cycle used to be: the routes
      diverge and nothing may correct them. The cycle no longer inherits it; the
      component still carries it, and a reader of component health meets the
      same signal that cannot change. Its own change.

      **The checkout has fallen behind the machine.** `private/root-observe.json`
      is missing the whole of `tunnel_supervision.execution` — the executor
      change 3 built — and `private/user-observe.json` a trusted compiler
      digest. The installer's reduction guard would refuse such an install, so
      this is a hazard rather than a wound, but the divergence should be closed
      where it is: the settings exist only on the machine.

      **Nine causes unread.** One natural occurrence named `probe_failed`
      through `relay_ingress`. The other nine a cycle can report have not been
      seen on this machine, and nothing read here says what they will look like.

      **Where a past cause is read.** The cycle's own record carries its result
      and no reason; `last_reason` answers for the present only. A degraded
      cycle in the past is explained from the event archive, by the component
      facts at the same microsecond. That is a place to look, not a defect, but
      no document says it yet.
