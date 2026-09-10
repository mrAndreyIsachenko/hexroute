# Tasks

## 1. Prove the cost before changing it

- [x] 1.1 A regression that fails against the current code and counts directory
      walks rather than seconds: a cycle's worth of appends takes at most one.
- [x] 1.2 A regression that the kept listing equals a fresh one, after appending
      and after eviction. The saving must not be bought with a wrong answer.
- [x] 1.3 Confirm by mutation — remove the cache, drop the amendment on publish,
      drop it on eviction. Each caught, judged by exit status.

## 2. Keep it

- [x] 2.1 The listing is held on the archive under the lock that already guards
      the directory, learned at the first question.
- [x] 2.2 Publishing a record amends it; evicting records amends it.
- [x] 2.3 Anything that may have half happened drops it, so the next question
      pays for a fresh walk.

## 3. Say what the helpers do

- [x] 3.1 The test helpers that plant and damage records act on the directory
      from outside the archive. They tell it so, because a real archive has one
      writer and now relies on that.

## 4. Gates

- [x] 4.1 `make check`, judged by exit status.
- [x] 4.2 `make secret-test`.

## 5. Measure what it bought

- [x] 5.1 Record the measurement rather than the intention: cost per cycle
      before and after, at a stated number of stored records.

      Eleven appends — one cycle's worth — against 20,000 stored records, on
      this machine: **0.767s** listing the directory each time, **0.109s**
      listing it once. Seven times, and the ratio grows with the store because
      only one of the two numbers does.

- [x] 5.2 Say plainly how much of the observed pause this accounts for and how
      much it does not.

      The live archive holds 69,000 records, about three and a half times the
      measured store, so this returns roughly 2.7 seconds per cycle. The pause
      was 6.6 to 13.1 seconds and observation accounts for 2.2 of it. So between
      one and eight seconds remain unaccounted for, and this change does not
      claim them.

      What was ruled out along the way, each by measurement: the endpoint probes
      (three, 0.7s each, all succeeding well inside their four-second timeouts),
      the route lookups (seventeen, 0.03s together), and the durable writes
      (twenty-two per cycle, 0.2s with both the file and the directory synced).
      The connectivity checkpoint store was suspected and cleared: its index is
      bounded by `MaxIndexEntries` and its latest pointer is read from a file
      rather than found by listing.

## 6. Close

- [x] 6.1 Sync the delta into the baseline and archive.
