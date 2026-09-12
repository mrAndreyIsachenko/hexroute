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

- [ ] 6.1 Install and run beside Twilight. Nothing is performed.
- [ ] 6.2 Replay the rule against the supervisor's 64 recorded restarts and
      record where it disagrees.
- [ ] 6.3 Each of the six causes agrees at least once: the carrier and the wake
      by waiting, the process, the returned link and the payload path by
      inducing them.
- [ ] 6.4 Record every disagreement, including the ones where Twilight acted and
      said nothing.

## 7. Close

- [ ] 7.1 Sync the delta into the baseline and archive.
