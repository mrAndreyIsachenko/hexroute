# Tasks

## 1. Prove the defect before removing it

- [x] 1.1 Write a test that drives cycles where the observer is slow and the machine is awake, and fails if a wake gap is named.
- [x] 1.2 Write a test that drives cycles where the machine slept and the observer was quick, and fails if no wake gap is named.
- [x] 1.3 Write a test for the loop that fails while a slow cycle pushes the next observation out.

## 2. Two clocks

- [x] 2.1 Give the cycle a wall clock and a steady clock, wired in production to `time.Now` and to the monotonic reading it carries.
- [x] 2.2 Replace `Observed.SincePrevious` with `Observed.Slept`, measured as the divergence.
- [x] 2.3 Carry nothing forward from a cycle that did not run: the first cycle has nothing to measure against.

## 3. The loop aims at a period

- [x] 3.1 Schedule the next observation from when the previous one began.
- [x] 3.2 Begin immediately when the period is already spent, and do not accumulate skipped periods.

## 4. Confirm the clock on the machine

- [x] 4.1 Verify on this Mac that the steady clock stops across a real sleep, rather than trusting the toolchain source for it.

      Measured 2026-09-13 with `pmset sleepnow`. The machine slept 2m29.4s and
      the monotonic clock advanced one second across it: 17.027s to 18.028s
      while the wall clock went 17.027s to 2m47.475s. The divergence then held
      at 2m29.448s rather than continuing to grow, so it is the sleep and not a
      rate difference.

      A first attempt closed the lid instead and proved nothing: the machine did
      not sleep, the loop printed every second throughout, and the prediction
      that a lid close is a sleep was simply wrong.

      That attempt did measure one thing worth keeping. With the machine awake
      the two clocks still separate at about 0.34 milliseconds a second, which
      is the wall clock being slewed. Over one observation interval that is
      about twenty milliseconds against a ninety second threshold, so the drift
      is recorded rather than compensated.

## 5. Gates and evidence

- [x] 5.1 Mutate the divergence and the schedule; confirm the named tests fail.

      Five mutations, four of them caught by a named test: measure the interval
      again rather than the divergence, measure against a cycle that never ran,
      wait a whole period after the work, and let the wall clock keep its
      monotonic reading.

      The fifth is the interesting one. Stripping `Round(0)` inside the decision
      changed nothing, because a test drives clocks it made up and those carry no
      monotonic reading to strip. In production it would zero the divergence
      permanently and the cause would never hold however long the machine slept —
      the opposite of the defect being corrected, and invisible to every test
      that fabricates a clock. So the property is asserted of the wall clock
      itself rather than of the arithmetic that uses it, and that assertion is
      what the fourth mutation fails.
- [x] 5.2 `make check` green.
- [ ] 5.3 Install, restart the daemon deliberately, and confirm the cycle after the restart names no wake gap.

## 6. Close

- [ ] 6.1 Sync the delta into the baseline, validate, archive.
