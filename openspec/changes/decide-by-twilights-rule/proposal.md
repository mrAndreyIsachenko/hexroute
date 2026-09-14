# Decide by Twilight's rule

Linear: HEX-16

## Why

The grill of item 9 settled that the executor reproduces the rule Twilight runs,
and that it is proved against Twilight acting before it is allowed to act. The
rule this runtime has is not Twilight's. In the window before the handover it
decided to rebuild the tunnel 125 times where Twilight rebuilt twice, and the
archive showed those were distinct events, not one condition recorded on many
cycles: the two runtimes ask different questions.

- Its carrier signature covers every configured route, 21 of them, including the
  twelve fallback routes that appear and disappear; Twilight's covers three paths
  — the upstream probe and the two ingress hosts. The larger one also counted a
  configuration edit, from 16 entries to 21, as a change of carrier.
- It rebuilds on a returned link and on a payload failure. Twilight does neither
  as a rebuild of this kind: health and restore restarts are off by recorded
  decision, and its payload rebuild waits for failover, which stays with Twilight
  until item 10.
- It names a wake gap when the pure sleep exceeds its threshold. Twilight names
  one when the gap between its ticks — which sleep sixty seconds between them —
  reaches 180 seconds.
- It reapplies routes when they drift. Routes stay with Twilight until a later
  change.

## What

- The decision rule acts on three causes, in Twilight's definitions: the tunnel
  process gone, a tick gap of at least the configured threshold, and a change in
  which interface carries the upstream probe and the ingress hosts. Returned
  links, payload failures and drifted routes stay in the record as grounds and no
  longer lead to an action.
- The record carries what the comparison needs without a second store beside it:
  the tick gap the wake cause was compared on, and the three-path carrier.
- A comparison of this runtime's decisions against Twilight's rebuilds, per cause,
  inside two cycles, counting natural and induced agreements separately.
- A soak of at least seven days, judged by that comparison.

## Impact

- Affected specs: `tunnel-supervision`
- Affected code: `internal/tunnelplan`, `internal/rootdaemon`, `internal/event`,
  and a new comparison under `internal` with a thin command.
- Affected configuration: the wake threshold's meaning changes from pure sleep to
  tick gap, so its installed value has to be read before this is installed.
- Nothing is performed. The executor is change three.

## Closing measurement

The soak passes: at least seven days with no disagreement, at least three natural
wake-gap agreements, two carrier agreements and one induced process loss.
