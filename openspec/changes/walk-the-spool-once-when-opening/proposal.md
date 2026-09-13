# Walk the spool once when opening

## Why

Opening the user journal costs ten seconds, and listing its directory costs 360
milliseconds. The daemon now says so itself: of a 31.2 second window in which it
observes nothing, 11.1 seconds are launchd's throttle and 20.1 are the stores,
and the user journal is 10.2 of those.

The gap between ten seconds and a third of one is not a mystery. Opening a spool
walks its directory three times: `recover` reads it to find pending records,
`recover` ends by listing it again with a stat for every file, and `Open` lists
it a third time for one number — the highest sequence — which the listing
`recover` just took already held.

## What Changes

- Opening a spool keeps what its recovery learned instead of asking again, so
  one stat sweep happens where two did.
- What is kept is the listing the spool already maintains, so the first append
  after opening does not pay for a third.

## Impact

- Affected specs: `bounded-spool-operation-cost`
- Affected code: `internal/spool`
- Not in scope: the archive's open, which reads its directory twice for the same
  kind of reason and costs 3.9 seconds against the spool's 10.2. Same shape,
  smaller, and it should be measured after this rather than bundled into it.
- Not in scope: `ThrottleInterval`, which is a third of the window and is a
  decision about how hard a failing daemon may be allowed to retry, not a defect.
