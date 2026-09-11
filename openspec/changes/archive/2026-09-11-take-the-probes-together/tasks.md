# Tasks

## 1. Prove it before changing it

- [x] 1.1 A regression that asserts the property rather than a duration: each
      probe announces itself and waits for the others, so taken in turn the
      first waits alone and the test says so on its own deadline.
- [x] 1.2 A regression that the summary does not follow completion order: two
      probes fail, the later one in configuration order finishes last, and the
      failure recorded is the one sequence would have left.
- [x] 1.3 Confirm by mutation — take the probes in turn again, and stop waiting
      for them. Both caught, judged by exit status.

## 2. Take them together

- [x] 2.1 Every probe starts, each answer lands at its configured index, and the
      cycle waits once.
- [x] 2.2 The fold is the code that was there, walking the answers in
      configuration order. Concurrency changes when the cycle waits and nothing
      about what it concludes.
- [x] 2.3 The probes take the cycle's own context, so a cancelled cycle cancels
      every probe in flight.
- [x] 2.4 Only the endpoint probes. The rest of the observation costs 0.08
      seconds together, and making it concurrent would buy nothing and add ways
      to be wrong.

## 3. Gates

- [x] 3.1 `make check`, judged by exit status.
- [x] 3.2 `go test -race ./internal/rootdaemon/`, because this is the first
      concurrency in the cycle.

## 4. Prove it on the machine

- [x] 4.1 Install root and re-measure the pause the way it has been measured
      throughout: requests the runtime cannot act on, timed from outside.

      The first install measured nothing, because what was installed was the
      previous change. `make check` had rebuilt `bin/hexrouted` before this
      work was committed, so the binary in `bin/` carried the revision before
      it, and the install command named that path. The reinstall was verified
      by digest against the freshly built one before anything was measured.
- [x] 4.2 Record what it says.

      Twenty-five minutes, 24 samples:

      | | before this | after |
      |---|---|---|
      | pause | 2.4 to 4.6s | 1.0 to 4.3s |
      | median | about 2.5s | **1.21s** |
      | mean | — | 1.37s |
      | over two seconds | all | 1 of 24 |

      About 1.3 seconds returned, against a prediction of about 1.4.

      The whole arc, measured the same way each time: 9.8 seconds typical
      before any of it, 2.5 after the archive and the spool stopped listing
      their directories, 1.21 now. Against a fifteen-second deadline.

      What is left agrees with the parts: the slowest probe at about 0.7
      seconds, the rest of the observation at 0.08, the durable writes at about
      0.2 and the listings at about 0.2.

      One sample of 24 reached 4.3 seconds. It is recorded rather than
      explained: one outlier is not a pattern, and chasing it would mean
      profiling for a shape that may not recur.

## 5. Close

- [x] 5.1 Sync the delta into the baseline and archive.
