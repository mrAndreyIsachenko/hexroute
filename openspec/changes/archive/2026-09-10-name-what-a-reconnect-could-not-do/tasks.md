# Tasks

## 1. Prove the collapse before changing it

- [x] 1.1 A regression that fails against the current code: each distinct
      failure of a reconnect must be tellable apart by a caller, and each must
      reach the user runtime's record as its own reason.
- [x] 1.2 Confirm by mutation — collapse each named error back into the umbrella
      and confirm the named test fails. Judge by exit status: a mutation that
      does not compile is not a mutation.

## 2. Name the failures

- [x] 2.1 Give each distinct stop its own sentinel wrapping `ErrReconnect`:
      credentials that could not be read, a client that would not start the
      session, and the code that could not be derived.
- [x] 2.2 Leave `no client` and `nothing to submit` under the umbrella. They are
      faults in the deployment or in this code rather than conditions of the
      machine.
- [x] 2.3 Keep `ErrWindowTooShort` as it is. It is already the shape the others
      are being given.

## 3. Carry the step into the record

- [x] 3.1 `recoveryOutcome` gains a value per named failure, as it already has
      per named refusal.
- [x] 3.2 Each maps to its own logging reason, and every one of them still
      reports a degraded result rather than a refusal — nothing refused this.
- [x] 3.3 The message text stays out of the log. The vocabulary is a closed
      allowlist and free text would carry whatever a future error string holds.

## 4. Gates

- [x] 4.1 `make check`, judged by exit status.
- [x] 4.2 `make secret-test`, and read the new test names and fixtures by hand.
      Nothing about a credential belongs in this repository, including in the
      name of a test about failing to read one.

## 5. Prove it on the machine

- [x] 5.1 Installed. The running binary was confirmed against the build by its
      own digest and by the revision it carries, not by the installer's report:
      `c05e8fca2346d69e`, built from `e53bb62` with this change's uncommitted
      work in the tree.
- [x] 5.2 The blackhole was induced by removing the client address from the
      tunnel interface. The planner recognised it and planned a rescue rather
      than a reconnect, so the failures this change names were never reached.
      What the runtime recorded was the rescue being refused:
      `pritunl_reconnect_proposed rejected recovery_refused`. The named
      reconnect failures remain unexercised on this machine, and the reason is
      structural rather than incidental: the one fault that can be induced by
      hand is the one the planner answers with a rescue.
- [x] 5.3 It cannot be told. The reason it cannot is an instrument, not the
      machine.

      Root answered a refusal and wrote nothing about it.
      `pritunl_rescue_refused` had never appeared in any of root's logs.

      Measured afterwards against the same running root, the rescue path is
      whole. A request carrying a generation root cannot be at was answered
      `stale_generation` in 4ms, and root recorded `pritunl_rescue_refused /
      generation_conflict` 3.6ms later. So the rescuer was built, the mutation
      gate opens, and the refusal record works.

      Two branches answer without writing: the dispatcher's mutation gate,
      which refuses `precondition_failed` above the rescuer, and the broker's
      internal response when no reader takes the envelope. The asking runtime
      cannot separate them either — `refusalOutcome` maps
      `precondition_failed` and every unnamed code, `internal_error` among
      them, to one reason. Neither side wrote down which it was.

      A third measurement bounds the question without settling it. Root's
      observation loop stops answering for 6.6 to 13.1 seconds once every ~70
      seconds: 127 requests at first, median 5.6ms, p95 21ms, five inside a
      cycle, and 400 more afterwards. That first sample looked like a pause
      that was lengthening; the longer one does not support it. Under the I/O
      deadline a
      blocked request is still answered correctly, only late; over it the
      caller's own deadline expires first and it records a failure rather than
      a refusal. Blocking alone therefore does not produce what was recorded.

      Nothing here is a statement about Pritunl, and nothing here is an
      artefact of the induction. Three defects were found; none belong to this
      change:
      - the mutation gate refuses without recording a reason, and it is the
        only place a `precondition_failed` leaves root unwritten;
      - `refusalOutcome` collapses "root looked and disagreed" into "root
        failed internally";
      - root's observation cycle blocks its own operator loop for seconds at a
        time, and the pause is growing.

## 6. Close

- [x] 6.1 Sync the delta into the baseline and archive.
