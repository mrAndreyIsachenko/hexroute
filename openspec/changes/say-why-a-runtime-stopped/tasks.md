# Tasks

## 1. What ended the loop

- [ ] 1.1 The observation loop returns what ended it alongside the error, from a closed set; verified by a test per exit that asserts the name.
- [ ] 1.2 `Run` records the stop with `daemon_stopped` and `degraded`, naming what ended it, and the ordinary cancelled-context ending keeps `ok` and names nothing; verified by tests over both.
- [ ] 1.3 The record goes to the error log rather than the journal it may have failed to write; verified by a test in which the info log fails and the stop is still reported.
- [ ] 1.4 Failing to write the record does not change the exit; verified by a test in which both logs fail.
- [ ] 1.5 The reasons are allowlisted, and a stop cannot name one that is not; verified by the logging vocabulary's own gate.
- [ ] 1.6 Mutate the classification and the placement; confirm the named tests fail, and record the counts.

## 2. The other runtime

- [ ] 2.1 The user daemon answers the same way; verified by tests over its own loop.
- [ ] 2.2 Mutate it too; record the counts.

## 3. Gates and evidence

- [ ] 3.1 `make check` passes; verified by its exit status.
- [ ] 3.2 The runbooks say what a stop record means and where to read it; verified by following them.

## 4. On the machine

- [ ] 4.1 Install both daemons and confirm an ordinary restart still records `daemon_stopped` with `ok`; verified by the logs around a deliberate restart.
- [ ] 4.2 Confirm a failure is recorded, by inducing one this runtime can be made to meet without damage; verified by the record naming it.

## 5. Close

- [ ] 5.1 Sync the delta into the baseline, validate, archive.
