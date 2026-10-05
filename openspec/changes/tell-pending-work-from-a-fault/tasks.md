# Tasks

## 1. The ground

- [ ] 1.1 Record what was already known and what is new, so this change does not claim a reading it inherited; verified by the earlier task that holds it.

      `hold-the-tunnel-under-a-grant` task 1.1 read on 2026-09-25 that the two
      standing operations swap the two ingress targets, and concluded that
      neither is drift: the live arrangement is the one the runtime owning the
      tunnel enforces, and this runtime's preferences are its inverse. That
      reading is inherited, not made here.

      What is new is the consequence, which no task records: because the
      proposal is right and standing, the cycle's health can never be sound, so
      the health reports the same value forever and reports nothing by it.

- [ ] 1.2 Record the state the health was in before this change, from the machine; verified by a reading that names the duration and the counters together.

      Measured 2026-10-05: root `DEGRADED` unbroken from 04:12:45Z to 11:36Z,
      `consecutive_failures: 0`, `attempts: 0`, `last_reason: probe_failed`,
      with `link_present: true`, `link_failures: 0`, `payload_failures: 0` and
      the kernel agreeing with every carried route the runtime had observed.
      One `observation_cycle` record covers the whole stretch because the
      change gate suppresses a result that has not changed.

- [ ] 1.3 Read how many operations stand unapplied right now and from which roles, so the quantity this change publishes has a value to be checked against on the machine.

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

- [ ] 5.1 Omit from a role's reported payload the fields that role's path does not maintain, starting with `attempts` on the root path; verified by a test asserting the field is absent rather than zero.

- [ ] 5.2 Prove the paths that do maintain it still report it; verified by a test over the sentinel or recovery path's payload where a spent attempt appears.

- [ ] 5.3 Hold the two shapes together; verified by a test asserting every field in a role's reported payload is written by that role's path.

## 6. What an operator reads

- [ ] 6.1 Rewrite the section of `docs/macos/root-observe.md` that sends a reader after a probe, so it says a degraded cycle names its failure and a standing proposal is read from `pending_operations`; verified by the documentation gate.

- [ ] 6.2 Do the same for `docs/macos/user-observe.md`.

- [ ] 6.3 Record that a health which never changed was not a health; verified by the paragraph naming the seven hours and what they did not say.

## 7. Mutation discipline

- [ ] 7.1 Mutate the health condition, the reason selection and the payload assembly; verified by every survivor closed by a test or recorded with the reason it was left.

      A mutation that does not compile is rewritten, not counted.

## 8. On the machine

- [ ] 8.1 Install the built runtime and read the health in the first cycle after it starts; verified by a reading that gives the health, the reason and `pending_operations` together.

      Install the build from the same command that builds it. The checkout's
      private root configuration diverges from the installed one —
      `wake_threshold_seconds` 90 against 180 — so the installed configuration
      is left in place and the divergence is recorded, not resolved here.

- [ ] 8.2 Prove the route divergence is still visible after the health stopped grading it; verified by the proposal events and the count in the same reading.

- [ ] 8.3 Prove a real failure still reads as unsound on this machine, under a condition induced once; verified by a reading of the health and the named cause while the condition holds.

      One induced occurrence proves the forced mode only. Record what natural
      occurrence, if any, this leaves unread.

- [ ] 8.4 Read the health again after a restart, to prove nothing about it was carried in a file; verified by a reading that follows a `kill -TERM` and the daemon's own restart.

## 9. Close

- [ ] 9.1 Run the full gate and report any gate that did not run and why; verified by the gate's exit status, not by reading its output.

- [ ] 9.2 Sync the delta into the baseline, validate and archive; verified by the drift gate.

- [ ] 9.3 Record what this change leaves open: HEX-11's remaining question, whether a standing proposal should be re-derived every cycle at all.
