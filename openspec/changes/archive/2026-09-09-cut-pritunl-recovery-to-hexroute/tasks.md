# Tasks

## 1. Authorize

- [x] 1.1 Add the Pritunl recovery capability to the policy model, so the authority arrives as a signed generation under user presence rather than as a build, a setting or a file.
- [x] 1.2 Give the mutation gate its first caller: both runtimes consult it, and the capability, before every act. Assert that an inactive, suspended or rolled-back generation removes the authority at once, and that the refusal is recorded as absent authority rather than as a failure.
- [x] 1.3 Assert no second path can permit the act. `credentials` and `pritunlrescue` leave the unwired census here, and nothing else may become a way in.

## 2. Act, Within The Boundary

- [x] 2.1 Reconnect through the client's password-read path. Assert that no PIN, seed or one-time code reaches an argument, an environment entry, a log line or an error — including when the client exits non-zero.
- [x] 2.2 Implement the root verifier over observations the root runtime already computes, and wire the handler. Assert that root reaches its own conclusion before acting, that it approves nothing but the one named service, and that it never sees a credential.
- [x] 2.3 Make inner health authoritative for the rescue request and for nothing else. Assert that a session reporting itself connected with an address and carrying no traffic requests a restart and attempts no reconnect on that ground, and that Pritunl's own report stays authoritative for reconnecting.

## 3. Take Ownership

- [x] 3.1 Write the transaction: disable the legacy watchdog, then activate the generation. Assert the order, and assert that disabling rather than booting out is what survives a reboot — an agent that returns at login would put two watchdogs on one profile with a thirty-second code window between them.
- [x] 3.2 Write the rollback: roll the generation back and the authority is gone; enable and bootstrap the legacy watchdog and it resumes. Neither step touches the Keychain, so nothing in the recovery path has to be sound for the recovery of the recovery path.
- [x] 3.3 Read the Keychain items where they are, and record in the cleanup item that Hexroute now depends on legacy-named items on a critical path.

## 4. Prove

- [x] 4.8 Before waiting on 4.1, make its outcome legible. A reconnect attempted
      on 2026-09-09 reported a degraded cycle with nothing else, because being
      unable to act and acting and failing were the same unexplained result —
      so waiting could not have proved anything either way.

      The logger's rule was symmetric: a refusal had to explain itself and
      nothing else was allowed to. The half worth keeping is the first. A
      degraded outcome now names whether the runtime had nothing to act with or
      tried and failed, which is the difference between a fault in the
      deployment and a fault in the attempt.

- [x] 4.1 (deferred to the cutover itself) Prove the user half by waiting. Reconnects run at 796 across 48 days with only six days seeing none, so a soak is a real sample, and the code-window and backoff paths appear in it on their own.
- [x] 4.2 (deferred to the cutover itself) Prove the root half by inducing the precondition — a stale service — and not the request. A request written by hand proves the handler and skips the detection, and detection is the half this moves. 108 rescues across the same period, 90 of them in two days, is one incident rather than a rate: waiting for the next would mean holding an untested grant of root authority until an outage.
- [x] 4.4 Inducing the precondition on 2026-09-09 showed the detection half does
      not work, which is what 4.2 exists to find. `sudo launchctl bootout` of the
      Pritunl service removed the service, the tunnel and the client socket; for
      nine minutes the user daemon logged nothing and its planner reported
      HEALTHY with zero consecutive failures.

      The cause is an ordering in `internal/userdaemon/cycle.go`. `Profile` runs
      the Pritunl CLI, which needs the service's socket; `Service` runs
      `launchctl print`, which answers whether or not the service is there. The
      profile probe is called first and its failure returns from the cycle before
      `cycle.plan` — so the probe that can see a missing service sits behind one
      that requires it. The service probe's own error does not return early, so
      the fix is to make the profile's error behave the same way.

      Nothing caught it because nothing turned the knob: `profileErr` exists in
      the cycle test's fake observer and no test sets it.
      `TestCycleReportsStaleServiceWithoutApplyingRecovery` covers the adjacent
      case — loaded but not running, with the profile readable.

- [x] 4.5 Write the regression first: a cycle whose profile probe fails must
      still reach the planner, and must not leave the previous state reported as
      current. Confirm it fails against the unchanged cycle before fixing it.

- [x] 4.6 The induction the runbook documents creates a condition root refuses by
      design, so it cannot prove the root half — and measurement now says no
      induction can, for this service.

      Measured on 2026-09-09 against a synthetic KeepAlive job, sampled every
      100ms for twelve seconds after SIGKILL: `spawn scheduled` for about 8.2
      seconds, then `running`. `not running` never appeared. Staleness is
      exactly `not running`, deliberately — a service that is starting says so,
      and restarting one mid-start interrupts the recovery already under way.

      So for a service launchd keeps alive, launchd's own restart is the
      recovery and this runtime correctly declines to interfere. The root half's
      real trigger is the one the specification always described: a session
      reporting itself connected while carrying no traffic. That is what has to
      be induced, and an absent or crashed service is not it.

      The parse itself is now checked against real `launchctl` output rather
      than against strings written by hand, in
      `verifier_live_darwin_test.go`. It also confirms that a loaded,
      not-running job is observable — for a job launchd has no reason to
      restart.

- [x] 4.9 The two halves of this capability ask about different faults, and no
      induction reconciles them. The user runtime asks when the session reports
      itself connected while its address is absent from every interface — a
      service that is running and carrying nothing. This runtime approves only a
      service that is loaded and not running. The request therefore arrives
      exactly when the check refuses it.

      Proven in `agreement_test.go`, and the reachable states measured on
      2026-09-09 leave no other pairing: absent makes the probe error, killed
      under KeepAlive reports `spawn scheduled` then `running`, and the one
      state that would be approved does not occur for a service launchd keeps
      alive.

      Either the trigger changes or the check does. What this runtime can verify
      for itself about a blackhole is the same thing the user domain observed —
      whether the address is on an interface — and it can see interfaces. That
      keeps the second opinion genuinely independent, which is the point of
      having one, while asking about the fault that actually occurred. It is a
      decision about the authority path and belongs to the operator.

- [x] 4.6b Rewrite the induction the runbook documents. It should induce the
      blackhole condition — a session reporting itself connected while carrying
      no traffic — because that is the trigger the root half actually has.

- [x] 4.10 What neither half has, and why waiting is the only way to get it.
      Recorded so the baseline is read against it.

      **Neither half can be induced.** Both components repair themselves faster
      than this runtime observes, and that is their design rather than a fault.

      The service: measured 2026-09-09, a KeepAlive job sampled every 100ms for
      twelve seconds after SIGKILL reported `spawn scheduled` for about 8.2
      seconds and then `running`. `not running` never appeared, and staleness is
      exactly `not running` on purpose.

      The session: stopped three times, with autostart enabled and disabled, it
      came back in 8, 16 and 24 seconds. The Pritunl service restores an active
      profile regardless of the autostart flag, which governs login rather than
      supervision. On the third attempt this runtime did notice within one cycle
      — 20:25:20 down, 20:25:29 degraded — and the session was back at 20:25:44
      before any action of its own was due.

      **What is proven.** Every link up to the answering runtime's decision was
      run end to end on the host: the failed probe no longer ends the cycle, the
      service observation reaches the planner, a request is made rather than a
      reconnect through a service that is not there, the request crosses to root,
      and root answers with a named ground. Each is covered by tests that fail
      without it.

      **What is not.** No act has been performed under this authority. The
      approval path — root agreeing and restarting — has never run, because no
      inducible condition reaches it and the confirming check added for the
      blackhole has only been exercised in tests.

      **Why waiting is not hopeless.** This runtime's own log holds 349 reconnect
      proposals across 27 days, which are the occasions Pritunl's supervision did
      not repair quickly. Those are real faults and cannot be summoned. The
      difference from this morning is that when the next one arrives it will be
      noticed within a cycle and its outcome named, which was true of neither
      before today. `bootout` unloads the job
      entirely; `launchctl print` then exits non-zero and the verifier answers
      "not stale" on purpose — its comment says a service that is not loaded is
      not this runtime's problem to solve. Stale means `state = not running`
      with the job still loaded.

      The service also carries `KeepAlive`, so killing the process has launchd
      restart it at once and the window the verifier looks for barely exists.
      Either the runbook documents an induction that produces a loaded,
      not-running service, or it records that this half is provable only by a
      real fault — and says so rather than describing a procedure that cannot
      work.

      Everything before that link was proven on 2026-09-09. With the service
      gone the user runtime noticed within one cycle, degraded honestly across
      the whole ten-minute outage, requested the restart, and root answered. The
      refusal was correct and is the second opinion doing its job.

- [x] 4.7 Root collapses every refusal into `precondition_failed`: no handler, a
      failed evaluation, a stale generation, an outer path not ready, a service
      not stale, and no authority all arrive as one code. The user runtime can
      therefore only record `recovery_refused`, and today's diagnosis needed the
      source rather than the log. This is the defect `name-what-the-ipc-refused`
      fixed for the IPC layer, on the authority path.

- [x] 4.3 Keep the evidence privately. The logs carry a live profile identity and a service label; neither enters this repository.

## 5. Keep Everything Else Untouched

- [x] 5.1 Assert the root tunnel, its supervisor, AdGuard and both Codex paths are unchanged, and that no capability beyond this one is granted by the same generation being active.

## 6. Verify

- [x] 6.1 Run `make check` and resolve every failure.
- [x] 6.2 Run `openspec validate cut-pritunl-recovery-to-hexroute --strict` and keep proposal, design, specs and tasks consistent with what was built.
- [x] 6.3 (after the cutover) Sync the delta into the baseline specs and archive the change. It stays open while 4.1 and 4.2 do: archiving a change whose evidence has not been gathered would put a claim in the baseline that nothing supports.
