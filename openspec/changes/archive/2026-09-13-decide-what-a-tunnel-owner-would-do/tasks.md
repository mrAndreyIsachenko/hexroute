# Tasks

## 1. The decision, before anything reads it

- [x] 1.1 `internal/tunnelplan` takes observations and the previous cycle's
      state and returns a decision and the next state. Pure: no clock, no
      filesystem, no processes.
- [x] 1.2 A regression per cause, each failing against an empty planner.
- [x] 1.3 A regression for several causes at once: one decision naming all of
      them, because the runtime it will be compared against reports one reason
      and an honest disagreement must not look like a wrong decision.
- [x] 1.4 A regression that no cause is a decision too, and is recorded.
- [x] 1.5 Confirm by mutation — remove each cause in turn and confirm the named
      test fails. Judge by exit status.

      Eight mutations, all caught: each of the six causes removed, a first cycle
      made to invent a carrier change, and the payload threshold lowered to one.

## 2. The probe that proves traversal

- [x] 2.1 A payload probe that fails when the payload does not traverse, and is
      not satisfied by a completed connection.
- [x] 2.2 A regression that a socket which answers without carrying the payload
      is a failure. This is the assertion the whole sixth cause rests on.

      A listener that accepts and closes is the shape of a path that is up and
      carries nothing. Anything that only dials calls it a success; this calls
      it a failure.

      A mutation here survived and was worth reading rather than patching: the
      traversal test was `response.StatusCode > 0`, which is true whenever a
      response exists, so replacing it with `true` changed nothing observable.
      That is a redundant condition in the code rather than a gap in the tests,
      and the condition was removed.
- [x] 2.3 It is configured like the endpoint probes and taken with them, so a
      cycle still waits once.

## 3. Memory between cycles

- [x] 3.1 The carrier signature, whether connectivity was present, and the count
      of consecutive payload failures are carried across cycles in their own
      durable file.
- [x] 3.2 It survives a restart, and a cycle that cannot read it decides from
      what it can see rather than refusing to decide.
- [x] 3.3 A regression that a restart does not manufacture a carrier change out
      of an absent previous signature.

## 4. Recording, so the comparison is possible

- [x] 4.1 Every cycle records its decision, including the cycles that decided
      nothing. A decision that agreed and a decision never reached look the same
      in an empty log.
- [x] 4.2 The record names the causes and is durable.

      It goes into the event archive rather than a file of its own. The archive
      already has bounds, priorities and a review tool, and a record of this
      kind with none of those is how the stores reached 778 megabytes. The way
      in is narrow — one method that takes a decision and nothing else — so it
      does not become a back door around the journals that own everything else
      in there.

      Two mutations here were worth more than the tests they broke. One survived
      because the test passed a nil recorder, so the branch that tells "decided
      nothing" from "was never asked" was never exercised; that distinction now
      has its own predicate and its own assertion. The other would not compile,
      which is not a mutation, and was reformulated until it did.

## 5. Gates

- [x] 5.1 `make check`, judged by exit status.
- [x] 5.2 `make secret-test`.

## 6. The soak

- [x] 6.1 Install and run beside Twilight. Nothing is performed.

      The first install recorded nothing at all, and the reason was a defect in
      this change rather than in the installation. The decision was reached at
      the end of the cycle, and eight observations can end a cycle before it
      gets there — so no decision happened on any cycle that stopped early.

      That is the wrong way round. The tunnel being in trouble is exactly when
      those observations fail, so the decision was absent at the moments it
      exists for, and `process_gone` could never have been reached at all.

      The cycle now decides on every pass. What an incomplete cycle did not see
      it does not decide from: the carrier and the link are compared only when
      the cycle saw them, and what it could not see is carried forward rather
      than overwritten — an empty signature is not a changed one, and treating
      it as one would rebuild the tunnel every time an observation failed.

      Found by the soak's first check, before any cause had been induced, which
      is the argument for checking that the machinery records anything before
      trying to make it record something particular.

      The second check found a second defect, and this one was only visible on
      the machine. The carrier signature was keyed by the route that answered
      rather than by the address asked about. A route observation carries both,
      and the route's own destination is the prefix that matched — so the live
      signature read:

          128.0.0.0=utun4   seven times
          invalid IP=utun4  four times

      Seven identical keys and four that are not addresses. A signature like
      that can change without the carrier changing and stay still when it does.

      It is keyed by the address asked about now, and addresses that are not
      addresses are left out. The test fixture had hidden it: it set a route's
      own destination equal to the address asked about, which the machine never
      does, so a mutation keying by the wrong one changed nothing. The mapping
      is its own function now and is tested against the shape the machine
      actually produced.

      It has run beside Twilight since, through a reinstall, a killed sing-box
      and a tunnel rebuilt by its owner, and has performed nothing. The three
      defects the soak found are in what it decided, never in what it did, which
      is the whole argument for deciding before being allowed to act.
- [x] 6.2 Replay the rule against the supervisor's recorded restarts and record
      where it disagrees.

      Thirty-three restarts over sixty-one days, and every one of them has a
      cause this planner knows: nineteen carrier changes, twelve wake gaps, two
      payload failures. Nothing unmapped.

      The number is thirty-three rather than the sixty-four stated earlier. That
      count included thirty-one `restart: waiting` lines, which are a step
      inside a restart rather than a restart, and the correction is recorded
      rather than quietly adopted.

      What this shows is that the vocabulary is complete for what actually
      happened. What it does not show is that this runtime would have acted at
      the same moments, which only the live comparison can say.
- [x] 6.3 Each of the six causes agrees at least once: the carrier and the wake
      by waiting, the process, the returned link and the payload path by
      inducing them.

      Five were reached by waiting, over the archive this runtime has written:
      routes drifted on every cycle, twelve wake gaps, six carrier changes, five
      returned links and one payload failure. The sixth had to be induced, which
      is what the replay said as well: sing-box has not exited by itself in
      sixty-one days.

      Inducing it needed the timing to be worked out rather than guessed. The
      process is sampled at the start of a cycle and the heartbeat is written at
      the end, so the next sample falls about sixty seconds after the last
      heartbeat, and the kill was timed at that. It landed first time.

      2026-09-12, kill at 22:04:13Z:

          22:04:16Z  hexroute  rebuild_tunnel  process_gone
          22:05:02Z  twilight  HEALTHY -> SINGBOX_EXITED  process_missing
          22:05:10Z  twilight  SINGBOX_EXITED -> STARTING  singbox_started

      They agree on the cause and disagree on the latency: three seconds against
      forty-nine. Restoring it took fifty-seven, which is four times the fifteen
      seconds predicted before the kill from the supervisor's health interval —
      the prediction read the interval and not the work.

      The link threshold held through it. While the process was gone the cycle
      stopped early and reported no configured endpoints, and no return was
      manufactured out of that.
- [x] 6.4 Record every disagreement, including the ones where Twilight acted and
      said nothing.

      The first disagreement is large and it is hexroute's. In the hour after
      the daemon came up on 2026-09-12 it reached 55 decisions and seven of them
      said rebuild the tunnel. Twilight, in the same hour, made no state
      transition at all. Six of the seven named a returned link.

      The record could not say why, because a decision record carries the action
      and the causes and not what they were decided from. The connectivity
      archive could: every cycle writes a relay_ingress observation carrying how
      many outer endpoints were configured and how many answered. One is
      configured, and it failed on 25 of 3,117 cycles over seven days — never
      twice in a row. Each of the six returns is the cycle immediately after one
      of those failures, sixty to sixty-three seconds later, six for six.

      So the cause had no threshold. Its neighbour, the payload path, has one —
      two consecutive failures — and the link had none, although both rest on a
      single network probe and there is no second endpoint to form a quorum.
      The supervisor that owns this tunnel declares the outer path down on the
      second consecutive failure and up on the first success; both halves are in
      its source and one of them was missing here.

      Two is therefore not a chosen number. It is the smallest the measurement
      supports, and over those seven days it would have declared the link absent
      exactly as often as Twilight did, which is never.

      What this leaves standing: a decision record that cannot be diagnosed on
      its own. Reading it required the connectivity archive beside it, and that
      only worked because both are written by the same runtime into the same
      store.

      The second disagreement is also hexroute's, and it is about itself. The
      cycle that saw the process gone also named a wake gap, and the machine had
      not slept. Three marks from the daemon's own log for one cycle:

          22:02:43.391  observation_cycle       the observation finished
          22:03:15.796  connectivity_snapshot   the fold finished, 32.4s later
          22:03:15.809  the decision recorded

      That was the first cycle after the daemon was reinstalled, and its first
      fold cost 32.4 seconds. The loop then slept its interval and observed
      again at 22:04:16, so ninety-three seconds passed between two
      observations against a threshold of ninety.

      The cause is therefore true and its subject is wrong. It is measured as
      the interval between this runtime's own observations, so a runtime that is
      slow reports a sleeping machine. A restart manufactures a rebuild on the
      cycle after it: with authority, installing this daemon would tear down the
      tunnel it was installed to watch. Twilight recorded no gap in that window;
      its own tick is cheap enough that the question never arose.

      Two things are owed and neither is done here. The wake gap should be
      measured from something that says the machine slept rather than from the
      observer's own diligence. And the loop should aim at a period rather than
      sleep a fixed interval after work of unbounded length, or every slow fold
      pushes the next observation out by however long it took.

## 7. Close

- [x] 7.1 Sync the delta into the baseline and archive.

      The capability is new, so the baseline is the delta with a purpose in
      front of it rather than a merge into something that existed.
