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
- [x] 2.5 The process is gone when the one the previous cycle saw is not the one running, replaced or not.

      Found by the soak's first induced process loss, 2026-09-17: stopped at
      11:38:09Z, Twilight noticed at 11:38:28Z and restarted it at 11:38:32Z,
      and this runtime — asking only whether a tunnel ran — saw one on both
      cycles around it and decided nothing. The PID the last cycle saw is kept
      in memory only, updated only by a cycle that saw a tunnel running, so a
      loss spanning a cycle with no tunnel is still seen and an installation is
      not a loss. The record carries `process_replaced`.

      Six mutations, six killed: a replacement not deciding, a replacement never
      seen, the first cycle counting one, a cycle with no tunnel forgetting the
      last, the replacement not passed to the rule, the record dropping it.

- [x] 2.6 The tunnel's observations are taken before anything that waits on a network, whatever the lid is doing, and no cause is decided from an observation nobody took.

      Measured on the night of 2026-09-18, a machine on battery with the lid
      closed: 21 dark wakes, each seconds long. Twilight rebuilt for six wakes,
      this runtime named eight, four were the same event. Its cycle reached the
      decision after probes that outlast a dark wake; one recorded gap covered
      112 minutes and four of Twilight's ticks. The cycle now observes the
      process and the carrier first and decides before the probes.

      The same night named a lost process eight times and lost none: the cycle
      returned at the lid gate before looking at the process, and the rule read
      "not running" from an observation nobody took. A cause needs its
      observation now, and the record says whether it was taken. A suspended
      cycle does not wait on the payload probe and carries its last answer.

      Seven mutations, seven killed: an unobserved process deciding, the ground
      dropped, the cycle never marking the observation, a suspended cycle
      running the probes, a suspended cycle probing the payload, a suspended
      cycle inventing an answer, the record dropping the field. The last
      survived its first run — nothing asserted the record carried it — and the
      record test now does.

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

- [x] 3.6 A process-gone episode beside the owner's own restart is explained, not a disagreement.

      Watching from outside, every restart Twilight makes for its own reasons
      replaces the process as a loss does. The judgement reads every transition
      to `STARTING` or `SINGBOX_EXITED` in Twilight's log, and a process-gone
      episode with no process rebuild but a restart inside the window is listed
      as explained. An agreement is taken before an explanation, and no other
      cause is explained.

      Five mutations, five killed: no explanation, an explanation for any cause,
      any restart explaining, `SINGBOX_EXITED` not read as a restart, and the
      judgement not passing the restarts on — which survived its first run
      because nothing drove the judgement through an explained loss, and a test
      that does now kills it.

      The first run of all eleven was void. The harness named its files in one
      shell variable, zsh did not split it, so no backup was taken and no
      mutation was undone: each applied on top of the last, several with
      stray backslashes that did not compile. The intended lines were restored
      one by one against the diff, the tests passed again, and the run was
      repeated by a harness that copies each file, compares it byte for byte
      after restoring, and stops if it differs.

- [x] 3.7 A silence inside the gap a wake was decided on is one the runtime accounted for.

      3.5 counted a silence as observed only if a wake was decided within three
      cycles of its end. On a night of dark wakes the cycle that finishes is
      several sleeps later: measured 2026-09-20, the machine was silent from
      00:46:03Z to 01:32:30Z — Twilight rebuilt at 00:45:54Z and reached healthy
      at 01:32:28Z — and this runtime's wake came 42 minutes after the silence
      ended, on a gap of 88 minutes that spanned it. The judgement called that a
      hole.

      A collected decision now carries the gap it was decided on, and a silence
      inside that gap counts as observed. The rule is untouched, so the seven
      days continue; only the soak command changes, and the ledger is collected
      again from the soak's start to carry the gaps.

      Five mutations, five killed: no covering rule, a covering tolerance a
      hundred times wider, a wake decided before the silence counting, the
      collection dropping the gap, the judgement dropping it. The fourth
      survived its first run — nothing asserted the collection kept it — and the
      collection test now does.

      Installed, and the judgement still refused: collections dedupe by
      sequence, so the decisions collected before the command carried gaps kept
      none, and collecting again added nothing for them. A collection now
      writes a decision again when it carries a gap the ledger lacks, and
      reading the ledger keeps the line that says more. Four more mutations,
      four killed: no enrichment, every repeat written, the read keeping the
      first, the read keeping the last — the last two needed a ledger with a
      gapless line written after a rich one, which is what an older command
      collecting after a newer one leaves.

- [x] 3.8 The decision is written before the rest of its cycle, and a silence a cycle ran inside is a lost record rather than an unobserved stretch.

      The night of 2026-09-20 again: a silence from 04:16:26Z to 04:32:53Z that
      no wake covered. The decision at 05:16:52Z named its previous cycle as
      04:26:33Z — inside that silence — and that cycle left no record at all.
      The daemon wrote the decision last, after the read model and the
      heartbeat, and the machine slept inside that tail: at 04:32:53Z two
      baselines were written and nothing else.

      The decision is written first now, so a cycle that reaches it leaves the
      record the comparison needs. Tested by a heartbeat that refuses: the loop
      ends there, and the decision is in the archive, which it was not before.

      The judgement reads the chain of cycles: a decision names the cycle before
      it by its gap, and a silence with a cycle inside it counts as observed. A
      decision that recorded no gap names no cycle. This is the one place where
      the soak accepts less than it would like: a decision whose record was lost
      cannot be compared against Twilight, and a disagreement inside such a
      stretch would go unseen. Recorded here because it is a weakening, not a
      fix.

      Five mutations, five killed: no cycle-inside rule, a gapless decision
      naming a cycle, a cycle after the silence counting, any decision
      accounting for a wake, and the record written after the heartbeat again.

- [x] 3.9 A stretch the machine dozed through is not judged, and what that gives up is written down.

      The first judgeable night, 2026-09-20, reported twenty disagreements:
      wake gaps decided here that Twilight did not make (8), wake gaps it made
      that were not decided (5), and process losses of the same shape (7). The
      machine dozed through it — twenty-one wakes of a few seconds — and the two
      runtimes woke in different ones: 02:06 against 02:14, 03:15 against 03:33.
      The comparison pairs events two minutes apart. A real sleep is the
      opposite: the lid closed on 2026-09-16 and the two named the wake five
      seconds apart.

      A dozing stretch is legible in the records already: a cycle that stops
      before the probes records an incomplete decision, and every decision that
      night is one. The collection records those stretches, and the judgement
      compares nothing inside them and treats no silence inside them as a hole.
      No daemon change, so the soak continues.

      What it gives up is in the spec: the rule is not proved for a dozing
      machine, and with authority it would have rebuilt the tunnel fourteen
      times that night against Twilight's six. That is owed by the executor in
      change three.

      One disagreement survived and showed the exclusion too narrow: the owning
      runtime named a wake at 10:21:47Z, awake, on a gap of 1,625 seconds that
      began at 09:54:42Z inside the doze, while this runtime's cycles had
      resumed and its own gap was a minute. An event whose gap began inside a
      dozing stretch is not judged either — the cycle a decision names for this
      runtime, the last line of its log for the owning one. Five more mutations,
      four killed: rebuilds judged by their moment alone, decisions judged by
      their moment alone, the previous activity taken from judged lines only,
      the decision carrying no previous. The fifth, dropping the guard against a
      moment nobody recorded, is equivalent: a zero moment falls in no stretch
      that starts in 2026.

      Eight mutations, eight killed: decisions judged while dozing, the owner's
      rebuilds judged while dozing, a stretch that swallows everything after it,
      silences inside a stretch still holes, a silence reaching past one
      excused, a stretch ending at the next incomplete decision, every decision
      opening a stretch, and the collection keeping no stretches.

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

      The induced process loss disagreed — see 2.5 — and the rule was changed,
      so this soak is void from 2026-09-17 11:38Z and starts again when the
      change is installed. Its ledger is kept beside the next one.

      Started again 2026-09-17 19:46:24Z, the daemon installed at 19:43:54Z with
      the configuration in place kept and the voided ledger moved to
      `soak.pid-rule-void-2026-09-17`; the reinstall left a silence of 28
      seconds. First reading: three decisions, no causes, no replacement named —
      the first cycle of a process compares none — tick gap 60,023 ms, three
      carrier entries, and the probe, ingress addresses and threshold the same
      as Twilight's. First collection: 44 records, no rebuild decision. The
      earliest judgement is 2026-09-24 19:46:24Z.

      The induced process loss agreed at the first attempt under the corrected
      rule: noted at 20:06:26Z, the process stopped, and the judgement reads one
      induced agreement with no disagreement — the same induction that
      disagreed under the old rule four hours earlier.

      That soak is void from 2026-09-18 09:44Z, when the night's dark wakes
      showed the rule deciding too late and naming losses nobody looked for —
      see 2.6. Started again 2026-09-18 13:47:52Z, the daemon installed at
      13:45:22Z with the configuration kept and the voided ledger moved to
      `soak.late-rule-void-2026-09-18`; the reinstall left a silence of 54
      seconds. First reading: three decisions, no causes, `process_observed`
      true, tick gap 59,912 ms. The earliest judgement is 2026-09-25
      13:47:52Z.

## 5. Close

- [ ] 5.1 Sync the delta into the baseline, validate, archive.
