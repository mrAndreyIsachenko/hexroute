# Tasks

## 1. Prove the defect before changing it

- [ ] 1.1 Add a regression asserting that appending to an archive holding tens
      of thousands of records opens no stored record. Assert the property, not a
      duration: make the stored records undecodable and require that the append
      neither fails on them nor notices them, the way the spool's test does. It
      must fail against the current implementation.
- [ ] 1.2 Add a regression for the age walk: with the oldest record inside the
      window, no further record is read; with some expired, reading stops at the
      first retained record inside the window.
- [ ] 1.3 Confirm both by mutation — apply the change, then reintroduce the full
      scan and confirm each named test fails.

## 2. Take size from the directory

- [ ] 2.1 Give the archive a directory scan that returns sequence and size from
      `ReadDir` and `Lstat` without reading any record, and refuses a directory
      whose shape cannot be trusted — a name that is not a record, a symlink, a
      subdirectory, a duplicate sequence.
- [ ] 2.2 Evaluate the byte bound from that scan.
- [ ] 2.3 Keep `Size()` answering from metadata rather than re-reading records.

## 3. Establish age from the oldest record only

- [ ] 3.1 Read the lowest retained sequence to learn the oldest record's stamp.
- [ ] 3.2 When that record is inside the window, complete the append without
      reading another record.
- [ ] 3.3 When it is outside, walk upward reading only expired records and stop
      at the first record inside the window.
- [ ] 3.4 A record that cannot be proved on that walk is set aside and reported,
      and the walk continues from the next sequence — it does not stop the
      append.

## 4. Keep eviction exactly as specified

- [ ] 4.1 Read records for priority only on the path that evicts for size.
- [ ] 4.2 Preserve priority order: diagnostics before operational before
      critical.
- [ ] 4.3 Preserve the overflow record naming the class dropped and the sequence
      range covered, for both the age and the size bound.
- [ ] 4.4 Preserve the refusal when only critical records remain and the bound is
      still exceeded, and keep it visible as an overflow condition.
- [ ] 4.5 Confirm the existing `local-event-archive` scenarios still pass
      unchanged — this change is about cost, and any behaviour difference here is
      a defect in the change.

## 5. Measure what changed

- [ ] 5.1 Record append cost at a thousand, ten thousand and forty-one thousand
      stored records, before and after, in the same way the spool's numbers were
      taken.
- [ ] 5.2 Confirm the journal's mirror no longer makes a published fact pay a
      full scan twice.

## 6. Gates

- [ ] 6.1 `make check`.
- [ ] 6.2 `make secret-test` clean, and read the new fixtures and test names by
      hand: no live hostname, endpoint or evidence enters this public repository.
- [ ] 6.3 Confirm no coexistence boundary is touched — AdGuard, both Codex paths,
      the disjoint labels, paths and sockets, and the split between root network
      authority and user Keychain access are all unaffected by a change to a
      root-owned local store.

## 7. Deploy and confirm on the live host

- [ ] 7.1 Build and install the root daemon with
      `scripts/macos/observe-root-launchd.sh`, keeping the previous binary for
      rollback.
- [ ] 7.2 Confirm the root daemon's CPU falls from the measured 23.3 seconds per
      30 seconds of wall time.
- [ ] 7.3 Confirm connectivity publications are accepted: `connectivity-stream.json`
      advances every cycle, and the user daemon logs no `connectivity_publication`
      refusal.
- [ ] 7.4 Restore the publication deadline to `interval/3` by installing a user
      daemon built from the branch, replacing the experimental twelve-second
      binary now on the host, and confirm publications still land.

## 8. Close

- [ ] 8.1 Reconcile the delta into the baseline and archive the change.
