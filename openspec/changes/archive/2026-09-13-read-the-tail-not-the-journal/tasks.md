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
- [x] 5.3 Install, restart, and measure the window between the process appearing and `daemon_started`.

      Measured 2026-09-13 across three restarts: 42.9, 33.5 and 33.3 seconds,
      against 169.9, 165.9 and 152.1 before. The replay cost is gone.

      The prediction that went with this change was "single seconds", and that
      was wrong. Thirty-three seconds of blindness remain, and they are now
      attributed rather than suspected. Sampling the process through the shorter
      window, with only this repository's frames shown:

      - the first seventeen seconds are `eventarchive.(*Archive).scan` beneath
        `eventarchive.Open`, which reads and decodes every archive record to
        compute one number — the highest sequence, which the filenames carry;
      - the rest is `spool.scanIndex` and `spool.parseStableName`, the listing
        of two spools holding 136,000 files.

      The first is the same defect this change removed, one store over, and it
      is not corrected here. The second is a stat per file and is real work.
- [x] 5.4 Correct the archive's open, which decodes every record for its highest sequence.

      Corrected here rather than deferred. The defect is the same one this
      change removed from the journals, the symptom is the same window, and
      closing the change with the symptom still standing would repeat what
      leaving it standing twice already did.

      Open takes the highest sequence from the listing the filenames carry.
      Mutations: decode every record for the maximum, and take the first
      sequence rather than the last. Both fail named tests.
- [x] 5.5 Install again and measure what the archive's correction returned.

      The archive's open went 3.873 seconds to 2.320 once the spool stopped
      walking its directory twice, and the replay this change is named for went
      3.016 to 0.108 for the same reason: the listing kept at open is the one the
      backwards walk asks for, so it costs nothing by the time it is wanted.

      The arc of the daemon's own work, measured at each step rather than
      predicted: about 152 seconds, then 33, then 20.1, then 7.0.

## 6. Close

- [x] 6.1 Sync the deltas into the baselines, validate, archive.
