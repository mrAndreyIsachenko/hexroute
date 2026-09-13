# Tasks

## 1. Prove the record cannot be read

- [x] 1.1 Write a test that a decision naming a cause carries the observation it was reached from; it must fail on the current record.

## 2. The planner returns what it used

- [x] 2.1 Add the grounds to the plan, built from what `Decide` already has.
- [x] 2.2 Leave absent what an incomplete cycle did not observe.

## 3. The record carries them

- [x] 3.1 Extend the event payload and its closed validation.
- [x] 3.2 Record the carrier as a truncated digest and a count, never as destinations.
- [x] 3.3 Fill the record from the plan's grounds rather than by gathering again.

## 4. The boundary holds

- [x] 4.1 Test that no destination reaches the record, with a live-shaped signature rather than a fixture that hides it.

## 5. Gates and evidence

- [x] 5.1 Mutate each ground and the digest; confirm the named tests fail.

      Six fail a named test: report the payload count after it was spent, put
      the signature where the digest belongs, report what an incomplete cycle
      did not see, drop the carrier count, accept a digest of any length, accept
      a cycle that both did and did not observe.

      The last two survived at first, and the reason was that nothing drove the
      refusals at all. The validation was written with the fields and tested
      only through records that pass it, so the checks that keep a whole carrier
      signature out of the field built to keep destinations out were held by
      nothing. They are driven now, from the path that stores a record rather
      than from a copy of the rule beside it.
- [x] 5.2 `make check` green.
- [ ] 5.3 Install, and read a decision back without another store beside it.

## 6. Close

- [ ] 6.1 Sync the delta into the baseline, validate, archive.
