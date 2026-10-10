# Tasks

## 1. The ground

- [ ] 1.1 Read the live machine before proposing anything that touches it: what
      holds the tunnel, which generation is active, what the root runtime's
      state and pending count are, and whether the soak window recorded as
      ending about 2026-10-05 has in fact ended. Report what was seen, not what
      was expected.
- [ ] 1.2 Measure the spool as it stands: its size against its bound, how many
      records it holds, and whether it is overflowing now. The correlation
      decision rests on this being the steady state; if it is not, say so.
- [ ] 1.3 Confirm that a record reaches the spool on every cycle, so a node
      registered with a heartbeat expectation is heard from while the machine is
      awake and doing nothing in particular. If the spool can be idle on a
      running machine, the expectation is wrong and this says why.

## 2. The incident reaches the upload queue

- [ ] 2.1 `connectivityjournal.AppendIncident(event.Incident)`: one closed type,
      its own validation, mirrored to the archive like any written record.
- [ ] 2.2 Prove the journal's readers skip it — `Records`, `RecordsAfter`,
      `Newest`, `LatestBaselines` and checkpoint verification — rather than
      trusting that they do because the spool's own incidents are already there.
- [ ] 2.3 Refuse a payload that is not an incident, and refuse an incident whose
      fields the schema does not accept.
- [ ] 2.4 Touch the marker from this one place and nowhere else; verified by a
      gate, not by reading.

## 3. Handing the tunnel back opens a condition

- [ ] 3.1 Record an availability incident at the handback site, beside the
      existing `tunnel.handback` record, carrying the reason from its closed
      list.
- [ ] 3.2 Record the end of it where the operator's resume takes the tunnel
      again.
- [ ] 3.3 Prove a handback with no reporting available still hands back, leaves
      its word and observes; the incident waits in the queue.
- [ ] 3.4 Prove the word in the file is unchanged — the operator's session still
      announces it once — because the reason that file exists has not changed.

## 4. The agent

- [ ] 4.1 A root binary that drains the spool through `telemetry.Uploader` and
      `cloudingest.NewHTTPTransport`, reporting which trigger woke it.
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
- [ ] 9.3 Prove the night window is not involved either way, because both are
      actionable; say what that means for how an alert at 03:00 will read.
- [ ] 9.4 Measure HEX-19's eviction cost again once records leave on
      acknowledgement, and record the number rather than the expectation.

## 10. Close

- [ ] 10.1 Sync the delta into the baseline, validate and archive, with every
      task that can be ticked beforehand ticked; verified by the drift gate.
- [ ] 10.2 Record what this leaves open: the spool's condition that nothing
      closes, the second and third changes and what each needs, and whether the
      single alert contact is still the right one now that a host can reach it.
