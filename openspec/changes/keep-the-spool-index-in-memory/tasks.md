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

- [ ] 4.2 Re-measure the pause on the machine with both fixes installed, and say
      what remains. Predicting it would be a guess; the pause was measured from
      outside and can be measured again the same way.

## 5. Close

- [ ] 5.1 Sync the delta into the baseline and archive.
