# Tasks

## 1. The ground

- [x] 1.1 Read the live machine before proposing anything that touches it: what
      holds the tunnel, which generation is active, what the root runtime's
      state and pending count are, and whether the soak window recorded as
      ending about 2026-10-05 has in fact ended. Report what was seen, not what
      was expected.

      Measured 2026-10-10. **Twilight holds the tunnel**: `sing-box` runs
      under `sudo -n` from `twilight/supervisor/client/`, started 2026-10-09
      12:44 local, beside the supervisor running since 2026-09-26 and its
      `caffeinate -i -s -w`. The soak window has ended and ownership went back.

      `hexrouted` answers its socket as the operator: `HEALTHY`,
      `observe-only`, generation 2428 then 2429 a moment later so the cycle is
      keeping its period, `pending_operations: 2`, `safe_mode: false`,
      `consecutive_failures: 0`, `last_reason: probe_succeeded`. The recovery
      quantities are absent, which is what `KeepsNoRecovery` means. Policy:
      active, `bundle_generation: 5`, `policy_generation: 4`, activated
      2026-09-26T00:15:59Z, **expires 2026-10-25T22:45:57Z**, no authorization
      suspension.

      `tunnel-state.json` carries `known: true`, `link_present: true`,
      `payload_failures: 0`, `link_failures: 0` and a carrier signature of three
      paths. It has **no owner field at all**, so it does not answer who holds
      the tunnel. The process listing does. A plan that reads this file for
      ownership would read nothing and conclude whatever it already believed.

      `node-id` is on disk, so this host already has an identity; what it lacks
      is a key and a registration.
- [x] 1.2 Measure the spool as it stands: its size against its bound, how many
      records it holds, and whether it is overflowing now. The correlation
      decision rests on this being the steady state; if it is not, say so.

      Overflow is the steady state, and the arithmetic says so rather than a
      log. The newest record is sequence 233,347 and the spool holds 83,036
      files, so 150,311 records are already gone: it sits at its bound and
      refuses non-critical records, which is what records an overflow incident.
      The correlation decision stands on measurement.

      And a second thing fell out, which no requirement covers. The root spool
      occupies 332,148 KiB for 83,036 records, and 83,036 × 4 KiB = 332,144 KiB
      — the bound is computed over record content, while the disk pays a block
      for each record of a few hundred bytes. The user spool is the same: 83,573
      records, 334,292 KiB. **Two spools bounded at 100 MiB each occupy 650
      MiB.** Recorded in the roadmap's Owed rather than fixed here.

      Measured beside it: `readmodel` holds 5,807 checkpoints in 468,116 KiB,
      against 5,650 in 336 MiB when it was last read — the unbounded store is
      growing. The event archive is 545,580 KiB. Hexroute's state on this
      machine is about 1.6 GiB.

      How many overflow incidents the spool currently holds is **not measured**:
      the command written for it used `grep -rlc`, which with both flags counted
      files rather than matches and returned exactly the file count for two
      different patterns. A plausible number meaning something else. Re-run
      with `grep -rl` before any claim about how many would upload.
- [x] 1.3 Confirm that a record reaches the spool on every cycle, so a node
      registered with a heartbeat expectation is heard from while the machine is
      awake and doing nothing in particular. If the spool can be idle on a
      running machine, the expectation is wrong and this says why.

      The newest spool record was written 3 minutes before it was read, on a
      machine doing nothing in particular, and the cycle's period is 60
      seconds. The spool is not idle on a running machine, so a 24-hour
      expectation is met while awake with three orders of magnitude to spare.

## 2. The incident reaches the upload queue

- [x] 2.1 `connectivityjournal.AppendIncident(event.Incident)`: one closed type,
      its own validation, mirrored to the archive like any written record.

      The urgency hook went in beside `Mirror` rather than at the call site,
      for the reason `Mirror` is there: "an incident makes the upload due" is a
      property of writing one, not something every caller must remember. A
      caller that forgets costs an alert its latency and says nothing about
      having forgotten. A failing marker is counted, never fatal — the record is
      written and only its promptness is lost, which the records themselves do
      not show.
- [x] 2.2 Prove the journal's readers skip it — `Records`, `RecordsAfter`,
      `Newest`, `LatestBaselines` and checkpoint verification — rather than
      trusting that they do because the spool's own incidents are already there.

      Each reader is compared before and against after, by digest and by
      sequence, not merely by count. `Newest` is the one that would have been
      wrong if the skip were missing, because an incident is written after the
      newest fact. Checkpoint verification reads through these same readers, so
      it is covered by them rather than by a second fixture of its own — stated
      here so the reasoning is visible instead of implied.
- [x] 2.3 Refuse a payload that is not an incident, and refuse an incident whose
      fields the schema does not accept.

      Six refusals: empty, no identity, and one each for a status, severity,
      category and component outside their closed lists. After all six the
      journal holds nothing, the mirror took nothing and the upload was never
      marked due — a refused incident must not leave an upload waiting for a
      record that is not there.

      And the other direction: a fact does **not** mark the upload due. Every
      cycle writes one, so a fact that marked it would start the agent every
      minute and the marker would stop meaning anything.
- [ ] 2.4 Touch the marker from this one place and nowhere else; verified by a
      gate, not by reading.

      Waits for section 4: the gate holds the marker's implementation, and
      there is nothing to hold until the agent that reads it exists. The
      structure is already in place — `Urgent` is only reachable through
      `AppendIncident` — and the gate is what keeps it that way.

## 3. Handing the tunnel back opens a condition

- [x] 3.1 Record an availability incident at the handback site, beside the
      existing `tunnel.handback` record, carrying the reason from its closed
      list.

      The reason reaches the alert through the identity, because
      `event.Incident` has no field for a reason and `event.Decode` is strict —
      a new field would make an older build refuse the record and, on the host,
      quarantine its own incidents after a rollback. An event identity admits
      `.`, `:` and `-` and **not** the underscore these reasons are written
      with, so `tunnelexec.Handback.Reference()` spells the three of them once,
      enumerated, and a reason outside the three has no spelling at all. A
      replacement of underscores would have answered for any string it was
      handed.

      The reason is carried because the three mean three different things to
      do: a lapsed grant is an expiry to renew, a reached bound is a loop, and
      a tunnel carrying nothing is the network. An alert without it sends the
      operator to the machine, which is what this change exists to avoid.

      While doing it the handback vocabulary became single-sourced:
      `Handbacks()` is the list, `Valid()` reads it, and the notice's own
      validation stopped keeping a second switch of the same three.
- [x] 3.2 Record the end of it where the operator's resume takes the tunnel
      again.

      **Written against something that is not true, and corrected by reading.**
      `resume` clears the rebuild bound and nothing else — `rate.go:86` says so
      — and `Handback()` returns nothing when `!Owns`, so a runtime holding
      nothing gives nothing back. The tunnel is taken by the operator's
      ceremony, not by `resume`.

      So the end is read from ownership: on every cycle that owns the tunnel,
      the word left for the operator says whether there is an ending to
      record. That word already is the state — "a handback that has not been
      superseded is what the machine is now" — so it gained a field rather than
      a third file gaining the same subject. It is marked, not removed, because
      the one case where removal bites is the case the notice exists for:
      nobody was at the machine to read it. Its decoder has never been strict,
      so a build that does not know the field announces the handback exactly as
      before, which rollback needs.
- [x] 3.3 Prove a handback with no reporting available still hands back, leaves
      its word and observes; the incident waits in the queue.

      True by construction rather than by a fixture: the incident is written to
      the local journal, and the network is the agent's business on its own
      schedule. Nothing in the handback path waits on a registry.

      What the writing of it must not do is end the runtime, and the first
      version did. A reason with no spelling returned an error and the cycle
      returned `invalid_runtime` — which is the failure of 2026-10-05 in a new
      place: *"A runtime SHALL NOT end because it cannot name something it was
      going to write down"*, measured as twenty minutes with no tunnel while the
      claim was still held. It now keeps `incident_unnameable` as the cycle's
      cause and finishes. The cause is documented beside the other ten in
      `docs/macos/root-observe.md`, which no gate required: the documentation
      and producer gates hold `internal/connectivity` and
      `internal/connectivityreduce`, and `internal/control` is one of the
      vocabularies the last change recorded as unheld. I added a value to an
      unheld vocabulary and the repository said nothing.
- [ ] 3.4 Prove the word in the file is unchanged — the operator's session still
      announces it once — because the reason that file exists has not changed.

## 4. The agent

- [x] 4.0 The drain path could not be used as it stood, and this is what it
      cost to find out.

      `Uploader.RunOnce` asked the spool for **every** record and kept the
      first 256. On this machine the root spool holds 83,036 — because nothing
      has ever drained it — so one pass read 83,036 records to send 256, and
      draining the store would have cost that scan once per batch: 325 passes,
      roughly 13 million file reads, the same work squared.

      `bounded-spool-operation-cost` already forbids this: *"A spool operation
      SHALL read and decode only the records it hands out."* The same
      requirement records that this machine has been bitten at this exact scale
      before — "the daemon decoded eighty-four thousand records on an append,
      never returned, and recorded nothing about it" — and the append path was
      fixed. The drain path was never exercised, so it kept the defect.

      Fixed by reading the listing (which opens no record) and then the records
      being sent, one open each. Held by a cost test that counts opens rather
      than seconds, for the reason the eviction cost test gives: a stopwatch
      measures the machine and a tuned threshold measures the threshold.

      **The cost test then found a second scan**, which is why it exists:
      `spool.Acknowledge` took event identities and decoded records until it
      found each one, so acknowledging a batch cost reading the whole store
      again. 1,280 opens to send 256 of 1,024 — 1,024 + 256, which named itself.
      It now takes sequences, and the translation lives in
      `ApplyAcknowledgement`, the one place that already holds both numbers. A
      record is not re-read to confirm it: a sequence is never reused and a
      stored record is immutable, so the file at a sequence is that record or
      gone.

      A pass now opens exactly what it sends, and draining a store costs the
      store once.

- [ ] 4.1 A root binary that carries the upload: it asks the runtime for a
      batch, signs and sends it through `cloudingest.NewHTTPTransport`, hands
      the acknowledgement back, and reports which trigger woke it. It does not
      open the store.

      Decided 2026-10-10 after reading what opening a spool does. `recover()`
      completes or discards every unfinished write, which is correct for the
      only writer and destructive for a second one, and the runtime appends
      every sixty seconds. A cross-process lock was rejected because it would
      sit on the append path whose cost requirement was found the hard way.

- [ ] 4.1a The runtime serves a batch and takes an acknowledgement over its
      socket: two bounded actions, the reply doing nothing locally but removing
      what was accepted.
- [ ] 4.1b Prove the socket's new actions cannot be used to make the runtime do
      anything else, and that an acknowledgement naming records the runtime did
      not hand out is refused.
- [ ] 4.2 Its launchd plist and wrapper: an interval for the stream, `WatchPaths`
      on the marker for the incident. Disjoint from Twilight, held by the
      existing launchd gate.
- [ ] 4.3 The marker is removed only after the acknowledgement; prove a job
      killed mid-upload uploads the same records again and that the interval
      starts a run with nothing new recorded.
- [ ] 4.4 Prove the observing cycle keeps its period while the registry does not
      answer; verified by measuring the cycle, not by arguing that it must.
- [ ] 4.5 Register the agent so the package census and the reachability census
      both see it.

## 5. The key and the node

- [ ] 5.1 The root installer generates the key when absent, takes the node
      identity as input, prints the public half once.
- [ ] 5.2 Prove an existing key is kept — the installer has overwritten live
      material before, and this is the file that cannot be regenerated.
- [ ] 5.3 Write the registration procedure: which rows the public half becomes,
      with the expectation set to 24 hours and the reason it is 24 and not 60
      seconds. No identity, key or endpoint in this repository.

## 6. The cloud turns a reported incident into a condition

- [ ] 6.1 `cloudincident.SignalFromHostIncident`: correlated by node, category
      and component; the host's own identity kept as evidence.
- [ ] 6.2 Prove the same condition reported again updates one incident and plans
      no second delivery.
- [ ] 6.3 Prove a reported resolution clears it.
- [ ] 6.4 Prove a thousand occurrences of one condition are one incident, using
      the shape the spool actually produces — an identity per sequence.
- [ ] 6.5 `make postgres-test` covers 6.1-6.4 against a real database.

## 7. The two things nothing was holding

- [ ] 7.1 Stop planning a channel nothing can claim, with the reason recorded
      where the planning is.
- [ ] 7.2 A gate refusing a planned channel the store cannot claim, so the plan
      and the claim cannot drift apart again.
- [ ] 7.3 A reachability gate: the package handling acknowledgements cannot
      reach a package holding a lease, a plan, a command or an executor. State
      what class it closes and what it does not.
- [ ] 7.4 Prove the only local change an acknowledgement causes is the removal of
      acknowledged records from the spool.

## 8. Gates

- [ ] 8.1 `make check`; verified by its exit status, run after the commit as
      well as before it.
- [ ] 8.2 `make postgres-test`; verified by its exit status.
- [ ] 8.3 Mutation run over the new readers and the new gates, each survivor
      closed or recorded with its reason.
- [ ] 8.4 Report any gate that did not run and why.

## 9. Evidence

- [ ] 9.1 The chain, end to end, with a spool incident: a real host-originated
      incident reaches Telegram. Record what was sent, what arrived and how long
      it took.
- [ ] 9.2 The named case, once: a real handback opens the incident, the alert
      arrives, and `resume` clears it. Record the three in order with their
      times.

      **This task cannot be run as written, and task 1.1 is why.** A handback
      is something only the owner can do, and Twilight owns the tunnel. None of
      the three causes — the rate bound, the grant lapsing, the tunnel carrying
      nothing — can occur for a runtime that holds nothing. The grant expiring
      on 2026-10-25 does not produce one either, for the same reason.

      So the named case costs a third handover, which is its own act: the second
      one found twelve defects, nine of them by running it, and three were worth
      carrying forward.

      Decided 2026-10-10: **this task waits for the next handover** and is owed
      rather than dropped. Alert delivery and tunnel ownership are different
      risks, and performing them in one act means neither can fail by itself.
      Task 9.1 proves the whole chain on a real producer — signature,
      transport, cursor, signal, correlation, policy, Telegram — so what waits
      is only that the recording sits where the handback is, which tests hold
      and a handover confirms.
- [ ] 9.3 Prove the night window is not involved either way, because both are
      actionable; say what that means for how an alert at 03:00 will read.
- [ ] 9.4 Measure HEX-19's eviction cost again once records leave on
      acknowledgement, and record the number rather than the expectation.

## 10. Close

- [ ] 10.1 Sync the delta into the baseline, validate and archive, with every
      task that can be ticked beforehand ticked; verified by the drift gate.
- [ ] 10.2 Record what this leaves open: the spool's condition that nothing
      closes, the second and third changes and what each needs, whether the
      single alert contact is still the right one now that a host can reach it,
      and task 9.2 owed against the next handover.
