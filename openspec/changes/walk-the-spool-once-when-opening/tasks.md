# Tasks

## 1. Prove the second walk before removing it

- [x] 1.1 Write a test that counts the listings an open takes and fails while it takes two.

## 2. Keep what recovery learned

- [x] 2.1 Have recovery's bound check use the kept listing rather than a fresh one.
- [x] 2.2 Have opening read the kept listing for its highest sequence.

## 3. Gates and evidence

- [x] 3.1 Mutate both call sites; confirm the named test fails.

      Both fail it: opening listing the directory again, and recovery dropping
      what it learned.

      A third mutation was tried on the line beside them — removing the check
      that refuses a spool holding more than its bound admits — and every test
      still passed. That check has been there since the beginning and nothing
      held it. It is held now, and it was found by mutating the neighbourhood
      rather than only the edit.
- [x] 3.2 `make check` green.
- [ ] 3.3 Install, restart, and read the daemon's own account of the user journal's open.

## 4. Close

- [ ] 4.1 Sync the delta into the baseline, validate, archive.
