# Tasks

## 1. Prove the silence before changing it

- [x] 1.1 A regression that fails against the current code: a request refused by
      the mutation gate leaves a record naming the gate, and a request no reader
      took leaves a record naming that.
- [x] 1.2 A regression that fails against the current code: the asking runtime
      reports a refusal and a failure of the runtime it asked as different
      reasons.
- [x] 1.3 Confirm by mutation — collapse each new record and each new reason back
      into what it replaced, and confirm the named test fails. Drive the
      mutations through the dispatcher and through the cycle, not through the
      helper that maps a code to an outcome: three times in the previous change
      a helper-level mutation passed while the call site was untested.

## 2. Give the outer layers a voice

- [x] 2.1 The dispatcher takes a reporter and calls it when the mutation gate
      refuses. The reason names the gate, not the act.
- [x] 2.2 The broker records an envelope no reader took, on the same path that
      answers `internal_error`.
- [x] 2.3 Root wires both to the stream its other refusal reports already use,
      and bounds repetition the way the connectivity publisher does.

## 3. Keep the two apart at the caller

- [x] 3.1 The unnamed-code arm of `refusalOutcome` gets its own outcome, distinct
      from the precondition arm.
- [x] 3.2 It maps to its own logging reason, and it reports a degraded result
      rather than a refusal: nothing refused this.
- [x] 3.3 `recovery_refused` keeps its meaning — the other side looked and
      disagreed — and stops carrying the case where it did not look.

## 4. Gates

- [x] 4.1 `make check`, judged by exit status.
- [x] 4.2 `make secret-test`, and read the new test names and fixtures by hand.

## 5. Prove it on the machine

- [ ] 5.1 Install root and the user daemon; confirm each running binary by digest
      and by the revision it carries.
- [ ] 5.2 Reproduce a gate refusal and a not-taken envelope against the running
      root, using a request that cannot perform the act, and read both records.
- [ ] 5.3 Record what each said.

## 6. Close

- [ ] 6.1 Sync the delta into the baseline and archive.
