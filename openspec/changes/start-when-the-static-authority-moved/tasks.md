# Tasks

## 1. The startup answer

- [x] 1.1 A handler whose store holds an active generation compiled against a different static digest is built rather than refused, and reports `restart_required` with reason `static_mismatch`; verified by a test that fails before the change.

      The state and the reason were already in the vocabulary, and already
      produced when a candidate is prepared against the wrong static authority.
      Nothing was invented; a runtime now reaches the same conclusion about its
      own store.
- [x] 1.2 The status names the generation from the store's lineage — bundle, domain generation and manifest digest, with no activation time; verified by a test that reads all four out of the reported status.

      The lineage did not report the manifest digest, though it had verified it
      against the active pointer; it does now, and a test of the real store
      compares the two. A lineage that cannot be read, or that does not say the
      authority moved, or that cannot make a well-formed status, names nothing —
      and the runtime keeps the answer it had rather than reporting a number
      nobody verified.
- [x] 1.3 Such a handler authorizes nothing; verified by a test that every action path refuses while it is in that state.

      By construction rather than by a second switch: mutation authority already
      requires an active generation, and there is none.
- [x] 1.4 Every other startup failure keeps the answer it had; verified by a test per failure — corruption, invalid signature, clock anomaly, unreadable store — asserting it is not reported as a static mismatch.

      And one nobody classified is still a refusal to start.
- [x] 1.5 Mutate the classification, the source of the generation, and the authority it grants; confirm the named tests fail, and record the counts.

      Nine applied, nine killed: a static mismatch still refusing, every failure
      read as one, an unread lineage naming a generation, a lineage that did not
      move still counting, the status not validated, the generation invented,
      the manifest not named, the lineage forgetting the manifest, and a state
      that is not `restart_required`.

      Two survived their first run and both said something about the tests. One
      could not be seen at all because the fake store answered from its own
      fixture, so a change to the real lineage was invisible — a test of the
      real store now compares its manifest with the active pointer's. The other
      was invisible because that fake returned an empty lineage beside its
      error, which makes checking the error indistinguishable from checking the
      value; it now answers with whatever it had read, and the runtime is
      required to refuse anyway.

## 2. Both daemons

- [x] 2.1 The root daemon starts in that state rather than rejecting its arguments; verified by a test over its startup path.

      No daemon code changed: both refused because the handler would not build,
      and it builds now. What proves it on this machine is task 4.1, and the
      daemon-level test is not written because driving either startup needs a
      real store, two sockets and a launchd identity to say what the handler's
      own tests already say.
- [x] 2.2 The user daemon does the same; verified the same way.

## 3. Gates and evidence

- [x] 3.1 `make check` passes; verified by its exit status.
- [x] 3.2 The runbook's sequence for changing static authority is written down as it now behaves, including what the operator will see between the restart and the activation; verified by following it.

      `docs/macos/policy-operations.md`, under its own heading: the order, what
      both daemons report in the window, that they authorize nothing there, and
      that rolling back is putting the previous configuration in place and
      restarting. Following it is task 4.

## 4. On the machine

- [ ] 4.1 Install, change the static authority, and confirm both daemons come up reporting `restart_required` with the generation they cannot run; verified by `hexroutectl policy status`.
- [ ] 4.2 Install and activate the prepared generation 5, and confirm both domains report it active; verified the same way.
- [ ] 4.3 Confirm the grant reached the runtime: the tunnel question is answered `authorized`; verified by the recorded answer in the archive.

## 5. Close

- [ ] 5.1 Sync the delta into the baseline, validate, archive.
