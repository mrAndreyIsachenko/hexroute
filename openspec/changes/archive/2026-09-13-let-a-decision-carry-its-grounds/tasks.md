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
- [x] 5.3 Install, and read a decision back without another store beside it.

      Read 2026-09-13 from the event archive and nothing else — no connectivity
      observations, no matching on time. Three decisions written by the installed
      build, each answering on its own:

          reapply_routes  causes: routes_drifted
            the cycle saw everything: True
            process running:          True
            machine slept:            0 ms
            carrier:                  49147013057d over 16 entries
            link believed present:    True after 0 consecutive failures
            payload traversed:        True after 0 consecutive failures
            route operations planned: 14

      The carrier digest is identical across the three, which is how a reader
      now sees that it did not change, and sixteen entries is what this runtime
      is configured to watch. `routes_drifted` is no longer a bare word: it
      stands on fourteen planned operations.

      The reader used for this was wrong first and is recorded rather than
      quietly fixed. Run immediately after the install it reported that no
      record carried grounds and asserted the running daemon predated them —
      a guess stated as a fact. It compares against the daemon's own
      `daemon_started` now, and distinguishes a daemon that has not cycled yet
      from one writing records without grounds.

## 6. Close

- [x] 6.1 Sync the delta into the baseline, validate, archive.
