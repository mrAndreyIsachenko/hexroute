# Tasks

## 1. Prove the ground before moving anything

- [ ] 1.1 Compare this runtime's targets against what both of the supervisor's route scripts apply, and record any destination only the supervisor knows.
- [ ] 1.2 Publish the first signed configuration version from the bytes running today, and prove it is byte for byte identical.

## 2. The claim

- [ ] 2.1 Write the claim: its shape, where it lives, who may write it.
- [ ] 2.2 Have the supervisor read it every tick and before every start, and not start the process while it is held.
- [ ] 2.3 Test that a supervisor restarted while the claim is held does not take the process back.

## 3. The transaction

- [ ] 3.1 The durable session record: phase, the previous owner's state, what this runtime started.
- [ ] 3.2 Refuse to start a transaction beside one already in flight.
- [ ] 3.3 Complete on two consecutive proofs of traversal inside the deadline.
- [ ] 3.4 Abort on the deadline: remove the claim, stop what was started, let the supervisor take it back.
- [ ] 3.5 A later invocation can abort what it finds.

## 4. Starting the tunnel from a signed version

- [ ] 4.1 Verify at every start, against this host, without reaching the network.
- [ ] 4.2 Refuse to start on a version that does not verify, and return ownership.

## 5. The rehearsal

- [ ] 5.1 Every phase except the claim and the start, reported as a rehearsal.
- [ ] 5.2 Run it on the machine and record what it found.

## 6. Gates and evidence

- [ ] 6.1 Mutate the claim, the completion rule, the abort and the verification; confirm the named tests fail.
- [ ] 6.2 `make check` green.
- [ ] 6.3 Run the real transaction and record what the tunnel did.

## 7. Close

- [ ] 7.1 Sync the delta into the baseline, validate, archive.
