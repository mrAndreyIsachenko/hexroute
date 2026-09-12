# Tasks

## 1. Prove the defect before removing it

- [x] 1.1 Write a test that fills a spool to its bound and appends, counting how many stored records are opened; it must fail on the current implementation.
- [x] 1.2 Write a test that appends repeatedly to a spool held at its bound and asserts no stored record is opened twice.

## 2. Carry the priority beside the listing

- [x] 2.1 Give `stableRecord` a priority that is empty until something needs it, and leave `scanIndex` reading nothing.
- [x] 2.2 Add `readPriority`, which reads the wire envelope and stops: no canonicalisation, no re-marshal, no event.
- [x] 2.3 Set the priority from the staged entry in `noteCommitted`, so a record the spool published is never read back.
- [x] 2.4 Add the classifying step that fills in what the listing does not know, reading each record at most once.

## 3. Evict from what the directory reports

- [x] 3.1 Change `chooseEvictions` to choose among listed records rather than decoded entries, and prove the choice is unchanged.
- [x] 3.2 Change `commit` and `noteCommitted` to take listed records as evictions.
- [x] 3.3 Remove `scanStable` from `Append`, both on the ordinary eviction path and on the critical-overflow path.
- [x] 3.4 Remove `scanStable` from `recover`'s pending-record branch.

## 4. A record that cannot be classified

- [x] 4.1 Set aside a record whose priority cannot be read, and continue evicting from the rest.
- [x] 4.2 Test that an append to a full spool succeeds with a damaged record stored, and that the damaged record is still on disk.

## 5. Gates and evidence

- [x] 5.1 Mutate the classifying step and the eviction order; confirm the named tests fail.
- [x] 5.2 `make check` green.
- [ ] 5.3 Install the root daemon, confirm the cycle completes against the full `user` spool, and record the cycle duration.

## 6. Close

- [ ] 6.1 Sync the delta into the baseline, validate, archive.
