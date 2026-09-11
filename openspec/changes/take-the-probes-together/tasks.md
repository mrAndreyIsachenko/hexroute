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

- [ ] 4.1 Install root and re-measure the pause the way it has been measured
      throughout: requests the runtime cannot act on, timed from outside.
- [ ] 4.2 Record what it says. The prediction is about 1.4 seconds returned from
      a pause of about 2.5; record what happened rather than what was expected.

## 5. Close

- [ ] 5.1 Sync the delta into the baseline and archive.
