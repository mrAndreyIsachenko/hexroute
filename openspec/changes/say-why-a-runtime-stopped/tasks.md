# Tasks

## 1. What ended the loop

- [x] 1.1 The observation loop returns what ended it alongside the error, from a closed set; verified by a test per exit that asserts the name.

      Six exits, five names: the entry guard, the log refusing the record the
      loop begins with, a summary whose state this runtime does not know, the
      fold's own record, the control state refusing an update, and the operator
      socket ending — with and without saying why.
- [x] 1.2 `Run` records the stop with `daemon_stopped` and `degraded`, naming what ended it, and the ordinary cancelled-context ending keeps `ok` and names nothing; verified by tests over both.
- [x] 1.3 The record goes to the error log rather than the journal it may have failed to write; verified by a test in which the info log fails and the stop is still reported.

      Driven through the real command: its log refuses the record the loop
      begins with, and the stop has to arrive on the other stream.
- [x] 1.4 Failing to write the record does not change the exit; verified by a test in which both logs fail.
- [x] 1.5 The reasons are allowlisted, and a stop cannot name one that is not; verified by the logging vocabulary's own gate.

      Five names rather than the seven the design first had. Two were removed by
      reading the code: the read model and the event archive report every
      failure of their own and carry on, so the fold's exit is the log refusing
      a record and nothing else. A name nobody can produce would have been a
      distinction the code does not make, asserted in a closed vocabulary.
- [x] 1.6 Mutate the classification and the placement; confirm the named tests fail, and record the counts.

      Nine applied, nine killed: every stop naming the journal, the socket
      ending naming it, the control state naming it, the fold naming a read
      model nobody can fail, a broken summary naming the journal, the record
      said through the journal it may have broken, the stop reported as asked
      for, its failure changing the ending, and an ending nobody failed on
      naming one anyway.

      One was rewritten rather than counted: removing the `errors.Is` branch
      left an unused import, and a mutation that does not compile is not a
      mutation the tests killed.

## 2. The other runtime

- [x] 2.1 The user daemon answers the same way; verified by tests over its own loop.

      Seven exits, five names. Two of them are the same state by two routes —
      the snapshot on disk and the controller's own — and answer under one.

      The publication's exit needed the real publisher rather than a fake,
      because the loop holds the concrete one: a root that does not answer costs
      the cycle nothing by design, so the fault had to be on this side, and it
      is a source that owns none of the components this domain speaks for.
- [x] 2.2 Mutate it too; record the counts.

      Nine applied, nine killed. Four survived the first run and every one of
      them was a test that did not exist yet: the publication's exit, the
      cycle's own record, and the three about where the stop is said, which
      needed the command driven for real as the root runtime's is.

## 3. Gates and evidence

- [ ] 3.1 `make check` passes; verified by its exit status.
- [ ] 3.2 The runbooks say what a stop record means and where to read it; verified by following them.

## 4. On the machine

- [ ] 4.1 Install both daemons and confirm an ordinary restart still records `daemon_stopped` with `ok`; verified by the logs around a deliberate restart.
- [ ] 4.2 Confirm a failure is recorded, by inducing one this runtime can be made to meet without damage; verified by the record naming it.

## 5. Close

- [ ] 5.1 Sync the delta into the baseline, validate, archive.
