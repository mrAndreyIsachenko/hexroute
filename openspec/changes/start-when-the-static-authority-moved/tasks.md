# Tasks

## 1. The startup answer

- [ ] 1.1 A handler whose store holds an active generation compiled against a different static digest is built rather than refused, and reports `restart_required` with reason `static_mismatch`; verified by a test that fails before the change.
- [ ] 1.2 The status names the generation from the store's lineage — bundle, domain generation and manifest digest, with no activation time; verified by a test that reads all four out of the reported status.
- [ ] 1.3 Such a handler authorizes nothing; verified by a test that every action path refuses while it is in that state.
- [ ] 1.4 Every other startup failure keeps the answer it had; verified by a test per failure — corruption, invalid signature, clock anomaly, unreadable store — asserting it is not reported as a static mismatch.
- [ ] 1.5 Mutate the classification, the source of the generation, and the authority it grants; confirm the named tests fail, and record the counts.

## 2. Both daemons

- [ ] 2.1 The root daemon starts in that state rather than rejecting its arguments; verified by a test over its startup path.
- [ ] 2.2 The user daemon does the same; verified the same way.

## 3. Gates and evidence

- [ ] 3.1 `make check` passes; verified by its exit status.
- [ ] 3.2 The runbook's sequence for changing static authority is written down as it now behaves, including what the operator will see between the restart and the activation; verified by following it.

## 4. On the machine

- [ ] 4.1 Install, change the static authority, and confirm both daemons come up reporting `restart_required` with the generation they cannot run; verified by `hexroutectl policy status`.
- [ ] 4.2 Install and activate the prepared generation 5, and confirm both domains report it active; verified the same way.
- [ ] 4.3 Confirm the grant reached the runtime: the tunnel question is answered `authorized`; verified by the recorded answer in the archive.

## 5. Close

- [ ] 5.1 Sync the delta into the baseline, validate, archive.
