## Why

Root's operator loop stops for about 2.5 seconds every cycle, and after the two
index fixes that is the whole of what remains. It was measured before any of
this work: 2.2 seconds of observation, of which 2.09 is three endpoint probes
taken one after another at about 0.7 seconds each.

They wait on a network. None of them tells another anything. Taking them in turn
costs their sum for no reason beyond the shape of the loop.

## What Changes

The cycle starts its endpoint probes together and waits once. The results are
folded in configuration order, so the summary is the one sequence would have
produced.

## Capabilities

### Modified Capabilities

- `local-control-plane-foundation`: an observation cycle waits once for what it
  can wait for together.

## Impact

- `internal/rootdaemon/cycle.go` — the probe loop.
- Not in scope: the other observations. They are milliseconds together and
  making them concurrent would add ways to be wrong for nothing.
