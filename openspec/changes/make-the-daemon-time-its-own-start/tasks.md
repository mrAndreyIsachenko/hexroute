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
- [ ] 3.3 Install, restart, and read the daemon's own account of its start.

## 4. Close

- [ ] 4.1 Sync the delta into the baseline, validate, archive.
