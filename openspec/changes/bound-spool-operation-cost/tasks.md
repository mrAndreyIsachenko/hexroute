# Tasks

## 1. Measure Before Changing

- [ ] 1.1 Take the reproduction saved with the diagnosis and confirm the growth independently. The profile named the frames; the test is what holds the claim after the code changes.
- [ ] 1.2 Record what each of the six `scanStable` callers actually needs. Two hand records out and must keep decoding; the rest need sequences and sizes, and the difference between those two sets is the whole change.

## 2. Prove What You Use

- [ ] 2.1 Obtain sequences from filenames and sizes from file metadata on the paths that do not hand records out. Assert no stored payload is decoded to complete an append, and that appending does not recompute a size the filesystem already reported.
- [ ] 2.2 Keep decoding where records are handed to a caller. Assert every returned record is proved before it leaves.
- [ ] 2.3 Assert the cost of appending, measuring and acknowledging does not grow with the number of stored entries, at the scale the machine reached rather than at a convenient one.
- [ ] 2.4 Assert no in-memory index was introduced. Disk stays the truth; a cache diverges quietly, which is what the re-reading exists to prevent.

## 3. Let A Damaged Record Stop Only Itself

- [ ] 3.1 Set aside and report a record that cannot be proved, and keep appending. Assert a new observation is recorded while a damaged record is stored — the observation is the larger loss.
- [ ] 3.2 Assert setting aside does not delete. The damaged record stays on disk because it is the only evidence of what damaged it.
- [ ] 3.3 Assert the spool still refuses entirely when the directory itself is unusable — wrong ownership, wrong mode, unreadable. Nothing about it can be trusted then.

## 4. Stop Carrying A Peer's Slowness

- [ ] 4.1 Derive the publication deadline from the observation interval and assert it stays below one cycle when the interval is reconfigured. A constant drifts out of that relationship without saying so.
- [ ] 4.2 Assert a publication that does not complete is abandoned rather than buffered, and that the cycle continues on schedule. The existing behaviour is correct and deliberate; only the waiting was wrong.

## 5. Prove It On The Machine

- [ ] 5.1 Add the journal-level test. The daemon does not call the spool; it calls the journal, and a mistake fits between them.
- [ ] 5.2 Live acceptance, stated as behaviour: the root daemon answers `policy status` promptly while the user daemon publishes, against the real store. It cannot do that now, and sixty thousand real records exist in one place.
- [ ] 5.3 Keep the evidence privately. It carries the operator's live paths and state.

## 6. Finish What This Unblocks

- [ ] 6.1 With the root daemon answering, complete the activation blocked at task 7b of `recover-policy-generations-after-expiry`: prepare and commit generation 3 across both domains. Its window closes 2026-09-16.
- [ ] 6.2 Record the abandoned state directories — `connectivity.pre-fold-position`, `connectivity.desynced`, the `*.superseded` copies — in the cleanup item, so whoever decides their fate does it by reading rather than by tripping over them.

## 7. Verify

- [ ] 7.1 Run `make check` and resolve every failure.
- [ ] 7.2 For each new property, restore the defect it guards and confirm the named test fails. A test that still passes with the rescan put back is measuring something else.
- [ ] 7.3 Run `openspec validate bound-spool-operation-cost --strict` and keep proposal, design, specs and tasks consistent with what was built.
- [ ] 7.4 Sync the delta into the baseline specs and archive.
