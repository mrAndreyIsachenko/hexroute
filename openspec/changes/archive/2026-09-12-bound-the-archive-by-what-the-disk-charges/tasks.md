# Tasks

## 1. Prove it before changing it

- [x] 1.1 A regression asserting a stored record counts what the filesystem
      charges, compared against `st_blocks` rather than against the record's
      length — so an implementation falling back to the length is caught rather
      than skipped over.
- [x] 1.2 A regression that both places the archive learns a size agree: what it
      records when it publishes, and what it reads from its directory.
- [x] 1.3 A regression that a record too large for the bound is refused on what
      it will occupy, not on what it contains.
- [x] 1.4 A regression that the age window is the one stated.
- [x] 1.5 Confirm by mutation: count stored records by their contents in each of
      the two places, count the incoming record by its contents, fall back to
      the length in the accounting, and restore the thirty-day window. Five
      mutations, all caught, judged by exit status.

## 2. Count what the disk charges

- [x] 2.1 `internal/diskusage` answers what a stored file takes and what a
      record will take, and keeps answering where the filesystem cannot be
      asked.
- [x] 2.2 The archive's index and its publish path both use it.
- [x] 2.3 The size the archive reports is the number it counts against the bound.
- [x] 2.4 The default age window is seven days.

## 3. Gates

- [x] 3.1 `make check`, judged by exit status.
- [x] 3.2 `make secret-test`.

## 4. Prove it on the machine

- [x] 4.1 Install and read what the archive trims to.

      | | before | after |
      |---|---|---|
      | occupied | 318 MB | **167 MB** |
      | contained | 97.6 MB | 51.3 MB |
      | files | 81,330 | 42,805 |
      | oldest record | 2026-09-03 | 2026-09-05 |
      | depth | 8 days | **7 days** |

      It trimmed by age, not by size: 167 megabytes occupied is under the
      256-megabyte bound, and the oldest record is exactly seven days back.

      The install itself was botched twice before this reading. Both times the
      binary in `bin/` was from the revision before the one being installed,
      because `make check` rebuilds it at whatever HEAD is at the time. Caught
      both times by checking the installed digest before measuring, which is
      the only reason the reading is of the right code. The install command now
      chains the build into it.
- [x] 4.2 Record what it says.

      **38,525 records went, against a prediction of about fifteen thousand.**
      The prediction assumed the byte bound would bite first and it was the age
      window that bit, harder. The number was wrong in the direction that
      matters — more evidence dropped than said — and it is recorded as wrong
      rather than restated.

      The overflow records took three attempts to look for, and the first two
      answers were the instrument rather than the archive. A shell glob over
      42,805 files, then a search filtered on `*.json` — and the archive names
      its records `*.event`, so both matched nothing and reported zero.

      **35 overflow records stand in the archive**, carrying schema
      `archive.overflow`. The eviction recorded itself, which is what the
      capability requires of it. What is recorded here is that answer, not the
      two zeroes that preceded it.

## 5. Close

- [x] 5.1 Sync the delta into the baseline and archive.
