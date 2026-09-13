# Tasks

## 1. A record may carry a duration

- [x] 1.1 Add a closed vocabulary of steps that may be timed, refusing anything outside it.
- [x] 1.2 Add a duration in milliseconds, absent rather than zero when there is nothing to report.
- [x] 1.3 Test that an unknown step is refused and that the field is omitted when unset.

## 2. Opening reports what it cost

- [x] 2.1 Have the connectivity host return how long each store took to open.
- [x] 2.2 Record one line per store before the daemon reports that it started.

## 3. Gates and evidence

- [x] 3.1 Mutate the vocabulary check and the omission; confirm the named tests fail.
- [x] 3.2 `make check` green.
- [x] 3.3 Install, restart, and read the daemon's own account of its start.

      Read 2026-09-13, and it corrected two things at once.

      The parts: checkpoints 0.001s, event archive 2.684s, root journal 2.931s,
      user journal 9.087s, replay 2.856s — 17.559 seconds against a window of
      28.663 between one daemon reporting stopped and the next reporting
      started. So the stores are two thirds of it and eleven seconds are not
      them, which no reading from outside had separated.

      And the total reported 0.001 seconds beside seventeen seconds of parts.
      That was a defect in this change: the total was set in a deferred call
      while the function returned its timings by value, so the copy left before
      the defer ran. The instrument caught it on its first reading, which is the
      argument for the instrument.

      Corrected with named returns, a test that fails when a total does not
      cover its parts, and a second record — the whole of what this process did
      before reporting started — so the window is attributed rather than
      assumed. What lies between that and the previous stop belongs to launchd.
- [ ] 3.4 Read the corrected account and attribute the window end to end.

## 4. Close

- [ ] 4.1 Sync the delta into the baseline, validate, archive.
