# Tasks

## 1. The ground

- [x] 1.1 Read on the machine how the archive's records divide by class and schema, and how old its oldest operational record is.

      Read 2026-09-14 17:49Z: 65,536 records. Operational: 46,194 connectivity
      observations and 2,581 tunnel decisions, the oldest from 2026-09-11 12:14Z.
      Critical: 14,832 overflow records and 1,929 connectivity baselines. In the
      last day 8,024 critical records were written, 7,954 of them overflow, and
      16,272 operational.

      That reading did not divide overflow records by reason, and the proposal
      then said the age bound had not bitten — unread. Read again at
      installation, 2026-09-14 18:55Z, by reason: 9,296 age overflow records
      held naming 21,306 evicted, 5,780 size naming 56,025. In the last day,
      age 2,404 naming 2,620 (1.1 each), size 5,366 naming 21,464 (4.0 each).
      Age was the larger cause. Oldest operational record 3 days 5 hours old,
      oldest critical 6 days 23 hours.

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

- [x] 2.4 The size bound is large enough that the age window is what binds.

      Measured 2026-09-15 on the machine: 19,383 records a day — 17,919
      connectivity observations, of which 10,724 are the user daemon's two
      components at about one every 16 seconds, 1,440 tunnel decisions, and the
      rest critical. One record occupies one four-kilobyte block, so 256
      megabytes holds about 3.6 days against a window of seven. Writing less
      would not have reached seven either: at one observation a cycle for every
      component it is about 11,500 a day, or 5.7 days.

      The bound is a gigabyte. A week of that rate is about 530 megabytes, and
      the disk has 170 gigabytes free. Measured on a generated archive of that
      size, 136,000 records: open 0.5-2.2 s, append 10.9 ms, an age eviction
      batch of 3,806 records 407 ms, a twelve-hour read 8.3 s. An append that
      evicts for size took 6.1 s, because choosing by priority reads every
      record; at this bound the window binds first and that path does not run in
      a steady state. It is recorded as HEX-19 rather than fixed here.

      Two mutations, two killed: the bound at 512 megabytes, and at 556
      megabytes — a week of records to the byte, which is where the test sits.
      The documentation gate is a third: it checked the document against a
      literal in the source, so it would have stopped checking anything the day
      the literal changed. It reads the constant now, and both a stale document
      and a changed bound fail it.

## 3. Gates and evidence

- [x] 3.1 Mutate the share, the counting, the covering and the age wait; confirm the named tests fail.

      Seven applied, seven killed: no share for size, freed counted by
      contents, covered only when the share was freed, covered whatever was
      freed, no wait for age, age evicting only past the wait, the wait
      required of every record rather than the oldest. The wait mutation did
      not compile as first written — it left the wait unused — and was redone
      before it counted.
- [x] 3.2 `make check` green.

      On the branch stacked on `decide-by-twilights-rule`, so the daemon
      installed from it carries both the soaked rule and this.
- [x] 3.3 Install the root daemon from this branch with the reinstall shorter than three cycles, and read the archive a day later: overflow records written in that day, and the oldest operational record against its age at installation.

      Installed 2026-09-14 18:55:55Z, with the configuration in place kept.
      Before installing, the silence the previous reinstall left was read as 38
      seconds, and the installation would have stopped above 120. This one left
      27 seconds. The soak was collected again from its start with silences
      measured, 4,058 records and no rebuild decision, and judged not passed for
      length and counts only — not unjudgeable.

      Read 2026-09-15 21:39Z, 27 hours after installing. The first half holds:
      overflow records written in the last day fell from 7,770 to 19 — 17 for
      size naming 1,025.0 evicted each, as predicted, and 2 for age naming 660.5
      each. Critical records stopped growing: 15,718, against 17,005 at
      installation.

      The second half does not. The oldest operational record was 2 days 18
      hours old, younger than the 3 days 5 hours at installation, not older.
      The prediction was wrong rather than the fix: 49,557 operational records
      covered 66.6 hours, about 17,900 a day, and at one block each a bound of
      65,536 records holds about 3.6 days of them with no critical record at
      all — by arithmetic, not measured. The 3 days 5 hours at installation
      reached back into sparser records from 2026-09-11. Retention is now
      limited by the bound itself, and seven days cannot be reached at this
      rate. The change stays open to reach them — 3.4 is where they are
      reached, and this task's second half was a prediction that could not hold
      at this bound rather than a fix that did not work.

      The soak of `decide-by-twilights-rule` is running, so this reinstall has
      to leave a silence shorter than three cycles or it puts a hole in it. The
      previous two left 27 and 28 seconds.

      What the reading has to show: the archive holds more than 65,536 records,
      no overflow record for size has been written since the installation, and
      the oldest operational record is older than 3 days 5 hours and climbing
      toward seven days.

- [x] 3.4 Install the raised bound, and read two days later that the window is what removes records.

      Installed 2026-09-15 23:38:20Z, configuration in place kept, silence 37
      seconds. Five minutes later the archive held 64,909 records, up from
      64,835, with no overflow record for size since the installation — but
      64,909 is still under 65,536, so nothing yet proves the bound rather than
      a quiet five minutes. The crossing is what proves it.

      Read 2026-09-18 09:44Z, two days after: 101,047 records, oldest
      operational 5 days 4 hours — older than the 3 days 5 hours at the first
      installation, and climbing — with no `size` overflow record written since
      the bound was raised and eight `age` records naming 414 evicted. Read
      again at 13:51Z the archive spans 7 days and 47 minutes, from 2026-09-11
      13:05Z: the window it states is what removes records now.

      It crossed at 2026-09-16 01:50Z: 66,299 records, past the 65,536 the old
      bound held to twice over two days. No overflow record for size since the
      installation; the only eviction was one age batch naming 337 records,
      which fell due when the oldest critical record passed seven days and a
      sixty-fourth. The oldest operational record is climbing — 2 days 20 hours,
      from 2 days 18 hours at the installation. The two-day reading is owed.

## 4. Close

- [ ] 4.1 Sync the delta into the baseline, validate, archive.
