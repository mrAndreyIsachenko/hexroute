# Tasks

## 1. The ground

- [x] 1.1 Read on the machine how the archive's records divide by class and schema, and how old its oldest operational record is.

      Read 2026-09-14 17:49Z: 65,536 records. Operational: 46,194 connectivity
      observations and 2,581 tunnel decisions, the oldest from 2026-09-11 12:14Z.
      Critical: 14,832 overflow records and 1,929 connectivity baselines. In the
      last day 8,024 critical records were written, 7,954 of them overflow, and
      16,272 operational.

## 2. The eviction

- [x] 2.1 Choosing what to evict for size counts what each record occupies.

      It counted contents against an excess counted in blocks. Found by
      measuring the test written for 2.2 before any fix: 73 overflow records for
      586 evicted, eight at a time, because eight half-kilobyte records are one
      block. That test passed against the unfixed code for the wrong reason, so
      it could not stand as the test for 2.2 until this was fixed. The index
      already holds what each stored record occupies, and choosing now uses it.
- [x] 2.2 An eviction for size frees a sixty-fourth of the bound beyond what the append needs, and an append goes ahead when what can be evicted covers the need but not the share.
- [x] 2.3 An eviction for age waits until the oldest record is a sixty-fourth of the window outside it, then evicts everything outside the window.

      Driven before the fix: 384 appends past a 256-second window wrote one
      overflow record per expiring append.

## 3. Gates and evidence

- [x] 3.1 Mutate the share, the counting, the covering and the age wait; confirm the named tests fail.

      Seven applied, seven killed: no share for size, freed counted by
      contents, covered only when the share was freed, covered whatever was
      freed, no wait for age, age evicting only past the wait, the wait
      required of every record rather than the oldest. The wait mutation did
      not compile as first written — it left the wait unused — and was redone
      before it counted.
- [ ] 3.2 `make check` green.
- [ ] 3.3 Install the root daemon from this branch with the reinstall shorter than three cycles, and read the archive a day later: overflow records written in that day, and the oldest operational record against its age at installation.

## 4. Close

- [ ] 4.1 Sync the delta into the baseline, validate, archive.
