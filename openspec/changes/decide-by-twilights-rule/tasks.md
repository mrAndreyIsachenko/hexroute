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
- [x] 2.2 The wake cause holds when the wall time between two cycles of the same process reaches the threshold, inclusive.

      First built as the interval plus the sleep the steady clock measured, so
      that a slow cycle would not look like a sleeping machine. A day into the
      soak that rule was found unable to pass it, by reading the archive rather
      than by any test: across idle sleeps of 136, 997 and 394 seconds on
      2026-09-14 — the archive silent for 162 and 1,586 seconds across them —
      every decision after recorded a sleep of zero, and over 2,365 decisions
      carrying grounds the largest was 115 milliseconds. The steady clock had
      been verified once, with `pmset sleepnow`, and did not stop for these
      idle sleeps. Twilight, holding the tunnel, recorded a wake after each of
      the three sleeps in the power log from 2026-09-11 to 2026-09-13; a
      clamshell entry into dark wake on 2026-09-12 has no wake record from it.

      Now the gap is the wall time since the previous cycle of the same
      process, as Twilight's is, and the measured sleep is a ground that decides
      nothing. 180 s is a gap and 179 s is not. A slow cycle names a wake, as a
      slow tick does in Twilight; the slowest measured was 93 s apart. The first
      cycle of a process names none, so a reinstall is not a wake. A threshold at
      or below the interval is still refused. The seven days start again.

      Seven mutations, seven killed: an exclusive threshold, the old interval
      plus sleep, the sleep added to the gap, the old value recorded, a negative
      gap accepted, the cycle's gap taken as its sleep, the gap not passed.
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
- [x] 3.4 A silence inside a collection is a hole, and a collection that did not measure its silences is not evidence.

      3.3 said a daemon down for more than three cycles is a hole, and it was
      true only where a collection started or where two met. Found by reading
      what `Continuous` compared, after the soak had started: a runtime stopped
      for hours between two collections left both windows looking whole. Each
      collection now records the longest gap between consecutive records it
      read, and the judgement refuses one longer than three cycles.

      The collections made before this were not measured, and absent is not
      read as zero: those windows are set aside. `collect --from` now wins over
      where the ledger reached, so collecting once again from the soak's start
      covers them with a window that was measured.

      Six mutations, six killed: a bound a hundred times looser, a bound
      exclusive at three cycles, the moments left unsorted, an unmeasured window
      read as silent for zero, a collection recording zero, `--from` ignored once
      the ledger holds a window. The fifth did not compile as first written and
      was redone before it counted.

      Installed 2026-09-14 18:55Z with the archive change stacked on it. The
      reinstall left a silence of 27 seconds. Collected again from 14:40:35Z:
      4,058 records, no rebuild decision, and the judgement read not passed for
      length and counts, not unjudgeable.
- [x] 3.5 A silence the runtime ended by deciding a wake is a sleep it observed, wherever it falls.

      3.4 made every silence longer than three cycles a hole, and the soak needs
      three sleeps: the first night would have made it unjudgeable. Found the
      next morning by reasoning about what 3.4 refused, before any sleep had
      happened in the soak. A collection now records where each long silence
      fell, including one before its first record, and a silence counts as
      observed when a wake gap was decided within three cycles of its end. A
      restarted runtime has no previous cycle and decides no wake, so its
      silence stays a hole. A long silence recorded without its place is refused.

      Eleven mutations, eleven killed: a wake window a hundred times wider, a
      wake before the silence ended counting, silences not checked, a lead never
      excused, a lead always excused, an unlocated silence accepted, silences
      not measured from the requested start, the bound inclusive, moments left
      unsorted, a collection keeping no silences, the judgement taking another
      cause as a wake. The last survived its first run — nothing drove the
      judgement through a sleep — and a test that does now kills it.

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

      First started 2026-09-14 14:40:35Z and voided a day in, when the wake rule
      was found unable to pass it (2.2). Its ledger is kept on the machine
      beside the new one, not deleted.

      Started again 2026-09-15 11:29:38Z, the daemon installed at 11:27:08Z with
      the configuration in place kept; the reinstall left a silence of 28
      seconds, before the start. First reading of the corrected rule: three
      decisions, no causes, three carrier entries, the probe and ingress
      addresses and the 180-second threshold the same as Twilight's, and a tick
      gap of 60,030 ms — the wall clock's, where the old rule would have
      recorded exactly the interval. First collection: 44 records, no rebuild
      decision. The earliest judgement is 2026-09-22 11:29:38Z.

      The first night passed without a sleep, and the reason is that the machine
      cannot have one: Twilight holds `caffeinate -i -s` for as long as its
      supervisor runs. Read 2026-09-16 12:25Z: that assertion had been held for
      two days and one hour, and the last sleep in the power log was twelve
      minutes before it started. The soak needs three natural wake gaps and
      would have waited for them for a week without one.

      Neither flag covers the lid, so the lid on battery is how the machine
      sleeps; on mains it reaches dark wake, which is no gap. The operator
      closes it for five minutes or more, three times over the soak. It is not
      recorded as an induction: nothing is told to either runtime, both see the
      same gap, and neither gets a hint the other lacks.

      The first, 2026-09-16: the lid closed on battery at 22:55:54Z, the machine
      woke at 23:06:44Z, and the archive was silent from 22:55:51Z to 23:06:51Z.
      This runtime decided `rebuild_tunnel` for `wake_gap` at 23:07:48Z with a
      tick gap of 711,378 ms; Twilight recorded `wake_gap_detected` at 23:07:53Z.
      The judgement counted one natural wake-gap agreement, no disagreement, and
      the silence as observed. The steady clock measured 648,176 ms of sleep
      this time, against 650 seconds in the power log: it stops for a lid sleep
      and did not for the idle sleeps of 2026-09-14, which is why the rule does
      not rest on it.

      Judged a minute after the wake, before either runtime's next cycle, the
      same silence read as a hole with no wake decided after it. Refusing was
      right — the decision had not been collected — but the message said no wake
      was decided, not that none had been collected yet.

      Carrier changes, 2026-09-17: three agreed, one natural and two induced,
      none disagreed. The natural one was at 06:06:41Z, when the route to one
      ingress target moved off the upstream carrier during an outage of every
      ingress. The runbook said to induce one by toggling the upstream VPN, and
      that was read as Pritunl for two attempts that changed nothing; Pritunl
      runs inside Twilight's tunnel, and Twilight's own code names the carrier
      as whatever interface carries the probe address — AdGuard VPN's. Toggling
      it breaks this repository's standing rule never to stop AdGuard, so the
      operator authorised it once. Noted at 11:11:16Z and switched off, the
      probe moved to `en0`; noted at 11:22:07Z and switched back on, it returned
      to `utun4`. Both inductions matched Twilight's rebuilds.

## 5. Close

- [ ] 5.1 Sync the delta into the baseline, validate, archive.
