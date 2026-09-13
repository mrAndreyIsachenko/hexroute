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
- [x] 3.3 Install, restart, and read the daemon's own account of the user journal's open.

      The user journal went 10.230 seconds to 2.679, and the stores together
      20.119 to 7.012. Two neighbours fell with it: the event archive 3.873 to
      2.320 and the replay 3.016 to 0.108, because the listing kept at open is
      the one they both go on to ask for.

      The account closes: parts 7.000, stores 7.012, process end to end 7.025.

      The reader that took this measurement was wrong first, and is recorded
      rather than quietly fixed. It found the last `daemon_stopped` before the
      last start and reported a window of forty minutes — because a daemon that
      is killed rather than asked to stop writes no record at all, and the stop
      it found was an hour old. It now walks back from the start and takes only
      what belongs to it, and says plainly when there is no stop to pair with.

## 4. Close

- [x] 4.1 Sync the delta into the baseline, validate, archive.
