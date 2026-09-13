# Tasks

## 1. Prove the defect before removing it

- [x] 1.1 Write a test that replays from a nearly current watermark against a journal holding many records, counting how many are opened; it must fail on the current implementation.
- [x] 1.2 Write a test that rebuilding the watermark from a broken lineage opens one record per journal rather than all of them.

## 2. The spool hands out less than everything

- [x] 2.1 Add `Sequences`, from the kept listing, opening nothing.
- [x] 2.2 Add `Entry`, which proves one record and reports absence as an answer.

## 3. Read the tail

- [x] 3.1 Walk backwards in `RecordsAfter`, stopping at the first fact at or below the watermark.
- [x] 3.2 Step over records that are not connectivity facts; keep an undecodable record an error.
- [x] 3.3 Take the watermark in `highestIssued` from the newest fact of each journal.

## 4. The guard that catches a wrong suffix

- [x] 4.1 Test that a journal whose fold positions do not rise yields a range reported as not continuous, and that startup refuses it.

## 5. Gates and evidence

- [x] 5.1 Mutate the walk and the stopping condition; confirm the named tests fail.

      Five mutations fail a named test: read the whole journal again, stop one
      record late, hand the tail back newest first, take the watermark from the
      oldest record, and stop at a record of another class.

      The last of those survived at first, and the reason was the fixture rather
      than the test. Nothing in this package's journals held a record that was
      not a fact, while the live one does constantly: a spool at its bound writes
      its own overflow incidents into the spool it is evicting from. The gap was
      closed by bounding a journal and overflowing it, which is how the machine
      makes them, rather than by writing one by hand.

      One mutation is recorded as untested. Treating a record evicted between the
      listing and the read as the end of the journal cannot be reached without
      injecting that race, and it is defensive rather than load-bearing.
- [x] 5.2 `make check` green.
- [ ] 5.3 Install, restart, and measure the window between the process appearing and `daemon_started`.

## 6. Close

- [ ] 6.1 Sync the deltas into the baselines, validate, archive.
