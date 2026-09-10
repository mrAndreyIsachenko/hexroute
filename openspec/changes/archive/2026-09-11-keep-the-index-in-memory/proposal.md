## Why

The root runtime's operator loop stops answering for six to thirteen seconds
once a minute, against a fifteen-second deadline on the socket it is failing to
answer. It was measured on 2026-09-11 by sending it requests that can perform
nothing and timing the replies.

A profile of the live process, taken while it was unresponsive, puts the time in
`os.ReadDir` and the sort inside it, with the garbage collector behind them
clearing the entries.

The archive lists its directory on every append. One cycle appends eleven
records, so it lists 69,000 entries eleven times. The previous change here
removed the decode from that listing and left the listing, and the requirement
it wrote down said the cost still grows with how many records are stored — which
is true per append and misses that a cycle makes many.

## What Changes

The archive learns its directory once and keeps that current as it publishes and
evicts. It forgets whenever a write may have half happened, so nothing is
answered from a listing it stopped being sure about.

## Capabilities

### Modified Capabilities

- `local-event-archive`: appending costs the write, not a directory listing.

## Impact

- `internal/eventarchive` — the listing is kept, amended and dropped.
- Not in scope: the stores hold 456MB and 270MB across 176,000 files. The
  retention window is what admits that, and narrowing it is a decision about how
  much history is wanted rather than about cost.
- Not in scope: the remainder of the pause. This accounts for part of it, and
  what is left needs another profile rather than another guess.
