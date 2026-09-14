# Tasks

## 1. The ground

- [x] 1.1 Read on the machine: with no claim, the daemon sees the previous owner's tunnel running.

      Read 2026-09-14 14:19Z: no claim on disk, and all 48 decisions since the
      release carry `process_running: true` — the daemon from change 1 finds the
      tunnel by the previous owner's configuration.
- [x] 1.2 Read on the machine: the configured upstream probe and ingress addresses are the ones Twilight's carrier watchdog logs, and the installed wake threshold beside Twilight's.

      The probe and both ingress addresses are the ones Twilight's watchdog logs.
      The wake threshold was not: 90 seconds installed against Twilight's 180,
      with a 60-second interval. Under the old meaning that was pure sleep; under
      this change it would name a wake gap after thirty seconds of sleep. The
      daemon would have started without complaint and the soak would have meant
      nothing, which is why this was read before installing rather than after.
      The installed value is changed to 180 at install.

      The archive at the same reading: 67.7 MiB of its 256 MiB bound, covering
      2026-09-07 19:32Z to 2026-09-14 14:19Z — six days and nineteen hours, not
      seven — in exactly 65,536 files, the second reading in a row to land on
      that number.

## 2. The rule

- [x] 2.1 Only the process, the wake gap and the carrier lead to `rebuild_tunnel`; nothing produces `reapply_routes`.

      A returned link, a failed payload path and drifted routes are still
      observed and recorded as grounds, and decide nothing. Their names stay in
      the record's vocabulary, because the archive holds records that use them.
      The payload count is now consecutive and stands until the path answers;
      it used to be spent on the decision it caused, and there is no decision.
- [x] 2.2 The wake cause holds when the interval plus the measured sleep reaches the threshold, inclusive.

      A sleep of 120 s at a 60 s interval is a gap and 119 s is not. The old
      test of a two-minute sleep had to become three minutes: two minutes of
      wall time with a second of running is 179 s of tick gap, which the runtime
      this reproduces does not rebuild on either. A threshold at or below the
      interval is refused, by the rule and by the configuration.
- [x] 2.3 The carrier signature is the upstream probe and the ingress targets.

      Built while observing, keyed by the address asked about. An ingress route
      that could not be read is still an entry with no interface, as that
      runtime writes `unknown` for one. The old helper keyed every route and is
      gone; its test was moved onto the cycle rather than left testing dead code.
- [x] 2.4 The record carries the tick gap and remains readable for records written before it.

## 3. The comparison

- [x] 3.1 A package that pairs this runtime's rebuild decisions with Twilight's rebuilds by cause inside a window, and reports agreements and disagreements in both directions.

      `internal/soakcompare`. Decisions for a cause close enough together are
      one episode, because this runtime decides again on every cycle until a
      condition passes and the owner rebuilds once.
- [x] 3.2 Induced events recorded by the operator and counted apart from natural ones.
- [x] 3.3 A command that reads both stores and the inductions and prints the judgement against the pass criterion.

      `hexroute-soak-compare collect|note|judge`. The judgement could not read
      the archive at the end, and this was found in the code before any of it
      was written: the archive answers for seven days at most and evicts by age
      regardless of priority, so the first hours of a seven-day soak are gone
      when it ends. `collect` copies rebuild decisions into a ledger as it goes
      and records the window each collection covered; `judge` refuses a soak
      with a stretch nobody collected, a collection whose archive had already
      lost more than three cycles of its start, or one that stops short of the
      end. A daemon down for more than three cycles is a hole by the same rule,
      which is right: nothing was observed.

## 4. Gates and evidence

- [x] 4.1 Mutate each cause's definition, the window and the induced count; confirm the named tests fail.

      Thirty applied, thirty killed.

      The rule, seven: an exclusive wake boundary, the sleep compared alone, a
      returned link acting again, drifted routes acting again, a threshold
      within one interval accepted, the payload count spent, the tick gap
      recorded without the interval. The last did not apply as first written —
      its anchor was typed from memory with the wrong alignment — and was redone
      against the text.

      The daemon, five: every target in the carrier, the probe left out of it,
      a threshold within the interval accepted by the configuration, the
      interval not taken from the configuration, the tick gap not recorded. The
      last two survived their first run because nothing tested them; a
      configuration test and a record test were written and both mutations die.

      The soak, sixteen: a shrunk window, decisions not grouped, induced counted
      as natural, an induction of any cause counting, wake gaps counted induced
      or not, disagreements not failing the soak, the length not required, a
      hole between collections accepted, a lost start accepted, a collection
      short of the end accepted, overlapping collections repeating decisions, an
      outer-path restore counted as a rebuild, any decision collected, a
      judgement with a hole going ahead, a negative tick gap accepted, causes
      interchangeable. Two survived their first run — the causes test checked a
      total that came out the same with the cause ignored, and the lead test used
      a lead six hours long that almost any bound refuses. Both now test at the
      boundary, and both mutations die.

      Two more, for a defect found while writing the install sequence rather
      than by any test: a collection run straight after the soak's start, or
      moments after another, reads an empty window, and recording it made every
      later judgement refuse the soak for reading nothing where there was
      nothing yet to read. An empty window within three cycles is no longer
      recorded; a longer one is, because that silence is a hole. Removing the
      guard fails one test, and a guard that skipped every empty window fails
      the other.
- [x] 4.2 `make check` green.
- [ ] 4.3 Install, and run the soak until it passes or a disagreement stops it.

## 5. Close

- [ ] 5.1 Sync the delta into the baseline, validate, archive.
