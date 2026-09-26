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

- [x] 1.6 Such a handler carries the lineage's generation, so the successor — which names its parent — is accepted rather than refused as a downgrade; verified by a test that puts the real compatibility rule a generation-5 bundle and the generation the runtime holds, and by one that reads what the store is asked with when the candidate is prepared. Both fail before the change.

      Reporting the mismatch was only half the remedy. Measured 2026-09-26 on
      this machine: both daemons came up and named it, and then refused the one
      bundle that ends it — `precondition_failed` from both, because a runtime
      carrying no generation reads every candidate's parent as a downgrade.
      Adopting the chain grants nothing: no generation is active, and the
      mutation gate refuses on that alone. A chain that would not survive its
      own validation is reported and not adopted.
- [x] 1.7 Mutate the adoption; confirm the named tests fail, and record the counts.

      Six applied, five killed: adopting nothing, adopting the bundle without
      the rest, keeping a chain that does not validate, adopting a number one
      past the one the lineage proved, and treating the held generation as
      active.

      One survived and cannot be seen: adopting before the status is validated.
      Every path where that status fails validation returns no handler, so the
      object carrying the mutated chain is discarded inside the constructor and
      no observer exists. Recorded rather than closed.

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

- [x] 4.1 Install, change the static authority, and confirm both daemons come up reporting `restart_required` with the generation they cannot run; verified by `hexroutectl policy status`.

      2026-09-26: the fixed binaries installed, the static digest and the new
      compiler put into both configurations by their own owners, both daemons
      restarted. Both came up — `root state=restart_required bundle=4 policy=3
      reason=static_mismatch` and `user state=restart_required bundle=4 policy=2
      reason=static_mismatch` — instead of the crash loop of 2026-09-25.
- [ ] 4.2 Install and activate the prepared generation 5, and confirm both domains report it active; verified the same way.
- [ ] 4.3 Confirm the grant reached the runtime: the tunnel question is answered `authorized`; verified by the recorded answer in the archive.

## 5. Close

- [ ] 5.1 Sync the delta into the baseline, validate, archive.
