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

- [ ] 4.1 Install and read what the archive trims to: it holds 315 megabytes
      occupied across eight days and should settle at 256 and seven.
- [ ] 4.2 Record what it says, including the overflow records the eviction
      writes. About fifteen thousand of the oldest records are expected to go;
      record what actually went.

## 5. Close

- [ ] 5.1 Sync the delta into the baseline and archive.
