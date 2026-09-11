# Tasks

## 1. Prove the cost before changing it

- [x] 1.1 A regression that counts directory listings rather than seconds: a
      cycle's worth of appends takes at most one.
- [x] 1.2 A regression that the kept listing equals a fresh one, after appending
      and after eviction.
- [x] 1.3 Confirm by mutation — remove the cache, drop the amendment on commit,
      stop removing evictions from it. Each caught, judged by exit status.

## 2. Keep it

- [x] 2.1 The listing is held on the spool under the lock that already guards
      the directory, learned at the first question.
- [x] 2.2 Committing amends it. Acknowledging and quarantining drop it: they
      move stable records and are rare beside appending.
- [x] 2.3 `Open` and `recover` still read the disk. Finding out what is actually
      there is their whole purpose.

## 3. Gates

- [x] 3.1 `make check`, judged by exit status.
- [x] 3.2 `make secret-test`.

## 4. Measure, and say what is left

- [x] 4.1 Record the measurement: one cycle's eleven appends against twenty
      thousand stored records cost **0.775s** listing each time and **0.097s**
      listing once. The archive measured 0.767 and 0.109 for the same shape.

- [x] 4.2 Re-measure the pause on the machine with both fixes installed, and say
      what remains.

      Measured the same way as before — requests the runtime cannot act on,
      timed from outside — over 25 minutes with both fixes installed and the
      stores at the size they had reached.

      | | before | after |
      |---|---|---|
      | pause | 6.6 to 13.1s | 2.4 to 4.6s |
      | typical | about 9.8s | about 2.5s |
      | period | about 70s | about 63s |
      | samples | 23 | 23 |

      What remains is the observation itself, and it was measured separately
      before any of this: 2.2 seconds, of which 2.09 is three endpoint probes
      taken one after another at about 0.7 seconds each. The residual and that
      number agree, so the pause is now accounted for end to end rather than
      partly.

      The probes are sequential and taking them together would return about 1.4
      seconds more. That is a change, not a leftover, and it is not made here:
      the loop now answers inside its deadline with eleven seconds to spare,
      and concurrency in an observation loop is worth proposing on its own
      terms rather than as the tail of a cost fix.

      One correction belongs in this record. Seven minutes into the run its
      output file was empty and this was read as no pause having occurred. The
      file was empty because the output was buffered. Absence of output is not
      absence of the thing, and it was reported as a result before the run
      finished.

## 5. Close

- [x] 5.1 Sync the delta into the baseline and archive.
